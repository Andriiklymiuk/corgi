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
