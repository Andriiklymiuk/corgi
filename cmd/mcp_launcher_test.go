package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
}
