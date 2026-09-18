package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils/agent/workspace"
)

func TestTheStackAnswersOnlyWithACompose(t *testing.T) {
	dir := phoneBoard(t, true)
	plain, stack := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(stack, "corgi-compose.yml"), []byte("services:\n  api:\n    path: api\n    port: 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "plain", AbsPath: plain, Status: workspace.StatusOK})
	reg.Upsert(workspace.Workspace{ID: "stack", AbsPath: stack, Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	launchStackHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/stack?workspace=plain", nil))
	var got struct {
		Compose  bool           `json:"compose"`
		Services []StackService `json:"services"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || got.Compose || got.Services == nil {
		t.Fatalf("plain: %d %s", rec.Code, rec.Body)
	}
	if rec := httptest.NewRecorder(); true {
		launchStackHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/stack?workspace=nope", nil))
		if rec.Code != 404 {
			t.Fatalf("unknown: %d", rec.Code)
		}
	}
	if rec := post(launchStackHandler, "/launch/stack", `{"workspace":"stack","do":"run","services":["api; rm -rf /"]}`); rec.Code != 400 {
		t.Fatalf("a shell in a name: %d %s", rec.Code, rec.Body)
	}
	if rec := post(launchStackHandler, "/launch/stack", `{"workspace":"plain","do":"run"}`); rec.Code != 400 {
		t.Fatalf("no compose, no run: %d", rec.Code)
	}
	if rec := post(launchStackHandler, "/launch/stack", `{"workspace":"stack","do":"dance"}`); rec.Code != 400 {
		t.Fatalf("unknown verb: %d", rec.Code)
	}
	if composePathIn(stack) == "" || composePathIn(plain) != "" {
		t.Fatal("composePathIn")
	}
}
