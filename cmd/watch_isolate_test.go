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
	if runStackNote(dir, "corgi/abc-7") != "" {
		t.Fatal("no compose, no note")
	}
	if err := os.WriteFile(filepath.Join(dir, "corgi-compose.yml"), []byte("name: acme\nservices:\n  api:\n    port: 4500\ndb_services:\n  pg:\n    driver: postgres\n    port: 5432\n    databaseName: acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	note := runStackNote(dir, "corgi/abc-7")
	for _, want := range []string{"corgi --isolate run-abc-7 db -u --wait", "(pg)", "exec <service> --feature corgi/abc-7", "frees the lease when the run ends"} {
		if !strings.Contains(note, want) {
			t.Errorf("missing %q in:\n%s", want, note)
		}
	}
	releaseRunStack(dir, "corgi/abc-7")
}
