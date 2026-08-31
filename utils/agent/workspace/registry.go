// Package workspace tracks which corgi stacks exist on this machine and where
// they live, so a message from a phone can name a stack instead of a path.
package workspace

import (
	"andriiklymiuk/corgi/utils/atomicfile"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Status is whether a registered workspace can be used right now.
type Status string

const (
	// StatusOK means the path exists and still holds a compose file.
	StatusOK Status = "ok"
	// StatusUnreachable means the path did not resolve. The row is kept —
	// an unmounted drive is not a deleted project, and "not found" would send
	// someone hunting for a workspace that is fine.
	StatusUnreachable Status = "unreachable"
	// StatusDisabled means a human or the supervisor took it out of service.
	StatusDisabled Status = "disabled"
)

// Workspace is one registered corgi stack.
type Workspace struct {
	ID          string    `json:"id"`
	Aliases     []string  `json:"aliases,omitempty"`
	AbsPath     string    `json:"absPath"`
	ComposeFile string    `json:"composeFile,omitempty"`
	Description string    `json:"description,omitempty"`
	Repos       []string  `json:"repos,omitempty"`
	Services    []string  `json:"services,omitempty"`
	LastUsedAt  time.Time `json:"lastUsedAt,omitempty"`
	Status      Status    `json:"status,omitempty"`
}

// Registry is the on-disk set of workspaces.
type Registry struct {
	Version    int         `json:"version"`
	UpdatedAt  time.Time   `json:"updatedAt"`
	Workspaces []Workspace `json:"workspaces"`
}

// registryVersion is bumped only for a breaking layout change.
const registryVersion = 1

// Upsert adds or updates a workspace, matching on ID so a moved repo updates
// its path instead of creating a duplicate row.
func (r *Registry) Upsert(w Workspace) {
	if w.Status == "" {
		w.Status = StatusOK
	}
	for i := range r.Workspaces {
		if !strings.EqualFold(r.Workspaces[i].ID, w.ID) {
			continue
		}
		existing := r.Workspaces[i]
		// Preserve fields the caller did not supply, so a partial update from
		// one code path cannot erase what another discovered.
		if len(w.Aliases) == 0 {
			w.Aliases = existing.Aliases
		}
		if len(w.Repos) == 0 {
			w.Repos = existing.Repos
		}
		if len(w.Services) == 0 {
			w.Services = existing.Services
		}
		if w.LastUsedAt.IsZero() {
			w.LastUsedAt = existing.LastUsedAt
		}
		if w.Description == "" {
			w.Description = existing.Description
		}
		r.Workspaces[i] = w
		return
	}
	r.Workspaces = append(r.Workspaces, w)
}

// Find returns the workspace with the given id, case-insensitively.
func (r *Registry) Find(id string) (Workspace, bool) {
	for _, w := range r.Workspaces {
		if strings.EqualFold(w.ID, id) {
			return w, true
		}
	}
	return Workspace{}, false
}

// Forget removes a workspace. Reports whether anything was removed.
func (r *Registry) Forget(id string) bool {
	for i := range r.Workspaces {
		if strings.EqualFold(r.Workspaces[i].ID, id) {
			r.Workspaces = append(r.Workspaces[:i], r.Workspaces[i+1:]...)
			return true
		}
	}
	return false
}

// Reconcile refreshes each workspace's status against the filesystem.
// exists is injected so tests do not need real directories.
func (r *Registry) Reconcile(exists func(path string) bool) {
	for i := range r.Workspaces {
		if r.Workspaces[i].Status == StatusDisabled {
			continue // a human turned this off; the filesystem does not override that
		}
		if exists(r.Workspaces[i].AbsPath) {
			r.Workspaces[i].Status = StatusOK
		} else {
			r.Workspaces[i].Status = StatusUnreachable
		}
	}
}

// Sorted returns the workspaces most-recently-used first, then by id, so every
// surface lists them in the same order.
func (r *Registry) Sorted() []Workspace {
	out := append([]Workspace(nil), r.Workspaces...)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].LastUsedAt.Equal(out[j].LastUsedAt) {
			return out[i].LastUsedAt.After(out[j].LastUsedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Load reads the registry, returning an empty one when the file is absent.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Registry{Version: registryVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.Version == 0 {
		r.Version = registryVersion
	}
	return &r, nil
}

// Save writes the registry with the tmp-write plus rename discipline the rest
// of corgi uses, so a SIGKILL mid-write cannot truncate it.
func Save(path string, r *Registry) error {
	r.Version = registryVersion
	r.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, data, 0o644)
}
