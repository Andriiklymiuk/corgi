package utils

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// A change that spans a stack ends as one pull request per repository. Opening
// them by hand needs a laptop; this is the part corgi can do from a phone.

// RepoPR is one repository's pull request, or the reason there is none.
type RepoPR struct {
	Repo    string `json:"repo"`
	Dir     string `json:"dir"`
	Branch  string `json:"branch"`
	URL     string `json:"url,omitempty"`
	Created bool   `json:"created"`
	Skipped string `json:"skipped,omitempty"`
	Error   string `json:"error,omitempty"`
}

// PRSet is the result of opening pull requests across a stack.
type PRSet struct {
	Branch string   `json:"branch"`
	Base   string   `json:"base,omitempty"`
	PRs    []RepoPR `json:"prs"`
}

// prRunner executes a git/forge command in a directory. Injected for tests.
type prRunner func(dir, name string, args ...string) (string, error)

func execInDir(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// OpenBranchPRs pushes branch in every repository that has commits on it and
// opens a pull request there, then cross-links the bodies so each PR names its
// siblings. Repositories with nothing to ship are reported as skipped, never
// silently dropped.
func OpenBranchPRs(dirs map[string]string, branch, base, title, body string, draft bool) (*PRSet, error) {
	return openBranchPRs(dirs, branch, base, title, body, draft, execInDir)
}

func openBranchPRs(dirs map[string]string, branch, base, title, body string, draft bool, run prRunner) (*PRSet, error) {
	if strings.TrimSpace(branch) == "" {
		return nil, fmt.Errorf("branch is required")
	}
	if err := validateBranchName(branch); err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("a pull request needs a title")
	}

	repos := make([]string, 0, len(dirs))
	for repo := range dirs {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	set := &PRSet{Branch: branch, Base: base}
	for _, repo := range repos {
		set.PRs = append(set.PRs, openOnePR(run, repo, dirs[repo], branch, base, title, body, draft))
	}
	crossLinkPRs(run, set, dirs, body)
	return set, nil
}

func openOnePR(run prRunner, repo, dir, branch, base, title, body string, draft bool) RepoPR {
	pr := RepoPR{Repo: repo, Dir: dir, Branch: branch}

	// Nothing to open a pull request for is the common case in a stack where
	// the change touched two of five repositories.
	if !branchHasCommits(run, dir, branch, base) {
		pr.Skipped = "no commits on " + branch
		return pr
	}
	if existing := existingPRURL(run, dir, branch); existing != "" {
		pr.URL, pr.Skipped = existing, "a pull request is already open"
		return pr
	}

	forge, err := detectForge(run, dir)
	if err != nil {
		pr.Error = err.Error()
		return pr
	}
	if out, err := run(dir, "git", "push", "-u", "origin", branch); err != nil {
		pr.Error = "push failed: " + firstLine(out)
		return pr
	}

	args := forge.createArgs(branch, base, title, body, draft)
	out, err := run(dir, forge.bin, args...)
	if err != nil {
		pr.Error = firstLine(out)
		return pr
	}
	pr.URL, pr.Created = forgeURL(out), true
	return pr
}

// crossLinkPRs appends the sibling list to every body, so opening one PR shows
// the rest of the change. Best effort: a failed edit leaves a working PR.
func crossLinkPRs(run prRunner, set *PRSet, dirs map[string]string, body string) {
	var links []string
	for _, pr := range set.PRs {
		if pr.Created && pr.URL != "" {
			links = append(links, "- "+pr.Repo+": "+pr.URL)
		}
	}
	if len(links) < 2 {
		return
	}
	full := strings.TrimSpace(body + "\n\nPart of one change across " + fmt.Sprint(len(links)) + " repositories:\n" + strings.Join(links, "\n"))
	for _, pr := range set.PRs {
		if !pr.Created || pr.URL == "" {
			continue
		}
		forge, err := detectForge(run, dirs[pr.Repo])
		if err != nil {
			continue
		}
		_, _ = run(dirs[pr.Repo], forge.bin, forge.editArgs(pr.URL, full)...)
	}
}

type forgeCLI struct {
	bin        string
	createArgs func(branch, base, title, body string, draft bool) []string
	editArgs   func(url, body string) []string
}

// detectForge picks gh or glab from the origin remote, so a GitLab stack is not
// handed GitHub's CLI.
func detectForge(run prRunner, dir string) (forgeCLI, error) {
	remote, err := run(dir, "git", "remote", "get-url", "origin")
	if err != nil {
		return forgeCLI{}, fmt.Errorf("no origin remote: %s", firstLine(remote))
	}
	if strings.Contains(remote, "gitlab") {
		if _, err := exec.LookPath("glab"); err != nil {
			return forgeCLI{}, fmt.Errorf("glab is not installed — see gitlab.com/gitlab-org/cli")
		}
		return forgeCLI{
			bin: "glab",
			createArgs: func(branch, base, title, body string, draft bool) []string {
				args := []string{"mr", "create", "--source-branch", branch, "--title", title, "--description", body, "--yes"}
				if base != "" {
					args = append(args, "--target-branch", base)
				}
				if draft {
					args = append(args, "--draft")
				}
				return args
			},
			editArgs: func(url, body string) []string {
				return []string{"mr", "update", url, "--description", body}
			},
		}, nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return forgeCLI{}, fmt.Errorf("gh is not installed — `brew install gh`")
	}
	return forgeCLI{
		bin: "gh",
		createArgs: func(branch, base, title, body string, draft bool) []string {
			args := []string{"pr", "create", "--head", branch, "--title", title, "--body", body}
			if base != "" {
				args = append(args, "--base", base)
			}
			if draft {
				args = append(args, "--draft")
			}
			return args
		},
		editArgs: func(url, body string) []string {
			return []string{"pr", "edit", url, "--body", body}
		},
	}, nil
}

// branchHasCommits reports whether branch carries anything base does not. With
// no base, any commit the remote has not seen counts.
func branchHasCommits(run prRunner, dir, branch, base string) bool {
	ref := strings.TrimSpace(base)
	if ref == "" {
		ref = defaultBaseRef(run, dir)
	}
	out, err := run(dir, "git", "rev-list", "--count", ref+".."+branch)
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) != "" && strings.TrimSpace(out) != "0"
}

func defaultBaseRef(run prRunner, dir string) string {
	if out, err := run(dir, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			return ref
		}
	}
	return "origin/main"
}

// existingPRURL returns the open pull request for branch, if the forge already
// has one — so re-running opens nothing twice.
func existingPRURL(run prRunner, dir, branch string) string {
	if out, err := run(dir, "gh", "pr", "view", branch, "--json", "url", "--jq", ".url"); err == nil {
		return forgeURL(out)
	}
	return ""
}

// forgeURL picks the URL out of a CLI's chatty output.
func forgeURL(out string) string {
	for _, line := range strings.Fields(out) {
		if strings.HasPrefix(line, "https://") {
			return strings.TrimRight(line, ".,)")
		}
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
