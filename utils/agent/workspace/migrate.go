package workspace

import (
	"path/filepath"
	"strings"
)

type LegacyEntry struct {
	Name        string
	Description string
	Path        string
}

func FromLegacy(entries []LegacyEntry) []Workspace {
	seen := map[string]int{}
	var out []Workspace

	for _, e := range entries {
		path := strings.TrimSpace(e.Path)
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		w := Workspace{
			ID:          legacyID(e, path),
			AbsPath:     filepath.Dir(path),
			ComposeFile: filepath.Base(path),
			Description: strings.TrimSpace(e.Description),
			Status:      StatusOK,
		}
		if w.ID == "" {
			continue
		}
		key := strings.ToLower(w.ID)
		if i, ok := seen[key]; ok {
			out[i] = w
			continue
		}
		seen[key] = len(out)
		out = append(out, w)
	}
	return out
}

func legacyID(e LegacyEntry, path string) string {
	if name := strings.TrimSpace(e.Name); name != "" {
		return name
	}
	return filepath.Base(filepath.Dir(path))
}

func MergeLegacy(r *Registry, entries []LegacyEntry, pathExists func(string) bool) (added int) {
	for _, w := range FromLegacy(entries) {
		if _, exists := r.Find(w.ID); exists {
			continue
		}
		if pathExists != nil && !pathExists(w.AbsPath) {
			continue
		}
		r.Upsert(w)
		added++
	}
	return added
}
