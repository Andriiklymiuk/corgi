// Package brief records what a supervised session was working on, so the one
// that replaces it can be told.
//
// A restart gives a NEW session with none of the conversation. corgi cannot
// restore that, but it does know what survives on disk: the branch in each
// repository, which hold uncommitted work, and any leftover worktrees.
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

// Brief is one ended session's leftovers.
type Brief struct {
	WorkspaceID string    `json:"workspaceId"`
	Dir         string    `json:"dir,omitempty"`
	EndedAt     time.Time `json:"endedAt"`
	// Cause and Reason are the supervisor's classification, carried verbatim so
	// the brief answers "why am I a new session" without a second lookup.
	Cause  string `json:"cause,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Restarts is how many times this workspace has been restarted. A brief
	// that keeps reappearing means something is wrong beyond a flaky network.
	Restarts int `json:"restarts,omitempty"`
	// Repos is one entry per service checkout, including worktrees.
	Repos []RepoState `json:"repos,omitempty"`
}

// RepoState is what one repository looked like when the session ended.
type RepoState struct {
	Service string `json:"service"`
	Dir     string `json:"dir,omitempty"`
	Branch  string `json:"branch,omitempty"`
	Dirty   bool   `json:"dirty,omitempty"`
	// Worktree marks a checkout materialized for a cross-repo branch rather
	// than the service's main one.
	Worktree bool `json:"worktree,omitempty"`
}

// Params is everything the supervisor knows at the moment a session ends.
type Params struct {
	WorkspaceID string
	Dir         string
	Cause       string
	Reason      string
	Restarts    int
	EndedAt     time.Time
}

// Capture builds a brief. repos comes from the caller because enumerating a
// stack's services means parsing a compose file, which this package
// deliberately knows nothing about — that keeps it testable without git, a
// compose file, or a workspace on disk.
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

// Empty reports whether there is nothing worth telling anyone. A brief with no
// repository state says only "it restarted", which the notification already
// said.
func (b Brief) Empty() bool {
	for _, r := range b.Repos {
		if r.Branch != "" || r.Dirty {
			return false
		}
	}
	return true
}

// Summary is the one line that goes in a restart notification.
//
// It has to be readable on a lock screen, so it names the branches and counts
// the rest rather than listing every repository.
func (b Brief) Summary() string {
	if b.Empty() {
		return ""
	}

	var branches []string
	seen := map[string]bool{}
	dirty := 0
	for _, r := range b.Repos {
		if r.Branch != "" && !seen[r.Branch] {
			seen[r.Branch] = true
			branches = append(branches, r.Branch)
		}
		if r.Dirty {
			dirty++
		}
	}
	sort.Strings(branches)

	var parts []string
	switch len(branches) {
	case 0:
	case 1:
		parts = append(parts, "was on "+branches[0])
	default:
		parts = append(parts, "was on "+strings.Join(branches, ", "))
	}
	if dirty == 1 {
		parts = append(parts, "1 repo has uncommitted changes")
	} else if dirty > 1 {
		parts = append(parts, fmt.Sprintf("%d repos have uncommitted changes", dirty))
	}
	return strings.Join(parts, " · ")
}

// dirName is where briefs live under the agent data directory.
const dirName = "briefs"

// Path is the file backing one workspace's brief. Only the most recent is kept:
// a brief is a handover note, and yesterday's is noise.
func Path(agentDir, workspaceID string) string {
	return filepath.Join(agentDir, dirName, sanitize(workspaceID)+".json")
}

// Write stores a brief, replacing any earlier one for that workspace.
//
// Written 0600 in the agent data directory: it names repository paths and
// branch names, which is more than a passer-by on a shared machine should get.
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
	// Write-then-rename, so a daemon killed mid-write cannot leave a truncated
	// brief that fails to parse for the session that needed it.
	return atomicfile.Write(path, data, 0o600)
}

// Read returns a workspace's brief, or nil when there is none — which is the
// ordinary case for a session that has not restarted.
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

// List returns every stored brief, newest first.
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
			continue // one unreadable brief must not hide the rest
		}
		var b Brief
		if json.Unmarshal(data, &b) == nil {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EndedAt.After(out[j].EndedAt) })
	return out, nil
}

// Clear removes a workspace's brief. Used when a workspace is forgotten, so a
// stale note cannot resurface against a different stack at the same id.
func Clear(agentDir, workspaceID string) error {
	err := os.Remove(Path(agentDir, workspaceID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// sanitize turns a workspace id into a filename. Getting any of this wrong
// serves the wrong workspace's branches to someone: it must not escape the
// briefs directory, distinct ids must not collide (hence the hash suffix when
// the mapping changed anything), and it lowercases to match the registry's own
// case-insensitive comparison, or `workspaces forget ACME` orphans the file.
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
	// Something was rewritten, so the safe form is no longer unique on its own.
	sum := sha256.Sum256([]byte(lower))
	if replaced == "" {
		return fmt.Sprintf("workspace-%x", sum[:4])
	}
	return fmt.Sprintf("%s-%x", replaced, sum[:4])
}
