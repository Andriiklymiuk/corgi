package brief

import (
	"andriiklymiuk/corgi/utils/atomicfile"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Brief struct {
	WorkspaceID string      `json:"workspaceId"`
	Dir         string      `json:"dir,omitempty"`
	EndedAt     time.Time   `json:"endedAt"`
	Cause       string      `json:"cause,omitempty"`
	Reason      string      `json:"reason,omitempty"`
	Restarts    int         `json:"restarts,omitempty"`
	Repos       []RepoState `json:"repos,omitempty"`
}

type RepoState struct {
	Service  string `json:"service"`
	Dir      string `json:"dir,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Dirty    bool   `json:"dirty,omitempty"`
	Worktree bool   `json:"worktree,omitempty"`
	Default  bool   `json:"default,omitempty"`
}

var restingBranchNames = map[string]bool{
	"main": true, "master": true, "develop": true, "dev": true, "trunk": true,
}

// Resting is true when the repo sits on its default branch (or one of the
// usual base names) - nothing a restarted session needs to be told about.
func (r RepoState) Resting() bool {
	return r.Default || restingBranchNames[r.Branch]
}

type Params struct {
	WorkspaceID string
	Dir         string
	Cause       string
	Reason      string
	Restarts    int
	EndedAt     time.Time
}

func Capture(p Params, repos []RepoState) Brief {
	endedAt := p.EndedAt
	if endedAt.IsZero() {
		endedAt = time.Now()
	}
	sorted := append([]RepoState(nil), repos...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Service < sorted[j].Service })
	return Brief{
		WorkspaceID: p.WorkspaceID,
		Dir:         p.Dir,
		EndedAt:     endedAt.UTC(),
		Cause:       p.Cause,
		Reason:      p.Reason,
		Restarts:    p.Restarts,
		Repos:       sorted,
	}
}

func (b Brief) Empty() bool {
	for _, r := range b.Repos {
		if (r.Branch != "" && !r.Resting()) || r.Dirty {
			return false
		}
	}
	return true
}

const maxNamedBranches = 3

func (b Brief) workBranches() []string {
	seen := map[string]bool{}
	var branches []string
	for _, r := range b.Repos {
		if r.Branch == "" || r.Resting() || seen[r.Branch] {
			continue
		}
		seen[r.Branch] = true
		branches = append(branches, r.Branch)
	}
	sort.Strings(branches)
	return branches
}

func (b Brief) Summary() string {
	if b.Empty() {
		return ""
	}

	branches := b.workBranches()
	dirty := 0
	for _, r := range b.Repos {
		if r.Dirty {
			dirty++
		}
	}

	var parts []string
	switch {
	case len(branches) == 0:
	case len(branches) <= maxNamedBranches:
		parts = append(parts, "was on "+strings.Join(branches, ", "))
	default:
		rest := len(branches) - maxNamedBranches
		parts = append(parts, fmt.Sprintf("was on %s and %d more", strings.Join(branches[:maxNamedBranches], ", "), rest))
	}
	if dirty == 1 {
		parts = append(parts, "1 repo has uncommitted changes")
	} else if dirty > 1 {
		parts = append(parts, fmt.Sprintf("%d repos have uncommitted changes", dirty))
	}
	return strings.Join(parts, " · ")
}

const dirName = "briefs"

func Path(agentDir, workspaceID string) string {
	return filepath.Join(agentDir, dirName, sanitize(workspaceID)+".json")
}

func Write(agentDir string, b Brief) error {
	if b.WorkspaceID == "" {
		return fmt.Errorf("brief: workspace id is required")
	}
	path := Path(agentDir, b.WorkspaceID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o600)
}

func Read(agentDir, workspaceID string) (*Brief, error) {
	data, err := os.ReadFile(Path(agentDir, workspaceID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b Brief
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(agentDir, workspaceID), err)
	}
	return &b, nil
}

func List(agentDir string) ([]Brief, error) {
	entries, err := os.ReadDir(filepath.Join(agentDir, dirName))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Brief
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(agentDir, dirName, e.Name()))
		if readErr != nil {
			continue
		}
		var b Brief
		if json.Unmarshal(data, &b) == nil {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndedAt.After(out[j].EndedAt) })
	return out, nil
}

func Clear(agentDir, workspaceID string) error {
	err := os.Remove(Path(agentDir, workspaceID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func sanitize(id string) string {
	lower := strings.ToLower(id)
	replaced := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, lower)

	if replaced == lower && replaced != "" {
		return replaced
	}
	sum := sha256.Sum256([]byte(lower))
	if replaced == "" {
		return fmt.Sprintf("workspace-%x", sum[:4])
	}
	return fmt.Sprintf("%s-%x", replaced, sum[:4])
}
