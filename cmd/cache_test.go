package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The generated file is only trustworthy while something fails when it stops
// matching the compose file it came from.
func TestCheckGitLabCacheFileDetectsDrift(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corgi-cache.yml")
	if err := os.WriteFile(path, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := checkGitLabCacheFile(path, "fresh\n")
	if err == nil {
		t.Fatal("expected drift to be reported")
	}
	if !strings.Contains(err.Error(), "--out") {
		t.Errorf("the error must say how to fix it, got: %v", err)
	}
}

func TestCheckGitLabCacheFileAcceptsAMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corgi-cache.yml")
	if err := os.WriteFile(path, []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkGitLabCacheFile(path, "same\n"); err != nil {
		t.Errorf("identical content must pass, got: %v", err)
	}
}

// A missing file is the common first run, and "no such file" alone does not
// tell anyone what to do about it.
func TestCheckGitLabCacheFileExplainsAMissingFile(t *testing.T) {
	err := checkGitLabCacheFile(filepath.Join(t.TempDir(), "nope.yml"), "x")
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !strings.Contains(err.Error(), "--gitlab --out") {
		t.Errorf("expected the generate command in the error, got: %v", err)
	}
}

// --out is what a repo runs once; it has to create .gitlab/ on the way.
func TestWriteGitLabCacheFileCreatesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitlab", "corgi-cache.yml")
	if err := writeGitLabCacheFile(path, "content\n"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "content\n" {
		t.Errorf("got %q", got)
	}
}

// Round-tripping is the whole contract: what --out writes is what --check
// accepts, or every CI run fails on a file it just generated.
func TestGitLabCacheWriteThenCheckRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitlab", "corgi-cache.yml")
	rendered := "# generated\n.corgi-cache:\n  cache: []\n"
	if err := writeGitLabCacheFile(path, rendered); err != nil {
		t.Fatal(err)
	}
	if err := checkGitLabCacheFile(path, rendered); err != nil {
		t.Errorf("write then check must round-trip, got: %v", err)
	}
}
