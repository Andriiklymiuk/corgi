package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestSavePromptIsReadOnceAndExpires(t *testing.T) {
	dir := t.TempDir()
	id, err := savePrompt(dir, "  fix the login loop\n")
	if err != nil {
		t.Fatal(err)
	}
	if !promptIDPattern.MatchString(id) {
		t.Fatalf("id %q", id)
	}
	info, err := os.Stat(filepath.Join(dir, "prompts", id))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("prompt file: %v %v", err, info.Mode())
	}
	text, err := takePrompt(dir, id)
	if err != nil || text != "fix the login loop" {
		t.Fatalf("take = %q, %v", text, err)
	}
	if _, err := takePrompt(dir, id); err == nil {
		t.Fatal("a prompt is read once")
	}
	if _, err := takePrompt(dir, "../../etc/passwd"); err == nil {
		t.Fatal("ids are validated")
	}

	stale := filepath.Join(dir, "prompts", "staleprompt01")
	os.WriteFile(stale, []byte("old"), 0o600)
	old := time.Now().Add(-promptMaxAge - time.Minute)
	os.Chtimes(stale, old, old)
	if _, err := takePrompt(dir, "staleprompt01"); err == nil {
		t.Fatal("an expired prompt is refused")
	}
	os.WriteFile(stale, []byte("old"), 0o600)
	os.Chtimes(stale, old, old)
	savePrompt(dir, "new")
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("saving sweeps expired prompts")
	}
}

func TestValidModel(t *testing.T) {
	for _, ok := range []string{"opus", "sonnet", "claude-opus-5", "claude-haiku-4-5-20251001", "opus[1m]"[:4]} {
		if !validModel(ok) {
			t.Errorf("%q should pass", ok)
		}
	}
	for _, bad := range []string{"", "opus; rm -rf /", "a b", "$(x)", "-opus"} {
		if validModel(bad) {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestLaunchNewKeepsThePromptOutOfTheShellLine(t *testing.T) {
	dir := phoneBoard(t, true)
	st := sessions.State{Windows: []sessions.Window{{ID: "win-1", Folders: []string{"/home/me/acme-api"}}}}
	raw, _ := json.Marshal(st)
	os.WriteFile(daemon.SessionsPath(dir), raw, 0o600)

	evil := "fix login'; rm -rf / #"
	rec := post(launchNewHandler, "/launch/new", `{"window":"win-1","prompt":`+strconvQuote(evil)+`,"model":"opus"}`)
	if rec.Code != 200 {
		t.Fatalf("new = %d: %s", rec.Code, rec.Body.String())
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	if len(entries) != 1 {
		t.Fatalf("spool has %d entries", len(entries))
	}
	spooled, _ := os.ReadFile(filepath.Join(dir, "commands", entries[0].Name()))
	line := string(spooled)
	if strings.Contains(line, "rm -rf") || strings.Contains(line, "fix login") {
		t.Fatalf("the prompt must travel by id, not in the command: %s", line)
	}
	if !strings.Contains(line, "agent claude --model opus --prompt-id ") || !strings.Contains(line, `"windowId":"win-1"`) {
		t.Fatalf("command line: %s", line)
	}
	prompts, _ := os.ReadDir(filepath.Join(dir, "prompts"))
	if len(prompts) != 1 {
		t.Fatalf("one saved prompt, got %d", len(prompts))
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "prompts", prompts[0].Name()))
	if string(saved) != evil {
		t.Fatalf("saved prompt = %q", saved)
	}

	cases := []struct {
		body string
		want int
	}{
		{`{"window":"win-9","prompt":"x"}`, 404},
		{`{"window":"win-1","model":"opus; ls"}`, 400},
		{`{"window":"win-1","profile":"nope"}`, 400},
		{`{"window":"","prompt":""}`, 200},
	}
	for _, c := range cases {
		if rec := post(launchNewHandler, "/launch/new", c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.body, rec.Code, c.want, rec.Body.String())
		}
	}
	rec = post(launchNewHandler, "/launch/new", `{}`)
	if rec.Code != 200 {
		t.Fatalf("an empty request opens a plain chat in the front window: %d", rec.Code)
	}
	if rec := post(launchNewHandler, "/launch/new", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("no body = %d", rec.Code)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
