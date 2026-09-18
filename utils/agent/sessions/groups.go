package sessions

import (
	"regexp"
	"strings"
)

type Group struct {
	Key        string   `json:"key"`
	Ticket     string   `json:"ticket,omitempty"`
	Sessions   []string `json:"sessions"`
	Workspaces []string `json:"workspaces,omitempty"`
	Branches   []string `json:"branches,omitempty"`
	Worktrees  []string `json:"worktrees,omitempty"`
	PRs        []string `json:"prs,omitempty"`
	Attempts   int      `json:"attempts,omitempty"`
	NeedsInput int      `json:"needsInput,omitempty"`
	Working    int      `json:"working,omitempty"`
}

var ticketInBranch = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9}-\d{1,6})\b`)

func GroupKey(s Session) string {
	if k := strings.TrimSpace(s.TicketKey); k != "" {
		return strings.ToUpper(k)
	}
	branch := strings.TrimSpace(s.Branch)
	if m := ticketInBranch.FindStringSubmatch(strings.ToUpper(branch)); m != nil {
		return m[1]
	}
	switch branch {
	case "", "main", "master", "develop", "dev", "trunk", "HEAD":
		return ""
	}
	return branch
}

func Groups(sessions []Session) []Group {
	var out []Group
	index := map[string]int{}
	for _, s := range sessions {
		key := GroupKey(s)
		if key == "" {
			continue
		}
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, Group{Key: key})
		}
		t := &out[i]
		t.Sessions = append(t.Sessions, s.ID)
		if t.Ticket == "" {
			t.Ticket = s.Ticket
		}
		t.Workspaces = appendUnique(t.Workspaces, s.Label)
		t.Branches = appendUnique(t.Branches, s.Branch)
		t.PRs = appendUnique(t.PRs, s.PR)
		if s.Attempt != "" {
			t.Attempts++
		}
		if s.Home != "" && s.Cwd != "" && s.Home != s.Cwd {
			t.Worktrees = appendUnique(t.Worktrees, s.Cwd)
		}
		switch s.Status {
		case StatusNeedsInput:
			t.NeedsInput++
		case StatusWorking:
			t.Working++
		}
	}
	return out
}

func appendUnique(list []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
