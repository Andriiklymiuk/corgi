package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestGuardVerdictReadsTheSessionOnTheBranch(t *testing.T) {
	st := sessions.State{Sessions: []sessions.Session{
		{ID: "s1", Display: "api·APP-1", Cwd: "/w/api", Branch: "corgi/APP-1", Gate: &sessions.GateRun{OK: false, Cmd: "go test ./...", Fails: 2}},
		{ID: "s2", Display: "api·APP-2", Cwd: "/w/api", Branch: "corgi/APP-2", Tests: &sessions.TestRun{OK: false, Cmd: "bun test"}},
		{ID: "s3", Display: "api·APP-3", Cwd: "/w/api/corgi_services/.worktrees/APP-3", Branch: "corgi/APP-3", Gate: &sessions.GateRun{OK: true, Cmd: "go test ./..."}},
	}}
	if ok, why := guardVerdict(st, "/w/api", "corgi/APP-1"); ok || !strings.Contains(why, "go test ./...") {
		t.Fatalf("a red gate refuses the push: %v %q", ok, why)
	}
	if ok, why := guardVerdict(st, "/w/api", "corgi/APP-2"); ok || !strings.Contains(why, "bun test") {
		t.Fatalf("red tests refuse the push: %v %q", ok, why)
	}
	if ok, _ := guardVerdict(st, "/w/api", "corgi/APP-3"); !ok {
		t.Fatal("a green gate passes")
	}
	if ok, _ := guardVerdict(st, "/w/api", "main"); !ok {
		t.Fatal("no session on the branch: nothing to say")
	}
	if ok, _ := guardVerdict(st, "/elsewhere", "corgi/APP-1"); !ok {
		t.Fatal("another repository's session does not count")
	}
}

func TestGuardInstallsAPrePushHookOnce(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	path, err := installGuard(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), guardMarker) || !strings.Contains(string(data), "corgi agent guard check") {
		t.Fatalf("hook:\n%s", data)
	}
	if info, _ := os.Stat(path); info.Mode()&0o100 == 0 {
		t.Fatal("the hook is executable")
	}
	if _, err := installGuard(repo, false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(path, []byte("#!/bin/sh\necho mine\n"), 0o755)
	if _, err := installGuard(repo, false); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("another hook: %v", err)
	}
	if _, err := installGuard(repo, true); err != nil {
		t.Fatal(err)
	}
	if err := uninstallGuard(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", "pre-push")); !os.IsNotExist(err) {
		t.Fatal("gone after uninstall")
	}
}
