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

// fakeLister returns a fixed session set (or an error) for the rail.
type fakeLister struct {
	sessions []api.SessionInfo
	err      error
}

func (f *fakeLister) List(context.Context) ([]api.SessionInfo, error) {
	return f.sessions, f.err
}

// fakeSurface records Select calls and reports a fixed Current.
type fakeSurface struct {
	current  api.SessionID
	selected api.SessionID
}

func (f *fakeSurface) Select(id api.SessionID) { f.selected = id }
func (f *fakeSurface) Current() api.SessionID  { return f.current }

// newTestServer builds a Server with empty hubs and the given rail backing.
func newTestServer(t *testing.T, list ui.Lister, tl ui.SessionSurface) *ui.Server {
	t.Helper()
	s, err := ui.NewServer(bridge.NewHub[api.Event](), bridge.NewHub[string](), list, tl)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

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
	s := newTestServer(t, &fakeLister{}, &fakeSurface{})

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
	s := newTestServer(t, &fakeLister{}, &fakeSurface{})

	// A request without the per-run token in the path has no handler → 404.
	u, _ := url.Parse(s.Base())
	u.Path = "/"
	status, _ := get(t, u.String())
	if status != http.StatusNotFound {
		t.Errorf("untokened root status = %d, want 404", status)
	}
}

func TestSessionsRailRendersAndMarksCurrent(t *testing.T) {
	list := &fakeLister{sessions: []api.SessionInfo{
		{SessionID: "ses-1", ProviderID: "claude", Status: api.StatusRunning, Label: "spike"},
		{SessionID: "ses-2", ProviderID: "codex", Status: api.StatusIdle},
	}}
	s := newTestServer(t, list, &fakeSurface{current: "ses-2"})

	status, body := get(t, s.Base()+"sessions")
	if status != http.StatusOK {
		t.Fatalf("sessions status = %d", status)
	}
	if !strings.Contains(body, "spike") || !strings.Contains(body, "claude") {
		t.Errorf("rail missing a session: %s", body)
	}
	if !strings.Contains(body, "running") {
		t.Errorf("rail missing status pill: %s", body)
	}
	// ses-2 is current → its row carries the active class.
	if !strings.Contains(body, "active") {
		t.Errorf("current session not marked active: %s", body)
	}
}

func TestSelectSwitchesTimelineAndReturnsPanel(t *testing.T) {
	surface := &fakeSurface{}
	s := newTestServer(t, &fakeLister{}, surface)

	form := strings.NewReader("session=ses-9")
	req, _ := http.NewRequest(http.MethodPost, s.Base()+"select", form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST select: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if surface.selected != "ses-9" {
		t.Errorf("Select got %q, want ses-9", surface.selected)
	}
	if !strings.Contains(string(body), `id="timeline"`) {
		t.Errorf("select did not return a timeline panel: %s", body)
	}
}

func TestEventsSSEStreamsWorkspaceBroadcasts(t *testing.T) {
	evHub := bridge.NewHub[api.Event]()
	s, err := ui.NewServer(evHub, bridge.NewHub[string](), &fakeLister{}, &fakeSurface{})
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
	s, err := ui.NewServer(bridge.NewHub[api.Event](), tlHub, &fakeLister{}, &fakeSurface{})
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
