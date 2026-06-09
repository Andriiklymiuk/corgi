package utils

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// PullRequestState is the forge-side view of one service branch's PR/MR.
// All fields are best-effort; an empty struct is never produced — PR is nil
// when no PR matches the branch.
type PullRequestState struct {
	Provider string `json:"provider"` // "github" | "gitlab"
	Number   int    `json:"number,omitempty"`
	State    string `json:"state"` // "open" | "merged" | "closed"
	Draft    bool   `json:"draft,omitempty"`
	URL      string `json:"url,omitempty"`
	CI       string `json:"ci,omitempty"` // "passing" | "failing" | "pending" | "none"
}

// AgentWork is what a per-service git/forge probe can see locally for one
// service's checkout. Tracker ticket correlation is layered on top by the
// tracker skill (see docs/tracker.md) — out of scope here.
type AgentWork struct {
	RepoPath string            `json:"repoPath"`
	Branch   string            `json:"branch,omitempty"`
	Dirty    bool              `json:"dirty,omitempty"`
	PR       *PullRequestState `json:"pr,omitempty"`
}

// ProbeAgentWork is a best-effort read of one service checkout's code state:
// branch, dirty flag, and the PR/MR for that branch with a CI rollup. Returns
// nil when dir isn't a git repo. Never errors — a missing gh/glab or no PR
// just leaves fields empty, so one bad service can't sink the whole snapshot.
func ProbeAgentWork(dir string) *AgentWork {
	if dir == "" || !isGitRepo(dir) {
		return nil
	}
	aw := &AgentWork{RepoPath: dir}
	if b, err := gitOut(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		aw.Branch = b
	}
	if dirty, err := isTreeDirty(dir); err == nil {
		aw.Dirty = dirty
	}
	if aw.Branch != "" && aw.Branch != "HEAD" {
		aw.PR = probePullRequest(dir, aw.Branch)
	}
	return aw
}

// probePullRequest tries GitHub (gh) first, then GitLab (glab). Returns nil if
// neither tool is installed or no PR/MR matches the branch.
func probePullRequest(dir, branch string) *PullRequestState {
	if _, err := exec.LookPath("gh"); err == nil {
		if pr := probeGithubPR(dir, branch); pr != nil {
			return pr
		}
	}
	if _, err := exec.LookPath("glab"); err == nil {
		if pr := probeGitlabMR(dir, branch); pr != nil {
			return pr
		}
	}
	return nil
}

func probeGithubPR(dir, branch string) *PullRequestState {
	cmd := exec.Command("gh", "pr", "view", branch,
		"--json", "number,state,isDraft,url,statusCheckRollup")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var raw struct {
		Number            int       `json:"number"`
		State             string    `json:"state"`
		IsDraft           bool      `json:"isDraft"`
		URL               string    `json:"url"`
		StatusCheckRollup []ciCheck `json:"statusCheckRollup"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return nil
	}
	return &PullRequestState{
		Provider: "github",
		Number:   raw.Number,
		State:    strings.ToLower(raw.State),
		Draft:    raw.IsDraft,
		URL:      raw.URL,
		CI:       rollupCI(raw.StatusCheckRollup),
	}
}

func probeGitlabMR(dir, branch string) *PullRequestState {
	cmd := exec.Command("glab", "mr", "list", "--source-branch", branch, "--output", "json")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var mrs []struct {
		IID    int    `json:"iid"`
		State  string `json:"state"`
		Draft  bool   `json:"draft"`
		WebURL string `json:"web_url"`
	}
	if json.Unmarshal(out, &mrs) != nil || len(mrs) == 0 {
		return nil
	}
	m := mrs[0]
	return &PullRequestState{
		Provider: "gitlab",
		Number:   m.IID,
		State:    strings.ToLower(m.State),
		Draft:    m.Draft,
		URL:      m.WebURL,
		CI:       "none", // glab list doesn't return a rollup; left "none" for v1.
	}
}

// ciCheck is one entry of GitHub's statusCheckRollup.
type ciCheck struct {
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
}

// rollupCI collapses a PR's check rollup into one CI verdict: any failure wins,
// else any pending downgrades to pending, else passing.
func rollupCI(checks []ciCheck) string {
	if len(checks) == 0 {
		return "none"
	}
	worst := "passing"
	for _, c := range checks {
		switch normalizeCIConclusion(c.Conclusion) {
		case "failing":
			return "failing"
		case "pending":
			worst = "pending"
		}
	}
	return worst
}

func normalizeCIConclusion(c string) string {
	switch strings.ToUpper(c) {
	case "SUCCESS":
		return "passing"
	case "FAILURE", "CANCELLED", "TIMED_OUT", "ERROR":
		return "failing"
	case "":
		return "none"
	default:
		return "pending"
	}
}
