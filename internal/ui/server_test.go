package ui_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dusto/tend/api"

	"github.com/dusto/tend-ui/internal/bridge"
	"github.com/dusto/tend-ui/internal/ui"
)

// get fetches u and returns the status code and body, closing the body itself
// so callers hold no *http.Response (keeps bodyclose happy).
func get(t *testing.T, u string) (int, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestServerServesShellAndAssets(t *testing.T) {
	s, err := ui.NewServer(bridge.NewHub[api.Event](), bridge.NewHub[string]())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = s.Close() }()

	// The app shell renders at the tokenized base.
	status, body := get(t, s.Base())
	if status != http.StatusOK {
		t.Fatalf("shell status = %d", status)
	}
	for _, want := range []string{"<title>tend-ui</title>", `href="assets/app.css"`, "assets/htmx.min.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("shell missing %q", want)
		}
	}

	// An embedded asset is served under the token path.
	status, css := get(t, s.Base()+"assets/app.css")
	if status != http.StatusOK {
		t.Fatalf("asset status = %d", status)
	}
	if !strings.Contains(css, "--teal") {
		t.Error("app.css did not come through the embed")
	}
}

func TestServerTokenGuardsRoot(t *testing.T) {
	s, err := ui.NewServer(bridge.NewHub[api.Event](), bridge.NewHub[string]())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = s.Close() }()

	// A request without the per-run token in the path has no handler → 404.
	u, _ := url.Parse(s.Base())
	u.Path = "/"
	status, _ := get(t, u.String())
	if status != http.StatusNotFound {
		t.Errorf("untokened root status = %d, want 404", status)
	}
}

func TestEventsSSEStreamsWorkspaceBroadcasts(t *testing.T) {
	evHub := bridge.NewHub[api.Event]()
	s, err := ui.NewServer(evHub, bridge.NewHub[string]())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = s.Close() }()

	assertSSEDelivers(t, s.Base()+"events", "provider_started", func() {
		evHub.Broadcast(api.Event{Type: "provider_started", Seq: 1, TS: time.Unix(0, 0)})
	})
}

func TestTimelineSSEStreamsRenderedBlocks(t *testing.T) {
	tlHub := bridge.NewHub[string]()
	s, err := ui.NewServer(bridge.NewHub[api.Event](), tlHub)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = s.Close() }()

	assertSSEDelivers(t, s.Base()+"timeline", "tl-marker-42", func() {
		tlHub.Broadcast(`<div class="tl">tl-marker-42</div>`)
	})
}

// assertSSEDelivers opens the SSE endpoint at url, runs broadcast once the
// stream is open, and fails unless a line containing want arrives.
func assertSSEDelivers(t *testing.T, url, want string, broadcast func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	// Broadcast only after the opening comment, so the subscription is registered
	// (a broadcast before anyone listens is dropped by design).
	sc := bufio.NewScanner(resp.Body)
	sawConnected, saw := false, false
	for sc.Scan() {
		line := sc.Text()
		if !sawConnected && strings.HasPrefix(line, ": connected") {
			sawConnected = true
			broadcast()
			continue
		}
		if strings.Contains(line, want) {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatalf("did not receive %q over SSE", want)
	}
}
