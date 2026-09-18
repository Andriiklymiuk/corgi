package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

func TestLaunchRegistersAWorkspace(t *testing.T) {
	phoneBoard(t, true)
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git"), 0o700)

	rec := post(launchWorkspacesHandler, "/launch/workspaces", `{"path":"`+repo+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var out struct{ ID, Path string }
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ID != filepath.Base(repo) || out.Path != repo {
		t.Fatalf("got %+v", out)
	}
	registry, _, _ := agentRegistry()
	if ws, ok := registry.Find(out.ID); !ok || ws.AbsPath != repo || ws.Status != workspace.StatusOK {
		t.Fatalf("registry: %+v %v", ws, ok)
	}
	if _, err := os.Stat(filepath.Join(repo, ".corgi", "agent.yml")); err != nil {
		t.Fatalf("no repo file: %v", err)
	}

	rec = httptest.NewRecorder()
	launchWorkspacesHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/workspaces", nil))
	if !strings.Contains(rec.Body.String(), `"path":"`+repo+`"`) {
		t.Fatalf("list: %s", rec.Body.String())
	}

	plain := t.TempDir()
	for _, body := range []string{
		`{"path":"` + plain + `"}`,
		`{"path":"relative/dir"}`,
		`{"path":"` + plain + `","id":"` + out.ID + `"}`,
	} {
		if rec := post(launchWorkspacesHandler, "/launch/workspaces", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestLaunchAddsAProfile(t *testing.T) {
	phoneBoard(t, true)
	rec := post(launchProfilesHandler, "/launch/profiles", `{"name":"work","configDir":"~/.claude-work"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	launchProfilesHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/profiles", nil))
	if !strings.Contains(rec.Body.String(), `{"name":"work","configDir":"~/.claude-work"}`) {
		t.Fatalf("list: %s", rec.Body.String())
	}
	if got := launchProfileNames(); len(got) != 1 || got[0] != "work" {
		t.Fatalf("names: %v", got)
	}
	for _, body := range []string{`{"name":"no spaces","configDir":"~/.x"}`, `{"name":"empty"}`} {
		if rec := post(launchProfilesHandler, "/launch/profiles", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestLaunchTicketByRef(t *testing.T) {
	dir := phoneBoard(t, true)
	tasks := watch.LoadTasks(dir)
	task, err := tasks.Add("Write the docs", "", "api", "bar", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := post(launchTicketHandler, "/launch/ticket", `{"ref":"`+task.Ref()+`","workspace":"api","do":"move","status":"Doing"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"Doing"`) {
		t.Fatalf("move by ref: %d %s", rec.Code, rec.Body.String())
	}
	rec = post(launchTicketHandler, "/launch/ticket", `{"ref":"`+strings.ToLower(task.Ref())+`","do":"move","status":"Done"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("move by lower-case ref, no workspace: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(launchTicketHandler, "/launch/ticket", `{"ref":"TASK-99","do":"move","status":"Done"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown ref: %d", rec.Code)
	}
	if rec := post(launchTicketHandler, "/launch/ticket", `{"do":"move","status":"Done"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("no key, no ref: %d", rec.Code)
	}
}
