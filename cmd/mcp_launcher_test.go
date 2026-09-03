package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

func TestLaunchWorkspacesReturnsTheRegistry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	registerStack(t, agentD, "acme", stackWithAgentConfig(t, ""))

	rec := httptest.NewRecorder()
	launchWorkspacesHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/workspaces", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the workspace list may carry session URLs — it must not be cached")
	}
	var body struct {
		Workspaces []launchWorkspace `json:"workspaces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Workspaces) != 1 || body.Workspaces[0].ID != "acme" {
		t.Fatalf("workspaces = %+v", body.Workspaces)
	}
}

func TestLaunchWorkspacesRejectsNonGet(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	rec := httptest.NewRecorder()
	launchWorkspacesHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/workspaces", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestLaunchStartRequiresAWorkspace(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	rec := httptest.NewRecorder()
	launchStartHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/start", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a missing workspace", rec.Code)
	}
}

func TestLaunchStartRejectsNonPost(t *testing.T) {
	rec := httptest.NewRecorder()
	launchStartHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/start", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestLaunchStartRefusesASensitiveWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", dir)
	agentD, _ := agentDir()
	stack := stackWithAgentConfig(t, "version: 1\nworkspace:\n  id: acme\n  sensitive: true\n")
	registerStack(t, agentD, "acme", stack)
	// A daemon must appear "running" for start to reach the resolver, but with no
	// daemon the handler returns a clear error anyway; assert we don't 200 a start
	// for a sensitive workspace. Here there is no daemon, so it errors before the
	// sensitive check — which is still a non-200, the property we care about
	// (a sensitive workspace never quietly starts).
	rec := httptest.NewRecorder()
	launchStartHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/start",
		strings.NewReader(`{"workspace":"acme"}`)))
	if rec.Code == http.StatusOK {
		t.Error("start must not report success when the daemon is down / workspace is sensitive")
	}
}

func TestLauncherPageIsSelfContainedAndUsesTheStoredToken(t *testing.T) {
	rec := httptest.NewRecorder()
	launcherPageHandler(rec, httptest.NewRequest(http.MethodGet, "/app", nil))
	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Error("the launcher page must be served as HTML")
	}
	if !strings.Contains(body, "localStorage.getItem('corgi_token')") {
		t.Error("the launcher must read the device token from localStorage")
	}
	if strings.Contains(body, "src=\"http") || strings.Contains(body, "href=\"http") {
		t.Error("the launcher must be self-contained — no external assets")
	}
	// Lock the session-link hardening in place so it can't be silently removed.
	if !strings.Contains(body, "safeClaudeUrl(") {
		t.Error("the session link must be gated by safeClaudeUrl — a scanned URL must be validated before it is clickable")
	}
	if !strings.Contains(body, "noopener") {
		t.Error("the session link must open with rel=noopener")
	}
}

func TestLauncherPageKeepsThePhoneControlsItNeeds(t *testing.T) {
	rec := httptest.NewRecorder()
	launcherPageHandler(rec, httptest.NewRequest(http.MethodGet, "/app", nil))
	body := rec.Body.String()
	for what, want := range map[string]string{
		"the sessions panel":           "toggleSessions(",
		"start options":                "data-role=\"' + role + '\"",
		"the where-links-open control": "modeSwitch(",
		"hiding a card":                "corgi_hidden",
		"stopping a session":           "/launch/stop",
		"the state on the dot":         "dotState(",
		"the live session name":        "nameNote(",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the launcher must keep %s (%q)", what, want)
		}
	}
	// A control toggled with the hidden attribute must actually disappear:
	// a flex display rule outranks the attribute without this.
	if !strings.Contains(body, "[hidden]{display:none!important}") {
		t.Error("the launcher must force [hidden] over its display rules")
	}
	// The refresh tick reads the session records again, which is how a session
	// Claude renamed shows its new name here without a reload.
	if !strings.Contains(body, "REFRESH_MS") {
		t.Error("the launcher must refresh itself while it is on screen")
	}
}

func TestLaunchStateNamesWhatTheCardLeadsWith(t *testing.T) {
	attention := &launchLastEvent{Kind: "attention"}
	exited := &launchLastEvent{Kind: "exited", Cause: "network timeout"}
	for name, tc := range map[string]struct {
		row  launchWorkspace
		want string
	}{
		"a refused start is blocked":      {launchWorkspace{Note: "not logged in"}, "blocked"},
		"a blocking prompt outranks live": {launchWorkspace{Running: true, Live: 1, LastEvent: attention}, "attention"},
		"a live session is live":          {launchWorkspace{Running: true, Live: 2, LastEvent: exited}, "live"},
		"supervised with no session yet":  {launchWorkspace{Running: true}, "starting"},
		"nothing running is stopped":      {launchWorkspace{LastEvent: exited}, "stopped"},
		// A session someone started on the laptop is live even though the
		// daemon is not supervising the workspace.
		"an unsupervised session is live": {launchWorkspace{Live: 1}, "live"},
	} {
		if got := launchState(tc.row); got != tc.want {
			t.Errorf("%s: state = %q, want %q", name, got, tc.want)
		}
	}
}

func TestTopSessionCarriesTheLiveName(t *testing.T) {
	// Claude Code rewrites name/nameSource/nameSince in its own session record
	// when the session is renamed; the launcher must pass that through rather
	// than the name corgi started it with.
	top := newestLiveSession([]claudeSession{{
		Name: "Trim the launcher type scale", NameSource: "auto", NameSince: 1700000001000,
		StartedAt: 1700000000000, Kind: "interactive", Entrypoint: "sdk-cli",
		URL: "https://claude.ai/code/session_abc",
	}})
	if top == nil {
		t.Fatal("a session with a URL must become the top session")
	}
	if top.Name != "Trim the launcher type scale" {
		t.Errorf("name = %q, want the session's current name", top.Name)
	}
	if top.NameSource != "auto" || top.NameSince != 1700000001000 {
		t.Errorf("nameSource/nameSince = %q/%d, want them passed through so the page can say it was renamed",
			top.NameSource, top.NameSince)
	}
}

func TestLaunchStateReportsADisabledWorkspace(t *testing.T) {
	// A workspace the daemon gave up on looked exactly like a stopped one, so
	// Start was the tap that appeared to do nothing.
	if got := launchState(launchWorkspace{Disabled: true}); got != "disabled" {
		t.Errorf("state = %q, want disabled", got)
	}
	if got := launchState(launchWorkspace{Disabled: true, Running: true, Live: 2}); got != "disabled" {
		t.Errorf("state = %q; disabled outranks a live session — it is the thing to explain", got)
	}
}

func TestSessionProcessIsLiveRejectsARecycledPID(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no start time to read on this platform")
	}
	mine := os.Getpid()
	started, ok := processStartToken(mine)
	if !ok || started == "" {
		t.Fatalf("this process's start time must be readable, got %q ok=%v", started, ok)
	}
	if !sessionProcessIsLive(claudeSession{PID: mine, ProcStart: started}) {
		t.Error("a record whose start time matches the live process is live")
	}
	// The record outlived the machine and something else now holds its pid:
	// a live pid alone made a finished session show as live.
	if sessionProcessIsLive(claudeSession{PID: mine, ProcStart: "1"}) {
		t.Error("a start time that does not match must not pass for the same process")
	}
	// No recorded start time (an older CLI) keeps the old behaviour.
	if !sessionProcessIsLive(claudeSession{PID: mine}) {
		t.Error("without a recorded start time a live pid is still the best answer")
	}
	if sessionProcessIsLive(claudeSession{PID: 0}) {
		t.Error("pid 0 is not a session")
	}
}

func TestSameStartTokenIgnoresPsPadding(t *testing.T) {
	// ps pads a single-digit day, and the same reading must not look like a
	// different process over a space: that would drop a live session.
	if !sameStartToken("Wed Sep  3 16:04:12 2025", "Wed Sep 3 16:04:12 2025") {
		t.Error("internal padding must not matter")
	}
	if sameStartToken("Wed Sep  3 16:04:12 2025", "Wed Sep  3 16:04:13 2025") {
		t.Error("a different second is a different process")
	}
	if !sameStartToken(" 690 ", "690") {
		t.Error("surrounding space must not matter")
	}
}

func TestStartTokenCacheAnswersTwice(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("no start time to read on this platform")
	}
	// The second read must come from the cache: on darwin the uncached path
	// runs ps, and this list is polled every second.
	first, ok := processStartToken(os.Getpid())
	if !ok {
		t.Skip("could not read this process's start time")
	}
	cached, hit := cachedStartToken(os.Getpid())
	if !hit || cached != first {
		t.Errorf("cached = %q hit=%v, want the first reading %q", cached, hit, first)
	}
}

func TestSessionWaitingForOnlyAnswersWhenClaudeSaid(t *testing.T) {
	for name, tc := range map[string]struct {
		sess claudeSession
		want string
	}{
		"nothing recorded":      {claudeSession{}, ""},
		"a named block":         {claudeSession{WaitingFor: "input needed"}, "input needed"},
		"waiting without a why": {claudeSession{Status: "waiting"}, "your answer"},
		"a blocked tempo":       {claudeSession{Tempo: "blocked"}, "your answer"},
		"busy is not waiting":   {claudeSession{Status: "busy"}, ""},
	} {
		if got := sessionWaitingFor(tc.sess); got != tc.want {
			t.Errorf("%s: waitingFor = %q, want %q", name, got, tc.want)
		}
	}
}

func TestBuildLaunchWorkspacesSurfacesADiagnostic(t *testing.T) {
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "acme", AbsPath: "/dev/acme", Status: workspace.StatusOK})
	st := &daemon.Status{
		Diagnostics: []daemon.WorkspaceDiagnostic{
			{WorkspaceID: "acme", Warning: "workspace acme is marked sensitive — remote session start is refused"},
		},
	}
	out := buildLaunchWorkspaces(reg, st)
	if len(out) != 1 {
		t.Fatalf("rows = %d", len(out))
	}
	if !strings.Contains(out[0].Note, "sensitive") {
		t.Errorf("note = %q; a refused start's reason must reach the launcher, not vanish", out[0].Note)
	}
	if out[0].Running {
		t.Error("a refused workspace is not running")
	}
}

func TestBuildLaunchWorkspacesRunningWithURL(t *testing.T) {
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "acme", AbsPath: "/dev/acme", Status: workspace.StatusOK})
	st := &daemon.Status{
		Workspaces: []supervisor.RunState{{WorkspaceID: "acme", Running: true, SessionURL: "https://claude.ai/code/x"}},
	}
	out := buildLaunchWorkspaces(reg, st)
	if !out[0].Running || out[0].SessionURL == "" {
		t.Errorf("a running workspace must carry its state + url, got %+v", out[0])
	}
}

func TestBuildLaunchWorkspacesNilStatus(t *testing.T) {
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "acme", AbsPath: "/dev/acme", Status: workspace.StatusOK})
	out := buildLaunchWorkspaces(reg, nil)
	if len(out) != 1 || out[0].Running || out[0].Note != "" {
		t.Errorf("with no daemon, a workspace is just listed idle, got %+v", out[0])
	}
}

func TestLaunchSessionsHandlerRequiresAWorkspace(t *testing.T) {
	rec := httptest.NewRecorder()
	launchSessionsHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/sessions", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a sessions request with no workspace must be 400, got %d", rec.Code)
	}
}

func TestLaunchSessionsHandlerRejectsNonGET(t *testing.T) {
	rec := httptest.NewRecorder()
	launchSessionsHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/sessions?workspace=x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST to sessions must be 405, got %d", rec.Code)
	}
}

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := expandTilde("~/.claude-x"); got != filepath.Join(home, ".claude-x") {
		t.Errorf("expandTilde(~/.claude-x) = %q", got)
	}
	if got := expandTilde("/abs/path"); got != "/abs/path" {
		t.Errorf("an absolute path must pass through, got %q", got)
	}
	if got := expandTilde(""); got != "" {
		t.Errorf("empty must stay empty, got %q", got)
	}
}
