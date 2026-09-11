package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
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

func TestWatchEventsAndWorkingOnOne(t *testing.T) {
	dir := phoneBoard(t, true)
	st := sessions.State{Windows: []sessions.Window{{ID: "win-1", Folders: []string{"/home/me/acme-api"}}}}
	raw, _ := json.Marshal(st)
	os.WriteFile(daemon.SessionsPath(dir), raw, 0o600)

	os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	lines := []watch.Event{
		{Key: "jira:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "Login loops\nsecond line", URL: "https://acme.atlassian.net/browse/ABC-1", Workspace: "api"},
		{Key: "gitlab:acme/api!7", Kind: watch.KindPRReview, Ref: "acme/api!7", Title: "cover the nil case", URL: "https://gitlab.com/acme/api/-/merge_requests/7"},
		{Key: "jira:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "Login loops", URL: "https://acme.atlassian.net/browse/ABC-1"},
	}
	var buf bytes.Buffer
	for _, e := range lines {
		data, _ := json.Marshal(e)
		buf.Write(append(data, '\n'))
	}
	buf.WriteString("{ not json\n")
	os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), buf.Bytes(), 0o600)

	rec := httptest.NewRecorder()
	launchEventsHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/events", nil))
	if rec.Code != 200 {
		t.Fatalf("events = %d: %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Events []struct {
			Key, Kind, Ref, Title, URL string
			Actionable                 bool
		} `json:"events"`
	}
	json.Unmarshal(rec.Body.Bytes(), &listed)
	if len(listed.Events) != 2 {
		t.Fatalf("deduped by key, bad lines skipped: %+v", listed.Events)
	}
	// One queue, ordered by who is stuck: someone replying on a pull request
	// of mine outranks a fresh ticket that blocks nobody yet, whatever
	// arrived most recently.
	if listed.Events[0].Key != "gitlab:acme/api!7" || listed.Events[1].Key != "jira:ABC-1" {
		t.Errorf("whoever is blocked leads, got %q then %q", listed.Events[0].Key, listed.Events[1].Key)
	}
	if listed.Events[1].Title != "Login loops" {
		t.Errorf("the title is one line, got %q", listed.Events[1].Title)
	}
	if !listed.Events[0].Actionable || !listed.Events[1].Actionable {
		t.Error("both kinds have a prompt, so both are actionable")
	}

	rec = httptest.NewRecorder()
	launchEventsHandler(rec, httptest.NewRequest(http.MethodPost, "/launch/events", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", rec.Code)
	}

	if rec := post(launchWorkOnHandler, "/launch/work-on", `{"key":"jira:ABC-1","window":"win-1","model":"opus"}`); rec.Code != 200 {
		t.Fatalf("work-on = %d: %s", rec.Code, rec.Body.String())
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	if len(entries) != 1 {
		t.Fatalf("spool has %d entries", len(entries))
	}
	spooled, _ := os.ReadFile(filepath.Join(dir, "commands", entries[0].Name()))
	line := string(spooled)
	if strings.Contains(line, "corgi:stories") || strings.Contains(line, "Login loops") {
		t.Fatalf("the prompt must travel by id, not in the command: %s", line)
	}
	// The ref itself does ride along, so the board can put the session on
	// the ticket from its first event.
	if !strings.Contains(line, "--model opus --ticket ABC-1 --ticket-key jira:ABC-1 --prompt-id ") {
		t.Fatalf("command line: %s", line)
	}
	prompts, _ := os.ReadDir(filepath.Join(dir, "prompts"))
	saved, _ := os.ReadFile(filepath.Join(dir, "prompts", prompts[0].Name()))
	if !strings.Contains(string(saved), "/corgi:stories ABC-1") {
		t.Errorf("a new issue hands off to the stories skill, got %q", saved)
	}

	for _, c := range []struct {
		body string
		want int
	}{
		{`{"key":"nope"}`, 404},
		{`{"key":"jira:ABC-1","window":"win-9"}`, 404},
		{`{"key":"jira:ABC-1","model":"opus; ls"}`, 400},
		{`{"key":"jira:ABC-1","profile":"nope"}`, 400},
	} {
		if rec := post(launchWorkOnHandler, "/launch/work-on", c.body); rec.Code != c.want {
			t.Errorf("%s: %d, want %d: %s", c.body, rec.Code, c.want, rec.Body.String())
		}
	}
}

func TestWorkingOnSeveralIssuesAtOnce(t *testing.T) {
	dir := phoneBoard(t, true)
	os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	events := []watch.Event{
		{Key: "jira:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "one", Workspace: "api"},
		{Key: "jira:ABC-2", Kind: watch.KindIssueNew, Ref: "ABC-2", Title: "two", Workspace: "api"},
		{Key: "jira:ZZZ-9", Kind: watch.KindIssueNew, Ref: "ZZZ-9", Title: "elsewhere", Workspace: "web"},
		{Key: "gitlab:acme/api!7", Kind: watch.KindPRReview, Ref: "acme/api!7", URL: "https://gitlab.com/acme/api/-/merge_requests/7", Workspace: "api"},
	}
	var buf bytes.Buffer
	for _, e := range events {
		data, _ := json.Marshal(e)
		buf.Write(append(data, '\n'))
	}
	os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), buf.Bytes(), 0o600)

	if rec := post(launchWorkOnHandler, "/launch/work-on", `{"keys":["jira:ABC-1","jira:ABC-2"]}`); rec.Code != 200 {
		t.Fatalf("batch = %d: %s", rec.Code, rec.Body.String())
	}
	prompts, _ := os.ReadDir(filepath.Join(dir, "prompts"))
	if len(prompts) != 1 {
		t.Fatalf("one saved prompt, got %d", len(prompts))
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "prompts", prompts[0].Name()))
	if !strings.Contains(string(saved), "/corgi:stories ABC-1 ABC-2") {
		t.Errorf("a batch is one stories run, got %q", saved)
	}

	for _, c := range []struct {
		body, why string
		want      int
	}{
		{`{"keys":["jira:ABC-1","jira:ZZZ-9"]}`, "two workspaces cannot share a checkout", 400},
		{`{"keys":["jira:ABC-1","gitlab:acme/api!7"]}`, "a review comment does not batch", 409},
		{`{"keys":[]}`, "nothing named", 400},
		{`{"keys":["jira:ABC-1","jira:ABC-1","jira:ABC-1","jira:ABC-1","jira:ABC-1","jira:ABC-1","jira:ABC-1","jira:ABC-1","jira:ABC-1","jira:ABC-1","jira:ABC-1"]}`, "over the cap", 400},
	} {
		if rec := post(launchWorkOnHandler, "/launch/work-on", c.body); rec.Code != c.want {
			t.Errorf("%s (%s): %d, want %d: %s", c.body, c.why, rec.Code, c.want, rec.Body.String())
		}
	}

	// One key in the list is still the single-event prompt.
	if rec := post(launchWorkOnHandler, "/launch/work-on", `{"keys":["gitlab:acme/api!7"]}`); rec.Code != 200 {
		t.Fatalf("single review = %d: %s", rec.Code, rec.Body.String())
	}
}
