package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestAPullRequestGoesToTheWorkspaceThatListsItsRepo(t *testing.T) {
	gh := []watch.Source{namedSource{name: "github"}}
	prs := watch.Rules{Enabled: true, PRs: true}
	d := testDaemon(t)
	owner := WatchSpec{Workspace: "acme", Repos: []string{"acme/api"}, Sources: gh, Rules: prs}
	anyRepo := WatchSpec{Workspace: "any", Sources: gh, Rules: prs}
	d.Watches = []WatchSpec{anyRepo, owner}
	e := watch.Event{Key: "github:1", Source: "github", Kind: watch.KindPRReview, Ref: "acme/api#1", Mine: true, Body: "please fix"}
	if !d.ownedElsewhere(anyRepo, e) {
		t.Fatal("the workspace that watches every repo leaves it to the one that lists this one")
	}
	if d.ownedElsewhere(owner, e) {
		t.Fatal("its owner keeps it")
	}
	stranger := e
	stranger.Ref = "stranger/repo#1"
	if d.ownedElsewhere(anyRepo, stranger) {
		t.Fatal("a repo nobody lists is still taken")
	}
	ticket := watch.Event{Source: "linear", Kind: watch.KindIssueNew, Ref: "ABC-1"}
	if d.ownedElsewhere(anyRepo, ticket) {
		t.Fatal("tickets are not routed by repo")
	}
	asked := e
	asked.Kind = watch.KindReviewRequested
	if d.ownedElsewhere(anyRepo, asked) {
		t.Fatal("the owner takes no review requests, so a catch-all may")
	}
	d.Watches[1].Sources = nil
	if d.ownedElsewhere(anyRepo, e) {
		t.Fatal("an owner that does not watch github would never see it")
	}
	d.Watches[1] = owner
	d.Watches[1].Rules = watch.Rules{}
	if d.ownedElsewhere(anyRepo, e) {
		t.Fatal("a watch that is off owns nothing")
	}
}

func TestARunOnARepoThatIsNotHereGetsNoWorktree(t *testing.T) {
	d := testDaemon(t)
	d.Isolate = func(dir, _ string) ([]string, error) { return []string{dir}, nil }
	d.IsolateRepos = func(string, string, []string) ([]string, error) { return nil, nil }
	stacks := 0
	d.RunStack = func(string, string, string) string { stacks++; return "" }
	var mu sync.Mutex
	var prompt string
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		mu.Lock()
		prompt = strings.Join(args, " ")
		mu.Unlock()
		return exec.CommandContext(ctx, "echo", `{"result":"Outcome: pushed","total_cost_usd":0.01}`)
	}
	t.Cleanup(func() { claudeCommand = prev })
	spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), AgentDir: d.Dir, Rules: watch.Rules{Enabled: true}, Action: "fix", Isolate: true}
	d.Watches = []WatchSpec{spec}
	d.loadWatchFiles()
	d.fixBusy = map[string]chan struct{}{"acme": make(chan struct{}, 1)}
	e := watch.Event{Key: "github:7", Workspace: "acme", Ref: "other/repo#9", Kind: watch.KindPRReview}
	d.runFix(context.Background(), spec, e)
	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{"other/repo", "not a repository of this workspace", "Clone it into a new folder"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the run is told %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "has a worktree") || stacks != 0 {
		t.Errorf("no worktree and no stack of another repo: %d", stacks)
	}
	log, _ := os.ReadFile(filepath.Join(d.Dir, "watch", "runs", safeName(e.Key)+".log"))
	if !strings.Contains(string(log), "=== no worktree: other/repo is not a repository of this workspace") {
		t.Errorf("the log says why:\n%s", log)
	}
}

func TestARetroLeavesNothingOnThePersonsList(t *testing.T) {
	d := testDaemon(t)
	d.loadWatchFiles()
	k, _ := watch.CatalogKind("retro")
	spec := WatchSpec{Workspace: "acme"}
	e := watch.Event{Key: "routine:acme:retro:1", Workspace: "acme", Ref: "routine/retro", Title: "retro", Body: k.Prompt, Kind: watch.KindRoutine}
	d.watchState.Fixes.StartFor(e, e.At)
	d.fixDone(context.Background(), fixRun{spec: spec, e: e, out: "Outcome: nothing\n\n**4 runs, nothing worth a task.**\n\n## You do next\n- Press Work.\n\nNo database was up.\n"})
	for _, r := range d.watchState.Fixes.Records() {
		if len(r.Left) != 0 || len(r.Gaps) != 0 {
			t.Errorf("a report leaves no list and skipped no check: %v %v", r.Left, r.Gaps)
		}
		if r.Note != "4 runs, nothing worth a task." {
			t.Errorf("its note is its headline: %q", r.Note)
		}
	}
}
