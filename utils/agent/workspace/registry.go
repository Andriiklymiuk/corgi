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

type Status string

const (
	StatusOK          Status = "ok"
	StatusUnreachable Status = "unreachable"
	StatusDisabled    Status = "disabled"
)

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

type Registry struct {
	Version    int         `json:"version"`
	UpdatedAt  time.Time   `json:"updatedAt"`
	Workspaces []Workspace `json:"workspaces"`
}

const registryVersion = 1

func (r *Registry) Upsert(w Workspace) {
	if w.Status == "" {
		w.Status = StatusOK
	}
	for i := range r.Workspaces {
		if !strings.EqualFold(r.Workspaces[i].ID, w.ID) {
			continue
		}
		existing := r.Workspaces[i]
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

func (r *Registry) Find(id string) (Workspace, bool) {
	for _, w := range r.Workspaces {
		if strings.EqualFold(w.ID, id) {
			return w, true
		}
	}
	return Workspace{}, false
}

func (r *Registry) Forget(id string) bool {
	for i := range r.Workspaces {
		if strings.EqualFold(r.Workspaces[i].ID, id) {
			r.Workspaces = append(r.Workspaces[:i], r.Workspaces[i+1:]...)
			return true
		}
	}
	return false
}

func (r *Registry) Reconcile(exists func(path string) bool) {
	for i := range r.Workspaces {
		if r.Workspaces[i].Status == StatusDisabled {
			continue
		}
		if exists(r.Workspaces[i].AbsPath) {
			r.Workspaces[i].Status = StatusOK
		} else {
			r.Workspaces[i].Status = StatusUnreachable
		}
	}
}

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
