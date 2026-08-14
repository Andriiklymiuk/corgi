package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpsertMatchesOnIDSoAMovedRepoDoesNotDuplicate(t *testing.T) {
	r := &Registry{}
	r.Upsert(Workspace{ID: "acme", AbsPath: "/old/location"})
	r.Upsert(Workspace{ID: "acme", AbsPath: "/new/location"})

	if len(r.Workspaces) != 1 {
		t.Fatalf("got %d workspaces, want 1 — a moved repo must update its row, not add one", len(r.Workspaces))
	}
	if r.Workspaces[0].AbsPath != "/new/location" {
		t.Errorf("path = %q, want the new location", r.Workspaces[0].AbsPath)
	}
}

func TestUpsertIsCaseInsensitiveOnID(t *testing.T) {
	r := &Registry{}
	r.Upsert(Workspace{ID: "Acme", AbsPath: "/a"})
	r.Upsert(Workspace{ID: "acme", AbsPath: "/b"})

	if len(r.Workspaces) != 1 {
		t.Errorf("got %d workspaces, want 1", len(r.Workspaces))
	}
}

func TestUpsertPreservesFieldsAPartialUpdateOmits(t *testing.T) {
	r := &Registry{}
	used := time.Now().UTC().Truncate(time.Second)
	r.Upsert(Workspace{
		ID:         "acme",
		AbsPath:    "/a",
		Aliases:    []string{"todo app"},
		Repos:      []string{"api", "web"},
		Services:   []string{"api", "db"},
		LastUsedAt: used,
	})

	// A path-only refresh, e.g. from `corgi list` seeing the compose file again.
	r.Upsert(Workspace{ID: "acme", AbsPath: "/moved"})

	got := r.Workspaces[0]
	if len(got.Aliases) != 1 || got.Aliases[0] != "todo app" {
		t.Errorf("aliases = %v, want them preserved — a partial update must not erase what another path discovered", got.Aliases)
	}
	if len(got.Repos) != 2 || len(got.Services) != 2 {
		t.Errorf("repos/services were erased: %v / %v", got.Repos, got.Services)
	}
	if !got.LastUsedAt.Equal(used) {
		t.Errorf("lastUsedAt = %v, want it preserved", got.LastUsedAt)
	}
}

func TestUpsertDefaultsStatusToOK(t *testing.T) {
	r := &Registry{}
	r.Upsert(Workspace{ID: "acme", AbsPath: "/a"})

	if r.Workspaces[0].Status != StatusOK {
		t.Errorf("status = %q, want %q", r.Workspaces[0].Status, StatusOK)
	}
}

func TestForget(t *testing.T) {
	r := &Registry{}
	r.Upsert(Workspace{ID: "acme", AbsPath: "/a"})
	r.Upsert(Workspace{ID: "other", AbsPath: "/b"})

	if !r.Forget("ACME") {
		t.Error("Forget should match case-insensitively")
	}
	if len(r.Workspaces) != 1 || r.Workspaces[0].ID != "other" {
		t.Errorf("Forget removed the wrong row: %+v", r.Workspaces)
	}
	if r.Forget("missing") {
		t.Error("Forget should report false for an unknown id")
	}
}

func TestReconcileMarksUnreachableWithoutDeleting(t *testing.T) {
	r := &Registry{}
	r.Upsert(Workspace{ID: "mounted", AbsPath: "/mounted"})
	r.Upsert(Workspace{ID: "unmounted", AbsPath: "/volumes/external"})

	r.Reconcile(func(path string) bool { return path == "/mounted" })

	if len(r.Workspaces) != 2 {
		t.Fatal("an unreachable path must keep its row — an unmounted drive is not a deleted project")
	}
	byID := map[string]Status{}
	for _, w := range r.Workspaces {
		byID[w.ID] = w.Status
	}
	if byID["mounted"] != StatusOK {
		t.Errorf("mounted status = %q, want %q", byID["mounted"], StatusOK)
	}
	if byID["unmounted"] != StatusUnreachable {
		t.Errorf("unmounted status = %q, want %q", byID["unmounted"], StatusUnreachable)
	}
}

func TestReconcileLeavesDisabledAlone(t *testing.T) {
	r := &Registry{Workspaces: []Workspace{{ID: "off", AbsPath: "/exists", Status: StatusDisabled}}}

	r.Reconcile(func(string) bool { return true })

	if r.Workspaces[0].Status != StatusDisabled {
		t.Error("a human disabled this workspace; the filesystem must not re-enable it")
	}
}

func TestSortedIsMostRecentlyUsedFirst(t *testing.T) {
	now := time.Now().UTC()
	r := &Registry{Workspaces: []Workspace{
		{ID: "older", LastUsedAt: now.Add(-time.Hour)},
		{ID: "newest", LastUsedAt: now},
		{ID: "never"},
	}}

	got := r.Sorted()

	if got[0].ID != "newest" || got[1].ID != "older" || got[2].ID != "never" {
		t.Errorf("order = %q, %q, %q; want newest, older, never", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "registry.json")
	r := &Registry{}
	r.Upsert(Workspace{ID: "acme", AbsPath: "/a", Aliases: []string{"todo app"}, Services: []string{"api"}})

	if err := Save(path, r); err != nil {
		t.Fatalf("Save() = %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}

	if len(loaded.Workspaces) != 1 || loaded.Workspaces[0].ID != "acme" {
		t.Fatalf("round trip lost data: %+v", loaded.Workspaces)
	}
	if loaded.Version != registryVersion {
		t.Errorf("version = %d, want %d", loaded.Version, registryVersion)
	}
}

func TestSaveLeavesNoTempFileBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.json")

	if err := Save(path, &Registry{}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temp file must be renamed away, not left next to the real one")
	}
}

func TestLoadMissingFileIsEmptyNotAnError(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "absent.json"))

	if err != nil {
		t.Fatalf("a first run must not error, got %v", err)
	}
	if len(r.Workspaces) != 0 {
		t.Error("a missing registry should be empty")
	}
}

func TestLoadCorruptFileReportsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Error("a corrupt registry must surface, not silently reset to empty")
	}
}

func TestFromLegacySplitsComposePathIntoDirAndFile(t *testing.T) {
	got := FromLegacy([]LegacyEntry{
		{Name: "acme", Description: "the stack", Path: "/home/dev/acme/corgi-compose.yml"},
	})

	if len(got) != 1 {
		t.Fatalf("got %d workspaces, want 1", len(got))
	}
	if got[0].AbsPath != "/home/dev/acme" {
		t.Errorf("absPath = %q, want the directory", got[0].AbsPath)
	}
	if got[0].ComposeFile != "corgi-compose.yml" {
		t.Errorf("composeFile = %q", got[0].ComposeFile)
	}
	if got[0].ID != "acme" {
		t.Errorf("id = %q, want acme", got[0].ID)
	}
}

func TestFromLegacyFallsBackToDirectoryNameWhenUnnamed(t *testing.T) {
	got := FromLegacy([]LegacyEntry{{Path: "/home/dev/my-stack/corgi-compose.yml"}})

	if len(got) != 1 || got[0].ID != "my-stack" {
		t.Fatalf("got %+v, want an id derived from the directory", got)
	}
}

func TestFromLegacySkipsUnusableRows(t *testing.T) {
	got := FromLegacy([]LegacyEntry{
		{Name: "fine", Path: "/abs/corgi-compose.yml"},
		{Name: "relative", Path: "relative/corgi-compose.yml"},
		{Name: "empty", Path: "   "},
	})

	if len(got) != 1 || got[0].ID != "fine" {
		t.Errorf("got %+v, want only the absolute row", got)
	}
}

func TestMergeLegacyNeverClobbersConfiguredWorkspaces(t *testing.T) {
	r := &Registry{}
	r.Upsert(Workspace{ID: "acme", AbsPath: "/configured", Aliases: []string{"todo app"}})

	added := MergeLegacy(r, []LegacyEntry{
		{Name: "acme", Path: "/legacy/acme/corgi-compose.yml"},
		{Name: "fresh", Path: "/legacy/fresh/corgi-compose.yml"},
	})

	if added != 1 {
		t.Errorf("added = %d, want 1 (only the unknown one)", added)
	}
	existing, _ := r.Find("acme")
	if existing.AbsPath != "/configured" {
		t.Errorf("migration overwrote a configured workspace: absPath = %q", existing.AbsPath)
	}
	if len(existing.Aliases) != 1 {
		t.Error("migration erased configured aliases")
	}
}
