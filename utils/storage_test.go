package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDBPathExistsCreates(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "deeply", "nested", "file.txt")
	if err := ensureDBPathExists(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(target)); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

func TestEnsureDBPathExistsExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "x.txt")
	if err := ensureDBPathExists(target); err != nil {
		t.Errorf("err: %v", err)
	}
}

func TestSaveAndListExecPath(t *testing.T) {
	prev := storageFilePath
	storageFilePath = filepath.Join(t.TempDir(), "exec_paths.txt")
	storageInitOnce.Do(func() {})
	t.Cleanup(func() {
		storageFilePath = prev
	})

	src := t.TempDir()
	if err := SaveExecPath("proj1", "desc1", src); err != nil {
		t.Fatal(err)
	}

	got, err := ListExecPaths()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ep := range got {
		if ep.Name == "proj1" && ep.Description == "desc1" {
			found = true
		}
	}
	if !found {
		t.Errorf("not found in %+v", got)
	}
}

func TestClearExecPaths(t *testing.T) {
	prev := storageFilePath
	storageFilePath = filepath.Join(t.TempDir(), "exec_paths.txt")
	t.Cleanup(func() { storageFilePath = prev })

	if err := os.WriteFile(storageFilePath, []byte("a|b|c\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ClearExecPaths(); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(storageFilePath)
	if len(body) != 0 {
		t.Errorf("expected empty, got %q", body)
	}
}

func TestSaveExecPathUpdatesExisting(t *testing.T) {
	prev := storageFilePath
	storageFilePath = filepath.Join(t.TempDir(), "exec_paths.txt")
	t.Cleanup(func() { storageFilePath = prev })

	src := t.TempDir()
	if err := SaveExecPath("p1", "d1", src); err != nil {
		t.Fatal(err)
	}
	if err := SaveExecPath("p2", "d2", src); err != nil {
		t.Fatal(err)
	}

	got, _ := ListExecPaths()
	count := 0
	for _, ep := range got {
		abs, _ := filepath.Abs(src)
		if ep.Path == abs {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entry, got %d", count)
	}
}
