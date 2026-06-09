package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeFakeBin drops a shell script named `name` on a temp dir prepended to PATH.
func writeFakeBin(t *testing.T, dir, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-bin probe test is POSIX-only")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestProbeAgentWork_BranchAndGithubPR(t *testing.T) {
	bin := t.TempDir()
	// git: respond to the two reads the probe makes.
	writeFakeBin(t, bin, "git", `
case "$*" in
  *"rev-parse --abbrev-ref HEAD"*) echo "feature/login" ;;
  *"rev-parse --git-dir"*)         echo ".git" ;;
  *"status --porcelain"*)          echo " M file.go" ;;
  *) echo "" ;;
esac`)
	// gh: emit the JSON the probe asks for.
	writeFakeBin(t, bin, "gh", `
echo '{"number":42,"state":"OPEN","isDraft":true,"url":"https://x/pull/42","statusCheckRollup":[{"conclusion":"SUCCESS"}]}'`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	repo := t.TempDir()
	aw := ProbeAgentWork(repo)
	if aw == nil {
		t.Fatal("expected agent work, got nil")
	}
	if aw.Branch != "feature/login" {
		t.Errorf("branch = %q", aw.Branch)
	}
	if !aw.Dirty {
		t.Error("expected dirty tree")
	}
	if aw.PR == nil {
		t.Fatal("expected a PR")
	}
	if aw.PR.State != "open" || !aw.PR.Draft || aw.PR.Number != 42 {
		t.Errorf("pr = %+v", aw.PR)
	}
	if aw.PR.CI != "passing" {
		t.Errorf("ci = %q, want passing", aw.PR.CI)
	}
}

func TestProbeAgentWork_NoGitRepo(t *testing.T) {
	bin := t.TempDir()
	writeFakeBin(t, bin, "git", `exit 128`) // not a repo
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if aw := ProbeAgentWork(t.TempDir()); aw != nil {
		t.Errorf("expected nil for non-repo, got %+v", aw)
	}
}

func TestNormalizeCIConclusion(t *testing.T) {
	cases := map[string]string{
		"SUCCESS": "passing", "FAILURE": "failing", "PENDING": "pending",
		"": "none", "weird": "pending",
	}
	for in, want := range cases {
		if got := normalizeCIConclusion(in); got != want {
			t.Errorf("normalizeCIConclusion(%q) = %q, want %q", in, got, want)
		}
	}
}
