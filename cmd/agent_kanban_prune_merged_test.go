package cmd

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestPruneMergedWorktreesReleasesOnlyMergedIdleRuns(t *testing.T) {
	now := time.Now()
	fix := func(branch string) *CardFix { return &CardFix{Branch: branch, StartedAt: now.Add(-time.Hour)} }
	cards := []KanbanCard{
		{Key: "a", Ref: "api!1", Workspace: "ws", Fix: fix("corgi/merged"), Pull: &watch.PullStatus{State: "merged"}},
		{Key: "b", Ref: "api!2", Workspace: "ws", Fix: fix("corgi/open"), Pull: &watch.PullStatus{State: "opened"}},
		{Key: "c", Ref: "api!3", Workspace: "ws", Fix: fix("corgi/closed"), Pull: &watch.PullStatus{State: "closed"}},
		{Key: "d", Ref: "api!4", Workspace: "ws", Fix: &CardFix{Branch: "corgi/running", Running: true}, Pull: &watch.PullStatus{State: "merged"}},
		{Key: "e", Ref: "api!5", Workspace: "gone", Fix: fix("corgi/no-workspace"), Pull: &watch.PullStatus{State: "merged"}},
		{Key: "f", Ref: "api!6", Workspace: "ws", Pull: &watch.PullStatus{State: "merged"}},
	}
	var released []string
	release := func(dir, branch string) ([]string, []string, error) {
		released = append(released, dir+":"+branch)
		return []string{dir + "/.worktrees/x"}, nil, nil
	}
	root := func(id string) string {
		if id == "ws" {
			return "/ws"
		}
		return ""
	}

	out := pruneMergedWorktrees(cards, root, release, now)

	if len(released) != 1 || released[0] != "/ws:corgi/merged" {
		t.Fatalf("released %v, want only the merged, idle run", released)
	}
	if len(out) != 1 || !strings.Contains(out[0], "api!1") || !strings.Contains(out[0], "corgi/merged") {
		t.Errorf("report = %v", out)
	}
}

func TestPruneMergedWorktreesSaysNothingWhenNothingWasRemoved(t *testing.T) {
	now := time.Now()
	cards := []KanbanCard{{Key: "a", Ref: "api!1", Workspace: "ws", Fix: &CardFix{Branch: "corgi/merged"}, Pull: &watch.PullStatus{State: "merged"}}}
	release := func(string, string) ([]string, []string, error) { return nil, []string{"/ws/dirty"}, nil }

	if out := pruneMergedWorktrees(cards, func(string) string { return "/ws" }, release, now); len(out) != 0 {
		t.Errorf("report = %v, want none for a kept worktree", out)
	}
}
