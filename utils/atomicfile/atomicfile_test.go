package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplacesContentAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new" {
		t.Errorf("content = %q, want new", body)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file survived a successful write")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteCreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.json")
	if err := Write(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Errorf("content = %q, want hello", body)
	}
}

// The bug this package exists for: a failed rename used to leave the temp file
// on disk, and nothing else ever cleans it up.
func TestWriteRemovesTempWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	// A directory at the destination makes the rename fail while the temp
	// write itself succeeds.
	path := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(path, "child"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("data"), 0o644); err == nil {
		t.Fatal("Write succeeded, want a rename error")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file left behind after a failed rename")
	}
}

func TestWriteReportsAnUnwritableDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "state.json")
	if err := Write(path, []byte("data"), 0o644); err == nil {
		t.Fatal("Write succeeded into a missing directory, want an error")
	}
}
