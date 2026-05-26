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

func TestStepNeedsRun_NoCacheKeyAlwaysRuns(t *testing.T) {
	run, hash := StepNeedsRun(Service{ServiceName: "s", AbsolutePath: t.TempDir() + "/"}, 0, BeforeStartStep{Run: "x"}, false)
	if !run || hash != "" {
		t.Fatalf("no cacheKey should always run with empty hash, got run=%v hash=%q", run, hash)
	}
}

func TestStepNeedsRun_SkipsWhenUnchanged(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	svcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(svcDir, "lock"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := Service{ServiceName: "api", AbsolutePath: svcDir + "/"}
	step := BeforeStartStep{Run: "yarn", CacheKey: []string{"lock"}}

	run, hash := StepNeedsRun(svc, 0, step, false)
	if !run {
		t.Fatal("first run should execute")
	}
	PersistStepHash(svc, 0, hash)

	run2, _ := StepNeedsRun(svc, 0, step, false)
	if run2 {
		t.Fatal("unchanged inputs should skip")
	}
}

func TestStepNeedsRun_NoCacheForcesRun(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })
	svcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(svcDir, "lock"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := Service{ServiceName: "api", AbsolutePath: svcDir + "/"}
	step := BeforeStartStep{Run: "yarn", CacheKey: []string{"lock"}}
	_, hash := StepNeedsRun(svc, 0, step, false)
	PersistStepHash(svc, 0, hash)
	if run, _ := StepNeedsRun(svc, 0, step, true); !run {
		t.Fatal("--no-cache should force run")
	}
}

func TestHasCacheKeys(t *testing.T) {
	if (BeforeStartSteps{{Run: "a"}}).HasCacheKeys() {
		t.Fatal("no cacheKey -> false")
	}
	if !(BeforeStartSteps{{Run: "a", CacheKey: []string{"x"}}}).HasCacheKeys() {
		t.Fatal("cacheKey -> true")
	}
}
