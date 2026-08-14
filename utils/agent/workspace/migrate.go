package workspace

import (
	"path/filepath"
	"strings"
)

// LegacyEntry is one row of the pre-existing `corgi_exec_paths.txt` registry:
// a name, a description, and the path to a corgi-compose file.
//
// That file is pipe-separated with no quoting and is rewritten by truncating
// first, so a name containing "|" corrupts a row and a crash mid-write loses
// everything. Migration is one-way and lossy only in the sense that already
// corrupt rows are skipped.
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
// anything already recorded. Migration must never clobber a workspace that has
// since been configured with aliases or a config dir.
//
// pathExists filters out rows whose directory is gone. The legacy file was
// append-only with no pruning, so it accumulates temp directories and deleted
// projects; importing those would fill the workspace list with noise on the
// very first run.
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
