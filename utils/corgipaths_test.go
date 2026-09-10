package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCorgiServicesPrefersTheNestedFolderAndFallsBackToLegacy(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(".corgi", "corgi_services")

	if got := CorgiServicesRelIn(dir); got != nested {
		t.Errorf("empty workspace = %q, want %q", got, nested)
	}

	os.MkdirAll(filepath.Join(dir, "corgi_services"), 0o755)
	if got := CorgiServicesRelIn(dir); got != "corgi_services" {
		t.Errorf("legacy present = %q, want the legacy folder", got)
	}

	os.MkdirAll(filepath.Join(dir, nested), 0o755)
	if got := CorgiServicesRelIn(dir); got != nested {
		t.Errorf("both present = %q, want %q", got, nested)
	}

	if got := CorgiServicesRelIn(""); got != nested {
		t.Errorf("no workspace = %q, want %q", got, nested)
	}
}

func TestComposeDirOfWalksBackThroughEitherLayout(t *testing.T) {
	if got := ComposeDirOf("/ws/.corgi/corgi_services"); got != "/ws" {
		t.Errorf("nested = %q", got)
	}
	if got := ComposeDirOf("/ws/corgi_services"); got != "/ws" {
		t.Errorf("legacy = %q", got)
	}
}

func TestMigrateMovesTheFolderAndRetargetsGitignore(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "corgi_services")
	os.MkdirAll(filepath.Join(legacy, "db_services", "pg"), 0o755)
	os.WriteFile(filepath.Join(legacy, "db_services", "pg", ".env"), []byte("PGHOST=x"), 0o600)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules\ncorgi_services/*\n!corgi_services/.gitignore\n"), 0o644)

	moved, err := MigrateCorgiServices(dir)
	if err != nil || !moved {
		t.Fatalf("migrate = %v, %v", moved, err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the legacy folder should be gone")
	}
	env := filepath.Join(dir, ".corgi", "corgi_services", "db_services", "pg", ".env")
	if data, err := os.ReadFile(env); err != nil || string(data) != "PGHOST=x" {
		t.Errorf("content did not come along: %v", err)
	}
	ignore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	for _, want := range []string{"node_modules", ".corgi/corgi_services/*", "!.corgi/corgi_services/.gitignore"} {
		if !strings.Contains(string(ignore), want) {
			t.Errorf("missing %q in\n%s", want, ignore)
		}
	}

	again, err := MigrateCorgiServices(dir)
	if again || err != nil {
		t.Errorf("a second run should do nothing: %v, %v", again, err)
	}
	if noWorkspace, err := MigrateCorgiServices(""); noWorkspace || err != nil {
		t.Errorf("no workspace = %v, %v", noWorkspace, err)
	}
}

func TestMigrateRefusesWhileServicesRunOrBothFoldersExist(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "corgi_services")
	os.MkdirAll(legacy, 0o755)
	state, _ := json.Marshal(RunState{Services: []RunStateEntry{{Name: "api", Status: "running", PID: 1}}})
	os.WriteFile(filepath.Join(legacy, ".state.json"), state, 0o600)

	if _, err := MigrateCorgiServices(dir); err == nil || !strings.Contains(err.Error(), "corgi stop") {
		t.Fatalf("a running service should stop the move, got %v", err)
	}

	stopped, _ := json.Marshal(RunState{Services: []RunStateEntry{{Name: "api", Status: "stopped"}}})
	os.WriteFile(filepath.Join(legacy, ".state.json"), stopped, 0o600)
	os.MkdirAll(filepath.Join(dir, ".corgi", "corgi_services"), 0o755)
	if _, err := MigrateCorgiServices(dir); err == nil || !strings.Contains(err.Error(), "merge them by hand") {
		t.Fatalf("two folders should not be merged silently, got %v", err)
	}
}
