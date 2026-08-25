package cmd

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMigrateLegacyAgentDirMovesDataPreservingModes(t *testing.T) {
	// Reset the once so this test actually runs the migration.
	agentMigrateOnce = sync.Once{}

	base := t.TempDir()
	// Point CorgiDataDir (legacy source) at base via the override, and put agent
	// data there with a secret 0600 file.
	t.Setenv("CORGI_DATA_DIR", base)
	legacy := filepath.Join(base, "agent")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.yml"), []byte("version: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// The destination is a different per-user dir. With the override set,
	// legacy==newDir and nothing moves — so drive the mover directly with a
	// distinct target to exercise the copy/rename.
	newDir := filepath.Join(t.TempDir(), "corgi", "agent")
	migrateLegacyAgentDir(newDir)

	// config.yml must now be at the new dir, still 0600.
	info, err := os.Stat(filepath.Join(newDir, "config.yml"))
	if err != nil {
		t.Fatalf("migrated file missing: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("migrated config mode = %04o; credential files must stay owner-only", perm)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		// rename removes it; copy-fallback removes it too.
		t.Error("legacy agent dir should be gone after the move")
	}
}

func TestMigrateIsANoOpWhenDestinationExists(t *testing.T) {
	agentMigrateOnce = sync.Once{}
	base := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", base)
	if err := os.MkdirAll(filepath.Join(base, "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	newDir := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "keep.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	migrateLegacyAgentDir(newDir)
	// An existing destination must not be clobbered by legacy data.
	if data, _ := os.ReadFile(filepath.Join(newDir, "keep.txt")); string(data) != "mine" {
		t.Error("migration must not overwrite an existing destination")
	}
}
