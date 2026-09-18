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
	PersistStepHash(svc, 0, step, hash)

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
	PersistStepHash(svc, 0, step, hash)
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
	PersistStepHash(main, 0, step, hash)
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
	PersistStepHash(svc, 0, step, hash)

	if run, _ := StepNeedsRun(svc, 0, step, false); run {
		t.Fatal("marker plus node_modules present should skip")
	}

	if err := os.RemoveAll(nodeModules); err != nil {
		t.Fatal(err)
	}
	if run, _ := StepNeedsRun(svc, 0, step, false); !run {
		t.Fatal("a marker without its node_modules must not skip the install")
	}
}

func cachedNodeService(t *testing.T) (Service, BeforeStartStep, string) {
	t.Helper()
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	svcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(svcDir, "package-lock.json"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(svcDir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	svc := Service{ServiceName: "api", AbsolutePath: svcDir + "/"}
	step := BeforeStartStep{Run: "npm ci", CacheKey: []string{"package-lock.json"}}
	return svc, step, svcDir
}

func TestStepMarkerLivesInsideTheOutputDir(t *testing.T) {
	svc, step, svcDir := cachedNodeService(t)

	run, hash := StepNeedsRun(svc, 0, step, false)
	if !run {
		t.Fatal("first run must execute")
	}
	PersistStepHash(svc, 0, step, hash)

	marker := filepath.Join(svcDir, "node_modules", ".corgi-step-0")
	if got := readStepHash(marker); got != hash {
		t.Fatalf("expected the marker at %s with %q, got %q", marker, hash, got)
	}
	if _, err := os.Stat(filepath.Join(CorgiServicesDir(), cacheDirName)); !os.IsNotExist(err) {
		t.Errorf("a step with an output dir must not also write the central marker (stat err: %v)", err)
	}
	if run, _ := StepNeedsRun(svc, 0, step, false); run {
		t.Error("unchanged lockfile plus its own marker must skip")
	}
}

func TestStepNeedsRunWhenRestoredOutputDirIsStale(t *testing.T) {
	svc, step, svcDir := cachedNodeService(t)

	_, oldHash := StepNeedsRun(svc, 0, step, false)
	PersistStepHash(svc, 0, step, oldHash)

	if err := os.WriteFile(filepath.Join(svcDir, "package-lock.json"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	if run, _ := StepNeedsRun(svc, 0, step, false); !run {
		t.Fatal("a restored node_modules carrying an older marker must not skip the install")
	}

	_, newHash := StepNeedsRun(svc, 0, step, false)
	if err := writeStepHash(stepCachePath(svc, 0), newHash); err != nil {
		t.Fatal(err)
	}
	if run, _ := StepNeedsRun(svc, 0, step, false); !run {
		t.Fatal("a central marker must not vouch for an output dir that carries a different one")
	}
}

func TestStepMarkerStaysCentralWithoutAnOutputDir(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	svcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(svcDir, "go.sum"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := Service{ServiceName: "api", AbsolutePath: svcDir + "/"}
	step := BeforeStartStep{Run: "go mod download", CacheKey: []string{"go.sum"}}

	_, hash := StepNeedsRun(svc, 0, step, false)
	PersistStepHash(svc, 0, step, hash)
	if got := readStepHash(stepCachePath(svc, 0)); got != hash {
		t.Fatalf("expected the central marker, got %q", got)
	}
	if run, _ := StepNeedsRun(svc, 0, step, false); run {
		t.Error("unchanged go.sum must skip")
	}
}

func TestStepMarkerDoesNotCreateAMissingOutputDir(t *testing.T) {
	prev := CorgiComposePathDir
	CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { CorgiComposePathDir = prev })

	svcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(svcDir, "requirements.txt"), []byte("a==1"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := Service{ServiceName: "worker", AbsolutePath: svcDir + "/"}
	step := BeforeStartStep{Run: "pip install -r requirements.txt", CacheKey: []string{"requirements.txt"}}

	_, hash := StepNeedsRun(svc, 0, step, false)
	PersistStepHash(svc, 0, step, hash)
	if _, err := os.Stat(filepath.Join(svcDir, ".venv")); !os.IsNotExist(err) {
		t.Errorf("persisting a marker must not create .venv (stat err: %v)", err)
	}
}
