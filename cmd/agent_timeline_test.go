package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// The timeline joins the transcript, the board and the watch into one
// ordered story: prompts, tool bursts, tests, the pull request's standing
// and what was said on it.
func TestTimelineJoinsTheBooksInOrder(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	path := filepath.Join(home, "s1.jsonl")
	_ = os.WriteFile(path, []byte(`{"type":"user","uuid":"u1","timestamp":"2026-09-14T10:00:00Z","message":{"role":"user","content":"Fix the login loop"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-09-14T10:00:10Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/w/api/auth/refresh.go"}}]}}
{"type":"assistant","uuid":"a2","timestamp":"2026-09-14T10:00:20Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t2","name":"Edit","input":{"file_path":"/w/api/auth/refresh.go"}}]}}
{"type":"assistant","uuid":"a3","timestamp":"2026-09-14T10:01:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Found it: a grace window fixes it."}]}}
`), 0o600)
	prev := transcriptPathFor
	defer func() { transcriptPathFor = prev }()
	transcriptPathFor = func(sessions.Session) string { return path }

	pr := "https://github.com/acme/api/pull/7"
	pulls := watch.LoadPullLog(dir)
	_ = pulls.Set("acme/api#7", watch.PullStatus{State: "open", Checks: "passing", Review: "approved", At: time.Date(2026, 9, 14, 10, 30, 0, 0, time.UTC)})
	ev := watch.Event{Key: "gh:acme/api#7:r1", Ref: "acme/api#7", Kind: watch.KindPRReview, Author: "dan", Body: "Looks right.\nShip it.", At: time.Date(2026, 9, 14, 10, 20, 0, 0, time.UTC), Workspace: "api", URL: pr}
	_ = os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	line, _ := json.Marshal(ev)
	_ = os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), append(line, '\n'), 0o600)
	_ = watch.LoadHands(dir).Set(ev.Key, watch.Hand{At: time.Date(2026, 9, 14, 10, 21, 0, 0, time.UTC), To: "s1", By: "daemon"})

	s := sessions.Session{ID: "s1", Label: "api", Display: "api·auth", Cwd: "/w/api", StartedAt: time.Date(2026, 9, 14, 9, 59, 0, 0, time.UTC), PR: pr,
		Tests: &sessions.TestRun{OK: true, Cmd: "go test ./...", At: time.Date(2026, 9, 14, 10, 5, 0, 0, time.UTC)}}
	items := timelineFor(dir, s, 0)
	var kinds []string
	for _, it := range items {
		kinds = append(kinds, it.Kind)
	}
	want := []string{"start", "prompt", "tools", "said", "tests", "review", "handed", "pr"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v, want %v\n%+v", kinds, want, items)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v, want %v", kinds, want)
		}
	}
	if items[2].N != 2 || items[2].Text != "Read api/auth/refresh.go · Edit api/auth/refresh.go" {
		t.Fatalf("the tool burst: %+v", items[2])
	}
	if items[5].Text != "review by dan: Looks right. Ship it." || items[7].Text != "pull request open · ready to merge · checks ✓ · approved" {
		t.Fatalf("words: %q / %q", items[5].Text, items[7].Text)
	}
	if items[4].OK == nil || !*items[4].OK {
		t.Fatal("tests carry their outcome")
	}
	if got := timelineFor(dir, s, 3); len(got) != 3 || got[0].Kind != "review" {
		t.Fatalf("max keeps the newest: %+v", got)
	}
}
