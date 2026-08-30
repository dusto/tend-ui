package ui_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dusto/tend/api"

	"github.com/dusto/tend-ui/internal/bridge"
	"github.com/dusto/tend-ui/internal/session"
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

// fakeSurface records Select calls and reports a fixed Current, Usage, and tool list.
type fakeSurface struct {
	current  api.SessionID
	selected api.SessionID
	usage    session.Usage
	tools    []session.ToolRef
}

func (f *fakeSurface) Select(id api.SessionID)      { f.selected = id }
func (f *fakeSurface) Current() api.SessionID       { return f.current }
func (f *fakeSurface) Usage() session.Usage         { return f.usage }
func (f *fakeSurface) ToolCalls() []session.ToolRef { return f.tools }

// fakeConn reports a fixed connection state.
type fakeConn struct{ up bool }

func (f fakeConn) Connected() bool { return f.up }

// fakeCmd records the interactive-control calls and serves a fixed approval set.
type fakeCmd struct {
	approvals  []api.ApprovalSummary
	responded  map[api.ApprovalID]bool
	promptedTo api.SessionID
	promptText string
	cancelled  api.SessionID
	stopped    api.SessionID
}

func (f *fakeCmd) Approvals(context.Context, api.SessionID) ([]api.ApprovalSummary, error) {
	return f.approvals, nil
}
func (f *fakeCmd) Respond(_ context.Context, id api.ApprovalID, approved bool) error {
	if f.responded == nil {
		f.responded = map[api.ApprovalID]bool{}
	}
	f.responded[id] = approved
	return nil
}
func (f *fakeCmd) Prompt(_ context.Context, s api.SessionID, text string) error {
	f.promptedTo, f.promptText = s, text
	return nil
}
func (f *fakeCmd) Cancel(_ context.Context, s api.SessionID) error { f.cancelled = s; return nil }
func (f *fakeCmd) Stop(_ context.Context, s api.SessionID) error   { f.stopped = s; return nil }

// newTestServer builds a Server with empty hubs and the given rail backing
// (connected by default, empty commander).
func newTestServer(t *testing.T, list ui.Lister, tl ui.SessionSurface) *ui.Server {
	t.Helper()
	return newTestServerWith(t, list, tl, &fakeCmd{})
}

func newTestServerWith(t *testing.T, list ui.Lister, tl ui.SessionSurface, ctl ui.Commander) *ui.Server {
	t.Helper()
	s, err := ui.NewServer(bridge.NewHub[api.Event](), bridge.NewHub[string](), list, tl, fakeConn{up: true}, ctl)
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

func TestHeaderRendersUsageForCurrentSession(t *testing.T) {
	list := &fakeLister{sessions: []api.SessionInfo{
		{SessionID: "ses-1", ProviderID: "claude", CurrentModelID: "sonnet-4.6", Status: api.StatusRunning, Label: "spike"},
	}}
	surface := &fakeSurface{current: "ses-1", usage: session.Usage{
		ContextUsed: 124800, ContextWindow: 200000, HasContext: true,
		LastInput: 8200, LastOutput: 1100, LastTotal: 9300, HasToken: true,
	}}
	s := newTestServer(t, list, surface)

	status, body := get(t, s.Base()+"header")
	if status != http.StatusOK {
		t.Fatalf("header status = %d", status)
	}
	for _, want := range []string{"spike", "claude", "sonnet-4.6", "62%", "124,800", "9,300"} {
		if !strings.Contains(body, want) {
			t.Errorf("header missing %q: %s", want, body)
		}
	}
}

func TestJumpIndexRendersToolCalls(t *testing.T) {
	surface := &fakeSurface{tools: []session.ToolRef{
		{ID: "tc-a", Name: "read_buffer", Kind: "tool", Arg: "/repo/main.go", Status: "completed"},
		{ID: "tc-b", Name: "edit_buffer", Kind: "edit", Status: "running"},
	}}
	s := newTestServer(t, &fakeLister{}, surface)

	status, body := get(t, s.Base()+"jump")
	if status != http.StatusOK {
		t.Fatalf("jump status = %d", status)
	}
	for _, want := range []string{"read_buffer", "/repo/main.go", "completed", "edit_buffer", "tc-tc-a"} {
		if !strings.Contains(body, want) {
			t.Errorf("jump index missing %q: %s", want, body)
		}
	}
}

func TestHeaderEmptyWhenNoCurrentSession(t *testing.T) {
	s := newTestServer(t, &fakeLister{}, &fakeSurface{current: ""})
	_, body := get(t, s.Base()+"header")
	if !strings.Contains(body, "No session selected") {
		t.Errorf("expected empty-state header: %s", body)
	}
}

// The connection indicator reflects the real connection state, not a hardcoded
// value: connected renders green, disconnected renders the "down" state.
func TestStatusReflectsConnectionState(t *testing.T) {
	up, err := ui.NewServer(bridge.NewHub[api.Event](), bridge.NewHub[string](), &fakeLister{}, &fakeSurface{}, fakeConn{up: true}, &fakeCmd{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = up.Close() }()
	if _, body := get(t, up.Base()+"status"); strings.Contains(body, "down") || !strings.Contains(body, "connected") {
		t.Errorf("connected status wrong: %s", body)
	}

	down, err := ui.NewServer(bridge.NewHub[api.Event](), bridge.NewHub[string](), &fakeLister{}, &fakeSurface{}, fakeConn{up: false}, &fakeCmd{})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() { _ = down.Close() }()
	if _, body := get(t, down.Base()+"status"); !strings.Contains(body, "down") || !strings.Contains(body, "reconnecting") {
		t.Errorf("disconnected status wrong: %s", body)
	}
}

func TestApprovalsRenderWithDiffAndActions(t *testing.T) {
	detail, _ := json.Marshal(api.ApprovalDetail{
		Kind: "file_edit",
		FileEdit: &api.FileEditApproval{Targets: []api.FileEditTarget{{
			URI: "file:///repo/a.go", Diff: "@@ -1 +1 @@\n-old\n+new",
		}}},
	})
	ctl := &fakeCmd{approvals: []api.ApprovalSummary{
		{ApprovalID: "ap-1", SessionID: "ses-1", Kind: "file_edit", Detail: detail},
	}}
	s := newTestServerWith(t, &fakeLister{}, &fakeSurface{current: "ses-1"}, ctl)

	status, body := get(t, s.Base()+"approvals")
	if status != http.StatusOK {
		t.Fatalf("approvals status = %d", status)
	}
	for _, want := range []string{"Apply file edit?", "+new", "-old", `hx-post="approve"`, "ap-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("approvals missing %q: %s", want, body)
		}
	}
}

func TestApprovePostsRespondTrue(t *testing.T) {
	ctl := &fakeCmd{approvals: []api.ApprovalSummary{{ApprovalID: "ap-9", SessionID: "ses-1"}}}
	s := newTestServerWith(t, &fakeLister{}, &fakeSurface{current: "ses-1"}, ctl)
	postForm(t, s.Base()+"approve", "approval_id=ap-9")
	if ctl.responded["ap-9"] != true {
		t.Errorf("approve did not respond approved=true: %+v", ctl.responded)
	}
}

func TestDenyPostsRespondFalse(t *testing.T) {
	ctl := &fakeCmd{approvals: []api.ApprovalSummary{{ApprovalID: "ap-9", SessionID: "ses-1"}}}
	s := newTestServerWith(t, &fakeLister{}, &fakeSurface{current: "ses-1"}, ctl)
	postForm(t, s.Base()+"deny", "approval_id=ap-9")
	if v, ok := ctl.responded["ap-9"]; !ok || v {
		t.Errorf("deny did not respond approved=false: %+v", ctl.responded)
	}
}

func TestRespondIgnoresApprovalNotForFocusedSession(t *testing.T) {
	// The focused session (ses-1) has one pending approval (ap-1). A post for a
	// different id (ap-OTHER, e.g. a stale panel after a switch) must NOT respond.
	ctl := &fakeCmd{approvals: []api.ApprovalSummary{
		{ApprovalID: "ap-1", SessionID: "ses-1", Kind: "file_edit"},
	}}
	s := newTestServerWith(t, &fakeLister{}, &fakeSurface{current: "ses-1"}, ctl)

	postForm(t, s.Base()+"approve", "approval_id=ap-OTHER")
	if _, ok := ctl.responded["ap-OTHER"]; ok {
		t.Errorf("responded to an approval not pending for the focused session: %+v", ctl.responded)
	}
	// The legitimate one still works.
	postForm(t, s.Base()+"approve", "approval_id=ap-1")
	if ctl.responded["ap-1"] != true {
		t.Errorf("focused-session approval not answered: %+v", ctl.responded)
	}
}

func TestRespondNoOpWithoutFocusedSession(t *testing.T) {
	ctl := &fakeCmd{approvals: []api.ApprovalSummary{{ApprovalID: "ap-1", SessionID: "ses-1"}}}
	s := newTestServerWith(t, &fakeLister{}, &fakeSurface{current: ""}, ctl)
	postForm(t, s.Base()+"approve", "approval_id=ap-1")
	if len(ctl.responded) != 0 {
		t.Errorf("responded with no focused session: %+v", ctl.responded)
	}
}

func TestPromptDispatchesToFocusedSession(t *testing.T) {
	ctl := &fakeCmd{}
	s := newTestServerWith(t, &fakeLister{}, &fakeSurface{current: "ses-7"}, ctl)
	postForm(t, s.Base()+"prompt", "text=hello+there")
	if ctl.promptedTo != "ses-7" || ctl.promptText != "hello there" {
		t.Errorf("prompt = %q / %q, want ses-7 / 'hello there'", ctl.promptedTo, ctl.promptText)
	}
}

func TestCancelAndStopTargetFocusedSession(t *testing.T) {
	ctl := &fakeCmd{}
	s := newTestServerWith(t, &fakeLister{}, &fakeSurface{current: "ses-3"}, ctl)
	postForm(t, s.Base()+"cancel", "")
	postForm(t, s.Base()+"stop", "")
	if ctl.cancelled != "ses-3" || ctl.stopped != "ses-3" {
		t.Errorf("cancel/stop = %q / %q, want ses-3", ctl.cancelled, ctl.stopped)
	}
}

func TestControlActionsNeedFocusedSession(t *testing.T) {
	ctl := &fakeCmd{}
	s := newTestServerWith(t, &fakeLister{}, &fakeSurface{current: ""}, ctl)
	if code := postForm(t, s.Base()+"prompt", "text=hi"); code != http.StatusBadRequest {
		t.Errorf("prompt with no session = %d, want 400", code)
	}
	if ctl.promptedTo != "" {
		t.Error("prompt dispatched with no focused session")
	}
}

// postForm posts a urlencoded body and returns the status code.
func postForm(t *testing.T, u, body string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, u, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", u, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// A session id containing JSON metacharacters must be safely escaped in hx-vals
// (built via templ.JSONString, not string concatenation) — no attribute break,
// no injection.
func TestSessionsRailEscapesIDInHXVals(t *testing.T) {
	nasty := `x"><b> inject`
	list := &fakeLister{sessions: []api.SessionInfo{
		{SessionID: api.SessionID(nasty), ProviderID: "claude", Status: api.StatusIdle},
	}}
	s := newTestServer(t, list, &fakeSurface{})

	_, body := get(t, s.Base()+"sessions")
	if !strings.Contains(body, "hx-vals") {
		t.Fatalf("rail row missing hx-vals: %s", body)
	}
	// The raw id must not appear unescaped (that would break out of the attribute
	// or inject markup); templ.JSONString + attribute escaping neutralize it.
	if strings.Contains(body, `"session":"`+nasty) {
		t.Errorf("session id not escaped in hx-vals: %s", body)
	}
	if strings.Contains(body, "<b> inject") {
		t.Errorf("session id broke out as raw markup: %s", body)
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
	s, err := ui.NewServer(evHub, bridge.NewHub[string](), &fakeLister{}, &fakeSurface{}, fakeConn{up: true}, &fakeCmd{})
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
	s, err := ui.NewServer(bridge.NewHub[api.Event](), tlHub, &fakeLister{}, &fakeSurface{}, fakeConn{up: true}, &fakeCmd{})
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
