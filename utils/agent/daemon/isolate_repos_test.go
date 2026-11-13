package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestPullProjectsNamesTheRepoOfEveryPullRequestInTheRun(t *testing.T) {
	e := watch.Event{Kind: watch.KindPRComment, Ref: "acme/api#12", Riders: []watch.Event{{Ref: "acme/web#3"}, {Ref: "acme/api#12"}}}
	if got := pullProjects(e); strings.Join(got, ",") != "acme/api,acme/web" {
		t.Fatalf("got %v", got)
	}
	if got := pullProjects(watch.Event{Kind: watch.KindIssueNew, Ref: "acme/api#12"}); got != nil {
		t.Fatalf("a ticket names no repo yet: %v", got)
	}
	if got := pullProjects(watch.Event{Kind: watch.KindPRReview, Ref: "acme/api#12", Riders: []watch.Event{{Ref: "ABC-7"}}}); got != nil {
		t.Fatalf("one unknown ref means every repo: %v", got)
	}
}

func TestAReviewCommentRunMakesWorktreesOnlyInItsRepo(t *testing.T) {
	d := testDaemon(t)
	var mu sync.Mutex
	var scoped []string
	everyRepo := false
	d.Isolate = func(dir, branch string) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		everyRepo = true
		return []string{dir}, nil
	}
	d.IsolateRepos = func(dir, branch string, projects []string) ([]string, error) {
		mu.Lock()
		defer mu.Unlock()
		scoped = projects
		return []string{dir}, nil
	}
	var prompt string
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		mu.Lock()
		prompt = strings.Join(args, " ")
		mu.Unlock()
		return exec.CommandContext(ctx, "echo", `{"result":"Outcome: pushed","total_cost_usd":0.01}`)
	}
	t.Cleanup(func() { claudeCommand = prev })
	spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), AgentDir: d.Dir, Rules: watch.Rules{Enabled: true}, Action: "fix", Isolate: true, RunDB: "off"}
	d.Watches = []WatchSpec{spec}
	d.loadWatchFiles()
	d.fixBusy = map[string]chan struct{}{"acme": make(chan struct{}, 1)}
	e := watch.Event{Key: "gitlab:note:1", Workspace: "acme", Ref: "acme/core!9", Kind: watch.KindPRComment}
	d.runFix(context.Background(), spec, e)

	mu.Lock()
	defer mu.Unlock()
	if everyRepo || strings.Join(scoped, ",") != "acme/core" {
		t.Fatalf("only the pull request's repo: scoped=%v everyRepo=%v", scoped, everyRepo)
	}
	if !strings.Contains(prompt, "corgi worktree add <service>") {
		t.Fatalf("the run is told how to add another repo:\n%s", prompt)
	}
	log, _ := os.ReadFile(filepath.Join(d.Dir, "watch", "runs", safeName(e.Key)+".log"))
	for _, want := range []string{"=== making worktrees on corgi/acme-core!9 in acme/core", "=== worktrees ready in"} {
		if !strings.Contains(string(log), want) {
			t.Errorf("run log misses %q:\n%s", want, log)
		}
	}
}

func TestStuckWorktreeSetupGivesUpInsteadOfWaitingForever(t *testing.T) {
	d := testDaemon(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	d.Isolate = func(dir, branch string) ([]string, error) {
		<-release
		return nil, nil
	}
	start := time.Now()
	_, err := d.isolateWithin(t.TempDir(), "corgi/x", nil, 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "gave up") {
		t.Fatalf("want a give-up error, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("it waited far past the limit")
	}
}
