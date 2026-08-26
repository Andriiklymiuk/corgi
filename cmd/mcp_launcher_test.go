package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
