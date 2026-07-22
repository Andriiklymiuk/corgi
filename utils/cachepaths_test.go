package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planFor(t *testing.T, services []Service) CachePlan {
	t.Helper()
	return CachePathsFor(&CorgiCompose{Services: services})
}

func containsPath(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}

func TestCachePathsCoversEachEcosystem(t *testing.T) {
	cases := []struct {
		lockfile string
		wantPath string
		wantHome string
	}{
		{"package-lock.json", "api/node_modules", "~/.npm"},
		{"bun.lock", "api/node_modules", "~/.bun/install/cache"},
		{"pnpm-lock.yaml", "api/node_modules", "~/.local/share/pnpm/store"},
		{"yarn.lock", "api/node_modules", "~/.cache/yarn"},
		{"requirements.txt", "api/.venv", "~/.cache/pip"},
		{"uv.lock", "api/.venv", "~/.cache/uv"},
		{"Cargo.lock", "api/target", "~/.cargo/registry"},
		{"go.sum", "", "~/go/pkg/mod"},
		{"Gemfile.lock", "api/vendor/bundle", "~/.gem"},
		{"composer.lock", "api/vendor", "~/.composer/cache"},
		{"mix.lock", "api/deps", "~/.hex"},
	}
	for _, c := range cases {
		plan := planFor(t, []Service{{
			ServiceName: "api",
			Path:        "./api",
			BeforeStart: BeforeStartSteps{{Run: "install", CacheKey: []string{c.lockfile}}},
		}})
		if c.wantPath != "" && !containsPath(plan.Paths, c.wantPath) {
			t.Errorf("%s: expected %q in %v", c.lockfile, c.wantPath, plan.Paths)
		}
		if !containsPath(plan.Paths, c.wantHome) {
			t.Errorf("%s: expected %q in %v", c.lockfile, c.wantHome, plan.Paths)
		}
	}
}

// The markers and the dependency directories must always travel together.
func TestCachePathsAlwaysIncludesTheStepMarkers(t *testing.T) {
	plan := planFor(t, nil)
	want := filepath.Join("corgi_services", cacheDirName)
	if !containsPath(plan.Paths, want) {
		t.Errorf("expected %q in %v", want, plan.Paths)
	}
}

func TestCachePathsIgnoresServicesWithoutACacheKey(t *testing.T) {
	plan := planFor(t, []Service{{
		ServiceName: "api",
		Path:        "./api",
		BeforeStart: BeforeStartSteps{{Run: "npm install"}},
	}})
	for _, p := range plan.Paths {
		if strings.Contains(p, "node_modules") {
			t.Errorf("a step without a cacheKey cannot be skipped, so nothing to cache; got %v", plan.Paths)
		}
	}
}

func TestCachePathsKeyTracksLockfileContents(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "package-lock.json")
	if err := os.WriteFile(lock, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := []Service{{
		ServiceName:  "api",
		Path:         "./api",
		AbsolutePath: dir,
		BeforeStart:  BeforeStartSteps{{Run: "npm ci", CacheKey: []string{"package-lock.json"}}},
	}}

	first := planFor(t, svc).Key
	if planFor(t, svc).Key != first {
		t.Error("unchanged input must give a stable key")
	}

	if err := os.WriteFile(lock, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if planFor(t, svc).Key == first {
		t.Error("a changed lockfile must change the key")
	}
}

func TestCachePathsIsDeterministic(t *testing.T) {
	svc := []Service{
		{ServiceName: "web", Path: "./web", BeforeStart: BeforeStartSteps{{Run: "i", CacheKey: []string{"yarn.lock"}}}},
		{ServiceName: "api", Path: "./api", BeforeStart: BeforeStartSteps{{Run: "i", CacheKey: []string{"package-lock.json"}}}},
	}
	a, b := planFor(t, svc), planFor(t, svc)
	if strings.Join(a.Paths, ",") != strings.Join(b.Paths, ",") || a.Key != b.Key {
		t.Error("service order must not affect the output")
	}
}

func groupKey(plan CachePlan, id string) string {
	for _, g := range plan.Groups {
		if g.ID == id {
			return g.Key
		}
	}
	return ""
}

// The whole point of grouping: one language's lockfile churn must not evict
// another language's packages.
func TestCacheGroupsAreIndependentPerEcosystem(t *testing.T) {
	nodeDir, pyDir := t.TempDir(), t.TempDir()
	nodeLock := filepath.Join(nodeDir, "package-lock.json")
	pyLock := filepath.Join(pyDir, "requirements.txt")
	if err := os.WriteFile(nodeLock, []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pyLock, []byte("fastapi==1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := []Service{
		{
			ServiceName: "web", Path: "./web", AbsolutePath: nodeDir,
			BeforeStart: BeforeStartSteps{{Run: "npm ci", CacheKey: []string{"package-lock.json"}}},
		},
		{
			ServiceName: "worker", Path: "./worker", AbsolutePath: pyDir,
			BeforeStart: BeforeStartSteps{{Run: "pip install -r requirements.txt", CacheKey: []string{"requirements.txt"}}},
		},
	}

	before := planFor(t, svc)
	if groupKey(before, "node") == "" || groupKey(before, "python") == "" {
		t.Fatalf("expected a node and a python group, got %+v", before.Groups)
	}

	if err := os.WriteFile(nodeLock, []byte(`{"v":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	after := planFor(t, svc)

	if groupKey(after, "node") == groupKey(before, "node") {
		t.Error("changing a node lockfile must change the node key")
	}
	if groupKey(after, "python") != groupKey(before, "python") {
		t.Error("changing a node lockfile must NOT change the python key — that is the whole point of grouping")
	}
	if after.Key == before.Key {
		t.Error("the aggregate key must still track every lockfile")
	}
}

// A restored step marker next to a dependency directory that did not come back
// would make corgi skip an install whose output is missing.
func TestCacheMarkersAreKeyedOnEveryLockfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := []Service{{
		ServiceName: "web", Path: "./web", AbsolutePath: dir,
		BeforeStart: BeforeStartSteps{{Run: "npm ci", CacheKey: []string{"package-lock.json"}}},
	}}

	plan := planFor(t, svc)
	markers := groupKey(plan, "markers")
	if markers == "" {
		t.Fatal("expected a markers group")
	}
	if !strings.HasPrefix(markers, plan.Key) {
		t.Errorf("markers key %q must be derived from the aggregate key %q", markers, plan.Key)
	}
	for _, g := range plan.Groups {
		if g.ID == "markers" {
			continue
		}
		if containsPath(g.Paths, filepath.Join("corgi_services", cacheDirName)) {
			t.Errorf("group %s must not carry the step markers", g.ID)
		}
	}
}
