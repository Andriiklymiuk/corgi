package utils

import (
	"fmt"
	"strings"
	"testing"
)

// fakeForge records every command and answers the few git queries openBranchPRs
// makes, so the flow can be checked without a network or a forge CLI.
type fakeForge struct {
	calls   []string
	commits map[string]string
	create  map[string]string
	fail    map[string]bool
}

func (f *fakeForge) run(dir, name string, args ...string) (string, error) {
	call := dir + " " + name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	switch {
	case name == "git" && args[0] == "remote":
		return "git@github.com:acme/" + dir + ".git", nil
	case name == "git" && args[0] == "symbolic-ref":
		return "origin/main", nil
	case name == "git" && args[0] == "rev-list":
		return f.commits[dir], nil
	case name == "git" && args[0] == "push":
		if f.fail[dir] {
			return "remote rejected", fmt.Errorf("exit 1")
		}
		return "", nil
	case name == "gh" && args[0] == "pr" && args[1] == "view":
		return "", fmt.Errorf("no pr")
	case name == "gh" && args[0] == "pr" && args[1] == "create":
		return f.create[dir], nil
	}
	return "", nil
}

func TestOpenBranchPRsSkipsReposWithoutCommits(t *testing.T) {
	f := &fakeForge{
		commits: map[string]string{"api": "3", "web": "0"},
		create:  map[string]string{"api": "https://github.com/acme/api/pull/7"},
	}
	set, err := openBranchPRs(map[string]string{"api": "api", "web": "web"},
		"feature/x", "", "Add referrals", "why", false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.PRs) != 2 {
		t.Fatalf("prs = %+v", set.PRs)
	}
	byRepo := map[string]RepoPR{}
	for _, pr := range set.PRs {
		byRepo[pr.Repo] = pr
	}
	if !byRepo["api"].Created || byRepo["api"].URL != "https://github.com/acme/api/pull/7" {
		t.Errorf("api = %+v", byRepo["api"])
	}
	if byRepo["web"].Created || !strings.Contains(byRepo["web"].Skipped, "no commits") {
		t.Errorf("a repo with nothing on the branch must be reported as skipped, got %+v", byRepo["web"])
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "web ") && strings.Contains(c, "push") {
			t.Error("a repo with no commits must not be pushed")
		}
	}
}

func TestOpenBranchPRsCrossLinksSiblings(t *testing.T) {
	f := &fakeForge{
		commits: map[string]string{"api": "2", "web": "1"},
		create: map[string]string{
			"api": "https://github.com/acme/api/pull/7",
			"web": "https://github.com/acme/web/pull/9",
		},
	}
	set, err := openBranchPRs(map[string]string{"api": "api", "web": "web"},
		"feature/x", "main", "Add referrals", "why", false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.PRs) != 2 || !set.PRs[0].Created || !set.PRs[1].Created {
		t.Fatalf("both must be created: %+v", set.PRs)
	}
	edits := 0
	for _, c := range f.calls {
		if strings.Contains(c, "gh pr edit") {
			edits++
			if !strings.Contains(c, "pull/7") || !strings.Contains(c, "pull/9") {
				t.Errorf("an edited body must name both siblings: %s", c)
			}
		}
	}
	if edits != 2 {
		t.Errorf("edits = %d, want one per created pull request", edits)
	}
}

func TestOpenBranchPRsReportsAFailedPush(t *testing.T) {
	f := &fakeForge{commits: map[string]string{"api": "1"}, fail: map[string]bool{"api": true}}
	set, err := openBranchPRs(map[string]string{"api": "api"}, "feature/x", "", "T", "", false, f.run)
	if err != nil {
		t.Fatal(err)
	}
	if set.PRs[0].Created || !strings.Contains(set.PRs[0].Error, "push failed") {
		t.Errorf("a failed push must surface as an error, got %+v", set.PRs[0])
	}
}

func TestOpenBranchPRsValidatesInput(t *testing.T) {
	f := &fakeForge{}
	if _, err := openBranchPRs(nil, "", "", "T", "", false, f.run); err == nil {
		t.Error("an empty branch must be refused")
	}
	if _, err := openBranchPRs(nil, "feature/x", "", "", "", false, f.run); err == nil {
		t.Error("an empty title must be refused")
	}
	if _, err := openBranchPRs(nil, "--upload-pack=evil", "", "T", "", false, f.run); err == nil {
		t.Error("a flag-shaped branch must be refused")
	}
}

func TestForgeURL(t *testing.T) {
	out := "Creating pull request for feature/x\nhttps://github.com/acme/api/pull/7\n"
	if got := forgeURL(out); got != "https://github.com/acme/api/pull/7" {
		t.Errorf("forgeURL = %q", got)
	}
	if got := forgeURL("nothing here"); got != "" {
		t.Errorf("no URL must be empty, got %q", got)
	}
}

func TestDetectForgePicksGitLabForAGitLabRemote(t *testing.T) {
	run := func(dir, name string, args ...string) (string, error) {
		if name == "git" && args[0] == "remote" {
			return "git@gitlab.com:acme/api.git", nil
		}
		return "", nil
	}
	forge, err := detectForge(run, "api")
	if err != nil {
		t.Skip("glab not installed on this machine")
	}
	if forge.bin != "glab" {
		t.Errorf("bin = %q, want glab", forge.bin)
	}
	if got := strings.Join(forge.createArgs("b", "main", "T", "B", true), " "); !strings.Contains(got, "mr create") || !strings.Contains(got, "--draft") {
		t.Errorf("glab create args = %q", got)
	}
	if got := strings.Join(forge.viewArgs("b"), " "); !strings.Contains(got, "mr view") {
		t.Errorf("glab view args = %q", got)
	}
}

func TestDetectForgeReportsAMissingRemote(t *testing.T) {
	run := func(dir, name string, args ...string) (string, error) {
		return "fatal: no such remote", fmt.Errorf("exit 2")
	}
	if _, err := detectForge(run, "api"); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Errorf("err = %v", err)
	}
}

func TestExistingPRIsReturnedNotDuplicated(t *testing.T) {
	f := &fakeForge{commits: map[string]string{"api": "2"}}
	run := func(dir, name string, args ...string) (string, error) {
		if name == "gh" && args[0] == "pr" && args[1] == "view" {
			return "https://github.com/acme/api/pull/3", nil
		}
		return f.run(dir, name, args...)
	}
	set, err := openBranchPRs(map[string]string{"api": "api"}, "feature/x", "", "T", "", false, run)
	if err != nil {
		t.Fatal(err)
	}
	pr := set.PRs[0]
	if pr.Created || pr.URL != "https://github.com/acme/api/pull/3" || !strings.Contains(pr.Skipped, "already open") {
		t.Errorf("an existing pull request must be returned, not reopened: %+v", pr)
	}
	for _, c := range f.calls {
		if strings.Contains(c, "pr create") {
			t.Error("nothing must be created when one is already open")
		}
	}
}

func TestOpenBranchPRsIsTheExportedEntryPoint(t *testing.T) {
	if _, err := OpenBranchPRs(nil, "", "", "T", "", false); err == nil {
		t.Error("the exported entry point must validate too")
	}
}

func TestBranchHasCommitsFallsBackToOriginHead(t *testing.T) {
	var refs []string
	run := func(dir, name string, args ...string) (string, error) {
		if name == "git" && args[0] == "symbolic-ref" {
			return "origin/trunk", nil
		}
		if name == "git" && args[0] == "rev-list" {
			refs = append(refs, args[2])
			return "1", nil
		}
		return "", nil
	}
	if !branchHasCommits(run, "api", "feature/x", "") {
		t.Error("a branch with commits must report true")
	}
	if len(refs) != 1 || !strings.HasPrefix(refs[0], "origin/trunk..") {
		t.Errorf("with no base it must use origin HEAD, got %v", refs)
	}
}

func TestExecInDirRunsAndReportsFailure(t *testing.T) {
	dir := t.TempDir()
	out, err := execInDir(dir, "sh", "-c", "pwd")
	if err != nil || out == "" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := execInDir(dir, "sh", "-c", "exit 4"); err == nil {
		t.Error("a failing command must report an error")
	}
}
