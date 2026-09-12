package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/bots"
)

// The phone lists the bots without their souls, and opening one puts
// --bot on the new session's command line; a name nobody defined is 404.
func TestBotsReachThePhoneAndTheNewSessionLine(t *testing.T) {
	dir := phoneBoard(t, true)
	store, _ := bots.Load(bots.Path(dir))
	store.Put(bots.Bot{Name: "reviewer", Title: "Code Reviewer", Workspace: "api", Soul: "You review pull requests. Never merge.", Model: "opus", Color: "orange"})
	if err := bots.Save(bots.Path(dir), store); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	launchBotsHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/bots", nil))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "Never merge") {
		t.Fatalf("bots: %d %s", rec.Code, rec.Body)
	}
	var got struct {
		Bots []map[string]any `json:"bots"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Bots) != 1 || got.Bots[0]["title"] != "Code Reviewer" || got.Bots[0]["hasSoul"] != true || got.Bots[0]["color"] != "orange" {
		t.Fatalf("the phone sees the bot: %+v", got.Bots)
	}

	rec = post(launchNewHandler, "/launch/new", `{"bot":"reviewer"}`)
	if rec.Code != 200 {
		t.Fatalf("open: %d %s", rec.Code, rec.Body)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	spooled, _ := os.ReadFile(filepath.Join(dir, "commands", entries[len(entries)-1].Name()))
	if !strings.Contains(string(spooled), "agent claude --bot reviewer") {
		t.Fatalf("the session opens as the bot: %s", spooled)
	}
	if rec := post(launchNewHandler, "/launch/new", `{"bot":"nobody"}`); rec.Code != 404 {
		t.Fatalf("unknown bot: %d", rec.Code)
	}
	if rec := post(launchNewHandler, "/launch/new", `{"bot":"Bad Name"}`); rec.Code != 400 {
		t.Fatalf("bad name: %d", rec.Code)
	}
}

// Resuming needs the transcript to still be there under the account the
// launch runs as.
func TestResumeOnlyWhenTheTranscriptExists(t *testing.T) {
	cfg := t.TempDir()
	cwd := "/home/me/acme-api"
	if transcriptExists(map[string]string{"CLAUDE_CONFIG_DIR": cfg}, cwd, "sess-1") {
		t.Fatal("nothing there yet")
	}
	p := filepath.Join(cfg, "projects", "-home-me-acme-api", "sess-1.jsonl")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte("{}\n"), 0o600)
	if !transcriptExists(map[string]string{"CLAUDE_CONFIG_DIR": cfg}, cwd, "sess-1") {
		t.Fatal("the transcript is there")
	}
}
