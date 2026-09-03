package cmd

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func gitRepoOnBranch(t *testing.T, dir, branch string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--initial-branch=" + branch},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable or too old for %v: %v %s", args, err, out)
		}
	}
}

func TestDefaultSessionNameCarriesBranchAndClock(t *testing.T) {
	dir := t.TempDir()
	gitRepoOnBranch(t, dir, "fix/login-redirect")

	got := defaultSessionName("corgi", dir, "", time.Date(2026, 9, 3, 18, 55, 0, 0, time.UTC))

	if want := "corgi · fix/login-redirect · 18:55"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

func TestDefaultSessionNameNamesTheProfile(t *testing.T) {
	dir := t.TempDir()
	gitRepoOnBranch(t, dir, "main")

	got := defaultSessionName("corgi", dir, "work", time.Date(2026, 9, 3, 9, 2, 0, 0, time.UTC))

	if want := "corgi (work) · main · 09:02"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

func TestDefaultSessionNameDropsAnUnknownBranch(t *testing.T) {
	// Not a git checkout: the branch is the only part that may go missing.
	got := defaultSessionName("corgi", t.TempDir(), "", time.Date(2026, 9, 3, 18, 55, 0, 0, time.UTC))

	if want := "corgi · 18:55"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

func TestDefaultSessionNameKeepsIdAndClockWhenItMustShorten(t *testing.T) {
	dir := t.TempDir()
	gitRepoOnBranch(t, dir, "feature/"+strings.Repeat("long-", 12))

	got := defaultSessionName("corgi", dir, "", time.Date(2026, 9, 3, 18, 55, 0, 0, time.UTC))

	if n := len([]rune(got)); n > maxSessionNameLen {
		t.Errorf("name is %d runes (%q), want at most %d", n, got, maxSessionNameLen)
	}
	if !strings.HasPrefix(got, "corgi · feature/") || !strings.HasSuffix(got, " · 18:55") {
		t.Errorf("name = %q, want the id and the clock kept around a shortened branch", got)
	}
}

func TestDefaultSessionNameDropsTheBranchWhenThereIsNoRoom(t *testing.T) {
	dir := t.TempDir()
	gitRepoOnBranch(t, dir, "some-branch")
	id := strings.Repeat("w", 50)

	got := defaultSessionName(id, dir, "", time.Date(2026, 9, 3, 18, 55, 0, 0, time.UTC))

	if want := id + " · 18:55"; got != want {
		t.Errorf("name = %q, want %q — a branch stub identifies nothing", got, want)
	}
	if n := len([]rune(got)); n > maxSessionNameLen {
		t.Errorf("name is %d runes, want at most %d", n, maxSessionNameLen)
	}
}
