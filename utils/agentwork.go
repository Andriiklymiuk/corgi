package utils

import (
	"encoding/json"
	"os/exec"
	"strings"
)

type PullRequestState struct {
	Provider string `json:"provider"`
	Number   int    `json:"number,omitempty"`
	State    string `json:"state"`
	Draft    bool   `json:"draft,omitempty"`
	URL      string `json:"url,omitempty"`
	CI       string `json:"ci,omitempty"`
}

type AgentWork struct {
	RepoPath string            `json:"repoPath"`
	Branch   string            `json:"branch,omitempty"`
	Dirty    bool              `json:"dirty,omitempty"`
	PR       *PullRequestState `json:"pr,omitempty"`
}

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

type RepoState struct {
	Path     string `json:"path,omitempty"`
	Branch   string `json:"branch"`
	Dirty    bool   `json:"dirty"`
	Head     string `json:"head,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
}

func ProbeRepoState(dir string) (RepoState, bool) {
	if dir == "" || !isGitRepo(dir) {
		return RepoState{}, false
	}
	var st RepoState
	if b, err := gitOut(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && b != "HEAD" {
		st.Branch = b
	}
	st.Dirty = HasUncommittedWork(dir)
	return st, true
}

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
		CI:       "none",
	}
}

type ciCheck struct {
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
}

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
