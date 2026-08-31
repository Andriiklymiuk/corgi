package workspace

import (
	"path/filepath"
	"strings"
)

// LegacyEntry is one row of the pre-existing `corgi_exec_paths.txt` registry.
// That file is pipe-separated with no quoting and rewritten by truncating, so a
// name containing "|" corrupts a row. Migration is one-way and skips those.
type LegacyEntry struct {
	Name        string
	Description string
	Path        string
}

// FromLegacy converts legacy rows into workspaces, skipping unusable ones.
// Later rows win, matching the legacy file's own last-write-wins behaviour.
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

// legacyID prefers the compose file's own name and falls back to the directory,
// so a nameless entry still gets something a person would recognise.
func legacyID(e LegacyEntry, path string) string {
	if name := strings.TrimSpace(e.Name); name != "" {
		return name
	}
	return filepath.Base(filepath.Dir(path))
}

// MergeLegacy folds legacy rows into an existing registry without overwriting
// anything already recorded — a workspace since given aliases or a config dir
// must survive. pathExists prunes rows whose directory is gone; the legacy file
// was append-only and accumulated temp dirs and deleted projects.
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
