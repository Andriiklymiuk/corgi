package utils

import (
	"os"
	"path/filepath"
	"strings"
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

func TestEnsureCorgiServicesIgnore_AddsAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	EnsureCorgiServicesIgnore(dir, ".cache/")
	EnsureCorgiServicesIgnore(dir, ".cache/")
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if c := strings.Count(string(data), ".cache/"); c != 1 {
		t.Fatalf("want exactly one .cache/ entry, got %d in %q", c, string(data))
	}
}

func TestCacheScopeIsolatesRelocatedWorkdir(t *testing.T) {
	root := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = root
	t.Cleanup(func() { CorgiComposePathDir = prev })

	lock := filepath.Join(root, "lock.json")
	if err := os.WriteFile(lock, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	step := BeforeStartStep{Run: "install", CacheKey: []string{"lock.json"}}

	main := Service{ServiceName: "api", AbsolutePath: root}
	run, hash := StepNeedsRun(main, 0, step, false)
	if !run {
		t.Fatal("first run must not be cached")
	}
	PersistStepHash(main, 0, hash)
	if run, _ := StepNeedsRun(main, 0, step, false); run {
		t.Fatal("second run on the same dir should be cached")
	}

	worktree := Service{ServiceName: "api", AbsolutePath: root, CacheScope: CacheScopeForDir("/elsewhere/api")}
	if run, _ := StepNeedsRun(worktree, 0, step, false); !run {
		t.Fatal("a relocated workdir must not inherit the main checkout's marker")
	}
}

func TestStepCacheDirName(t *testing.T) {
	if got := stepCacheDirName(Service{ServiceName: "api"}); got != "api" {
		t.Errorf("unscoped dir name = %q, want api", got)
	}
	if got := stepCacheDirName(Service{ServiceName: "api", CacheScope: "abc123"}); got != "api-abc123" {
		t.Errorf("scoped dir name = %q, want api-abc123", got)
	}
}

func TestCacheScopeForDirIsStableAndDistinct(t *testing.T) {
	if CacheScopeForDir("/a") != CacheScopeForDir("/a") {
		t.Error("same dir must hash the same")
	}
	if CacheScopeForDir("/a") == CacheScopeForDir("/b") {
		t.Error("different dirs must hash differently")
	}
	if len(CacheScopeForDir("/a")) != 8 {
		t.Error("scope should be 8 chars")
	}
}

func TestActiveRequiredSkipInCi(t *testing.T) {
	required := []Required{{Name: "docker"}, {Name: "tunnel-client", SkipInCi: true}}

	prev := CIMode
	t.Cleanup(func() { CIMode = prev })

	CIMode = false
	if got := ActiveRequired(required); len(got) != 2 {
		t.Errorf("outside CI nothing is skipped, got %d", len(got))
	}
	CIMode = true
	got := ActiveRequired(required)
	if len(got) != 1 || got[0].Name != "docker" {
		t.Errorf("in CI skipInCi tools must drop out, got %v", got)
	}
}

func TestStepNeedsRunWhenCachedOutputIsGone(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	svcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(svcDir, "package-lock.json"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	nodeModules := filepath.Join(svcDir, "node_modules")
	if err := os.Mkdir(nodeModules, 0o755); err != nil {
		t.Fatal(err)
	}

	svc := Service{ServiceName: "api", AbsolutePath: svcDir + "/"}
	step := BeforeStartStep{Run: "npm ci", CacheKey: []string{"package-lock.json"}}

	_, hash := StepNeedsRun(svc, 0, step, false)
	PersistStepHash(svc, 0, hash)

	if run, _ := StepNeedsRun(svc, 0, step, false); run {
		t.Fatal("marker plus node_modules present should skip")
	}

	// The markers cache restored, the node cache did not.
	if err := os.RemoveAll(nodeModules); err != nil {
		t.Fatal(err)
	}
	if run, _ := StepNeedsRun(svc, 0, step, false); !run {
		t.Fatal("a marker without its node_modules must not skip the install")
	}
}
