package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestARunLeaseIsAValidLeaseName(t *testing.T) {
	for branch, want := range map[string]string{
		"corgi/abc-7":                      "run-abc-7",
		"corgi/acme-api#12":                "run-acme-api-12",
		"corgi/" + strings.Repeat("x", 60): "run-" + strings.Repeat("x", 36),
	} {
		if got := runLease(branch); got != want {
			t.Errorf("%s: %q, want %q", branch, got, want)
		}
	}
}

func TestARunInAWorkspaceWithDatabasesIsToldHowToStartItsOwn(t *testing.T) {
	dir := t.TempDir()
	if runStackNote(dir, "corgi/abc-7", "own") != "" {
		t.Fatal("no compose, no note")
	}
	if err := os.WriteFile(filepath.Join(dir, "corgi-compose.yml"), []byte("name: acme\nservices:\n  api:\n    port: 4500\ndb_services:\n  pg:\n    driver: postgres\n    port: 5432\n    databaseName: acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	note := runStackNote(dir, "corgi/abc-7", "own")
	for _, want := range []string{"corgi --isolate run-abc-7 db -u --wait", "(pg on :5432)", "empty", "Never restore, copy or load a dump", "exec <service> --feature corgi/abc-7", "frees the lease when the run ends"} {
		if !strings.Contains(note, want) {
			t.Errorf("missing %q in:\n%s", want, note)
		}
	}
	shared := runStackNote(dir, "corgi/abc-7", "shared")
	for _, want := range []string{"pg on :5432", "shared with the person and every other run", "never run a migration, a seed, a restore"} {
		if !strings.Contains(shared, want) {
			t.Errorf("shared: missing %q in:\n%s", want, shared)
		}
	}
	if strings.Contains(shared, "--isolate") {
		t.Errorf("a shared database takes no lease:\n%s", shared)
	}
	if runStackNote(dir, "corgi/abc-7", "off") != "" {
		t.Error("off says nothing")
	}
	releaseRunStack(dir, "corgi/abc-7")
}

func TestAnIsolatedRunsWorktreesComeWithThePackages(t *testing.T) {
	ws := t.TempDir()
	api := filepath.Join(ws, "api")
	newRepo(t, api)
	for name, body := range map[string]string{".gitignore": "node_modules/\n", "package-lock.json": `{"lockfileVersion":3}`} {
		if err := os.WriteFile(filepath.Join(api, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, api, "add", ".")
	gitIn(t, api, "commit", "-qm", "lock")
	if err := os.MkdirAll(filepath.Join(api, "node_modules", "pad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "corgi-compose.yml"), []byte("name: acme\nservices:\n  api:\n    path: ./api\n    port: 4500\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	trees, err := isolateFixWorktrees(ws, "corgi/abc-7")
	if err != nil || len(trees) != 1 {
		t.Fatalf("one worktree: %v %v", trees, err)
	}
	if _, err := os.Stat(filepath.Join(trees[0], "node_modules", "pad")); err != nil {
		t.Fatalf("the packages came along: %v", err)
	}
	if _, err := isolateFixWorktrees(t.TempDir(), "corgi/abc-7"); err == nil {
		t.Fatal("a folder without a compose file has nothing to isolate")
	}
	if removed, kept, err := releaseFixWorktrees(ws, ""); removed != nil || kept != nil || err != nil {
		t.Fatal("no branch, nothing to release")
	}
}
