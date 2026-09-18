package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestClaimsAreSharedAcrossWorktreesAndToldAtStart(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	wt := filepath.Join(repo, "corgi_services", ".worktrees", "APP-2")
	_ = os.MkdirAll(filepath.Dir(wt), 0o755)
	gitRun(t, repo, "worktree", "add", "-q", "-b", "corgi/APP-2", wt, "main")

	if got := sessions.CommonRoot(wt); !samePath(got, repo) {
		t.Fatalf("a worktree's common root is the repository: %q", got)
	}
	if got := workspaceRootOfDir(repo); !samePath(got, repo) {
		t.Fatalf("the repository's own: %q", got)
	}

	dir := t.TempDir()
	now := time.Now()
	_, _ = watch.LoadFileClaims(dir).Set(workspaceRootOfDir(repo), "s1", "api·auth", []string{"auth/refresh.go"}, now)
	st := sessions.State{Sessions: []sessions.Session{
		{ID: "s1", Label: "api", Display: "api·auth", Cwd: repo, Status: sessions.StatusWorking},
		{ID: "s2", Label: "api", Display: "api·APP-2", Cwd: wt, Status: sessions.StatusWorking},
	}}
	line := claimedHere(dir, st, "s2", repo, now)
	if !strings.Contains(line, "auth/refresh.go (api·auth)") {
		t.Fatalf("the newcomer is told: %q", line)
	}
	if claimedHere(dir, st, "s1", repo, now) != "" {
		t.Fatal("one's own claims are not news")
	}
	st.Sessions[0].Status = sessions.StatusGone
	if claimedHere(dir, st, "s2", repo, now) != "" {
		t.Fatal("a gone session holds nothing")
	}
}
