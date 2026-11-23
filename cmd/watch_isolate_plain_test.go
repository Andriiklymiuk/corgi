package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestARunInAPlainRepoGetsAWorktreeOfItsOwn(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"}, {"commit", "-q", "--allow-empty", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	dirs, err := isolateFixWorktrees(repo, "corgi/TASK-3")
	if err != nil || len(dirs) != 1 {
		t.Fatalf("one worktree for a repo with no compose file: %v %v", dirs, err)
	}
	real, _ := filepath.EvalSymlinks(repo)
	if !strings.HasPrefix(dirs[0], utils.AgentWorktreeBase(real)) && !strings.HasPrefix(dirs[0], utils.AgentWorktreeBase(repo)) {
		t.Fatalf("it lives where release looks: %s", dirs[0])
	}
	if head, _ := exec.Command("git", "-C", dirs[0], "rev-parse", "--abbrev-ref", "HEAD").Output(); strings.TrimSpace(string(head)) != "corgi/TASK-3" {
		t.Fatalf("on the run's branch: %s", head)
	}
	again, err := isolateFixWorktrees(repo, "corgi/TASK-3")
	if err != nil || len(again) != 1 || again[0] != dirs[0] {
		t.Fatalf("a follow-up run lands in the same place: %v %v", again, err)
	}
	removed, kept, err := releaseFixWorktrees(repo, "corgi/TASK-3")
	if err != nil || len(removed) != 1 || len(kept) != 0 {
		t.Fatalf("undo and prune release it: %v %v %v", removed, kept, err)
	}
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Fatal("gone")
	}
	if _, err := isolateFixWorktrees(filepath.Join(t.TempDir(), "nothing"), "corgi/x"); err == nil {
		t.Fatal("a folder with no repository is still refused")
	}
}

func TestACardWhoseRunFailedStopsSayingRunning(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	tasks := watch.LoadTasks(dir)
	if _, err := tasks.Add("history filter", "", "corgi", "cli", now); err != nil {
		t.Fatal(err)
	}
	_, _ = tasks.Started("TASK-1", now)
	fixes := watch.LoadFixLog(dir)
	e := watch.Event{Key: "task:1", Ref: "TASK-1", Workspace: "corgi", Kind: watch.KindTask, At: now}
	fixes.StartFor(e, now.Add(-time.Minute))
	fixes.Finish("task:1", nil, "", "could not create worktrees: no repository", now.Add(-time.Minute))
	cards := buildKanban(kanbanInputs{events: tasks.Events(now), moved: watch.LoadStateLog(dir), fixes: fixes, picks: watch.LoadPicks(dir), pulls: watch.LoadPullLog(dir), now: now})
	if len(cards) != 1 || cards[0].Column != ColInbox || !strings.HasPrefix(cards[0].Why, "the last run failed") || !strings.Contains(cards[0].Why, "no repository") || cards[0].Now != nil {
		t.Fatalf("a dead run is not Running: %+v", cards)
	}
}

func TestAPullRequestOfAnotherRepoGetsNoWorktreeOfThisOne(t *testing.T) {
	repo := t.TempDir()
	newRepo(t, repo)
	trees, err := isolateFixWorktreesIn(repo, "corgi/acme-api-1", []string{"acme/api"})
	if err != nil || len(trees) != 1 {
		t.Fatalf("a repo with no origin cannot say it is another one: %v %v", trees, err)
	}
	gitIn(t, repo, "remote", "add", "origin", "git@github.com:Acme/Web.git")
	if trees, err := isolateFixWorktreesIn(repo, "corgi/acme-api-2", []string{"acme/api"}); err != nil || trees != nil {
		t.Fatalf("acme/api is not here: %v %v", trees, err)
	}
	if trees, err := isolateFixWorktreesIn(repo, "corgi/acme-web-3", []string{"acme/web"}); err != nil || len(trees) != 1 {
		t.Fatalf("acme/web is: %v %v", trees, err)
	}
}

func TestAPullRequestOfTheWorkspaceRepoItselfGetsItsWorktree(t *testing.T) {
	ws := t.TempDir()
	newRepo(t, ws)
	gitIn(t, ws, "remote", "add", "origin", "https://github.com/acme/workspace.git")
	api := filepath.Join(ws, "api")
	newRepo(t, api)
	gitIn(t, api, "remote", "add", "origin", "https://github.com/acme/api.git")
	if err := os.WriteFile(filepath.Join(ws, "corgi-compose.yml"), []byte("name: acme\nservices:\n  api:\n    path: ./api\n    port: 4500\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := func(dir, file string) bool {
		_, err := os.Stat(filepath.Join(dir, file))
		return err == nil
	}
	root, err := isolateFixWorktreesIn(ws, "corgi/acme-workspace-1", []string{"acme/workspace"})
	if err != nil || len(root) != 1 || !head(root[0], "f.txt") || strings.Contains(root[0], "api-") {
		t.Fatalf("the workspace's own repo: %v %v", root, err)
	}
	svc, err := isolateFixWorktreesIn(ws, "corgi/acme-api-1", []string{"acme/api"})
	if err != nil || len(svc) != 1 {
		t.Fatalf("a service: %v %v", svc, err)
	}
	if none, err := isolateFixWorktreesIn(ws, "corgi/other-1", []string{"other/repo"}); err != nil || none != nil {
		t.Fatalf("neither: no worktree of every repository: %v %v", none, err)
	}
	if all, err := isolateFixWorktreesIn(ws, "corgi/abc-1", nil); err != nil || len(all) != 1 {
		t.Fatalf("a ticket still gets every service: %v %v", all, err)
	}
}
