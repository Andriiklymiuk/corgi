package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestWatchSinkNotifiesAndFixesOnce(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	var ran []string
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		ran = append(ran, dir+" "+strings.Join(args, " "))
		return exec.CommandContext(ctx, "echo", "opened https://github.com/acme/api/pull/412")
	}
	defer func() {
		claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, "claude", args...)
			cmd.Dir = dir
			return cmd
		}
	}()
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), Project: "ABC", Rules: watch.Rules{Enabled: true}, Action: "fix", SkipPermissions: true}}
	d.startWatches(context.Background())

	e := watch.Event{Key: "linear:ABC-1", Source: "linear", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "Login loops", Mine: true, At: time.Now()}
	d.handleWatchEvent(context.Background(), e)
	d.handleWatchEvent(context.Background(), e)

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case b := <-notes:
			got[b] = true
		case <-time.After(3 * time.Second):
			t.Fatalf("waiting for notices, have %v", got)
		}
	}
	if !got["new issue ABC-1 — Login loops"] || !got["fixed ABC-1 — https://github.com/acme/api/pull/412"] {
		t.Fatalf("notices %v", got)
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "--dangerously-skip-permissions") || !strings.Contains(ran[0], "/corgi:stories ABC-1") {
		t.Fatalf("claude runs %v", ran)
	}
	if data, _ := os.ReadFile(filepath.Join(d.Dir, "watch", "events.jsonl")); strings.Count(string(data), "\n") != 1 {
		t.Fatalf("events log: %q", data)
	}
	if logs, _ := filepath.Glob(filepath.Join(d.Dir, "watch", "runs", "*.log")); len(logs) != 1 {
		t.Fatalf("run logs %v", logs)
	}
}

func TestWatchRoutesByProjectAndRepo(t *testing.T) {
	d := testDaemon(t)
	d.Notify = func(_, _ string) {}
	d.Watches = []WatchSpec{
		{Workspace: "web", Dir: t.TempDir(), Repos: []string{"acme/web"}, Rules: watch.Rules{Enabled: true, PRs: true}},
		{Workspace: "api", Dir: t.TempDir(), Project: "API", Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true}},
	}
	d.startWatches(context.Background())
	d.handleWatchEvent(context.Background(), watch.Event{Key: "github:acme/api#3:c1", Kind: watch.KindPRComment, Ref: "acme/api#3", Mine: true})
	d.handleWatchEvent(context.Background(), watch.Event{Key: "linear:API-4", Kind: watch.KindIssueNew, Ref: "API-4", Mine: true})
	data, _ := os.ReadFile(filepath.Join(d.Dir, "watch", "events.jsonl"))
	if !strings.Contains(string(data), `"workspace":"api"`) || strings.Contains(string(data), `"workspace":"web"`) {
		t.Fatalf("routed wrong: %s", data)
	}
	if d.WatchIdentity("github") != "" {
		t.Fatal("no identity before a poll")
	}
}

func TestFixPromptPerKind(t *testing.T) {
	if p := fixPrompt(watch.Event{Kind: watch.KindPRReview, URL: "https://github.com/a/b/pull/1"}); !strings.Contains(p, "/corgi:review https://github.com/a/b/pull/1") {
		t.Fatal(p)
	}
	if p := fixPrompt(watch.Event{Kind: watch.KindIssueComment, Ref: "ABC-2", Author: "Max", Body: "also X"}); !strings.Contains(p, "Max") || !strings.Contains(p, "/corgi:stories ABC-2") {
		t.Fatal(p)
	}
	if b := watchBody(watch.Event{Kind: watch.KindPRReview, Ref: "a/b#1", State: "approved"}); b != "someone reviewed a/b#1: approved" {
		t.Fatal(b)
	}
}
