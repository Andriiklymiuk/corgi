package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestMigrateCommandMovesTheFolderAndSaysSo(t *testing.T) {
	restoreMigrateFlags(t)
	dir := chdirToCompose(t)
	legacy := filepath.Join(dir, "corgi_services")
	if err := os.MkdirAll(filepath.Join(legacy, ".logs", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(legacy, ".logs", "api", "boot.log"), []byte("up"), 0o644)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("corgi_services/*\n"), 0o644)

	out := captureStdout(t, func() { runRoot(t, "migrate", "--dry-run") })
	if !strings.Contains(out, "would move") {
		t.Fatalf("--dry-run should only say what it would do:\n%s", out)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("--dry-run must not move anything")
	}

	migrateDryRun = false
	out = captureStdout(t, func() { runRoot(t, "migrate") })
	if !strings.Contains(out, "moved") {
		t.Fatalf("migrate output:\n%s", out)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the legacy folder should be gone")
	}
	log := filepath.Join(dir, ".corgi", "corgi_services", ".logs", "api", "boot.log")
	if data, err := os.ReadFile(log); err != nil || string(data) != "up" {
		t.Errorf("logs did not come along: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); !strings.Contains(string(data), ".corgi/corgi_services/*") {
		t.Errorf(".gitignore = %q", data)
	}

	out = captureStdout(t, func() { runRoot(t, "migrate") })
	if !strings.Contains(out, "nothing to move") {
		t.Fatalf("a second run should be a no-op:\n%s", out)
	}
}

func TestMigrateCommandLeavesTheAutoMigrationAlone(t *testing.T) {
	restoreMigrateFlags(t)
	dir := chdirToCompose(t)
	os.MkdirAll(filepath.Join(dir, "corgi_services"), 0o755)
	migrateDryRun = true

	captureStdout(t, func() { runRoot(t, "migrate", "--dry-run") })
	if _, err := os.Stat(filepath.Join(dir, "corgi_services")); err != nil {
		t.Fatal("loading the compose must not migrate behind --dry-run")
	}
	if !utils.SkipCorgiServicesMigration {
		t.Error("the command should have taken the move into its own hands")
	}
}

// The migrate command turns the automatic move off for its own process. In a
// test binary that process is shared, so every other cmd test would inherit it.
func restoreMigrateFlags(t *testing.T) {
	t.Helper()
	dryRun, skip := migrateDryRun, utils.SkipCorgiServicesMigration
	t.Cleanup(func() { migrateDryRun, utils.SkipCorgiServicesMigration = dryRun, skip })
}

func TestServicesInSitsBesideDbServices(t *testing.T) {
	dir := t.TempDir()
	if got, want := utils.ServicesIn(dir), filepath.Join(dir, ".corgi", "corgi_services", "services"); got != want {
		t.Errorf("ServicesIn = %q, want %q", got, want)
	}
	if got, want := utils.DbServicesIn(dir), filepath.Join(dir, ".corgi", "corgi_services", "db_services"); got != want {
		t.Errorf("DbServicesIn = %q, want %q", got, want)
	}
}

func TestMigrateCommandReportsWhyItRefused(t *testing.T) {
	restoreMigrateFlags(t)
	dir := chdirToCompose(t)
	legacy := filepath.Join(dir, "corgi_services")
	os.MkdirAll(legacy, 0o755)
	os.MkdirAll(filepath.Join(dir, ".corgi", "corgi_services"), 0o755)

	code := -1
	original := osExit
	osExit = func(c int) { code = c }
	t.Cleanup(func() { osExit = original })

	migrateDryRun = false
	stderr := captureStderr(t, func() { runRoot(t, "migrate") })
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "merge them by hand") {
		t.Errorf("stderr should say what to do:\n%s", stderr)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Error("a refused migration must leave the legacy folder alone")
	}
}
