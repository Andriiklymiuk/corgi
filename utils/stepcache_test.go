package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashCacheKeyFiles_DeterministicAndContentSensitive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lock"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1 := hashCacheKeyFiles(dir, []string{"lock"})
	h2 := hashCacheKeyFiles(dir, []string{"lock"})
	if h1 != h2 || h1 == "" {
		t.Fatalf("want stable non-empty hash, got %q %q", h1, h2)
	}
	if err := os.WriteFile(filepath.Join(dir, "lock"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hashCacheKeyFiles(dir, []string{"lock"}) == h1 {
		t.Fatal("hash should change when content changes")
	}
}

func TestHashCacheKeyFiles_MissingIsStable(t *testing.T) {
	dir := t.TempDir()
	a := hashCacheKeyFiles(dir, []string{"nope"})
	b := hashCacheKeyFiles(dir, []string{"nope"})
	if a != b || a == "" {
		t.Fatalf("missing file should hash stably, got %q %q", a, b)
	}
}

func TestStepHash_WriteRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "0")
	if err := writeStepHash(path, "abc123"); err != nil {
		t.Fatal(err)
	}
	if got := readStepHash(path); got != "abc123" {
		t.Fatalf("want abc123, got %q", got)
	}
	if got := readStepHash(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("missing cache should read empty, got %q", got)
	}
}
