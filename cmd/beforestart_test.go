package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestRunCachedBeforeStart_SkipsUnchanged(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	svcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(svcDir, "lock"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := utils.Service{
		ServiceName:  "api",
		AbsolutePath: svcDir + "/",
		BeforeStart:  utils.BeforeStartSteps{{Run: "yarn", CacheKey: []string{"lock"}}},
	}

	var runs int
	runner := func(string) error { runs++; return nil }

	if err := runCachedBeforeStart(svc, false, runner); err != nil {
		t.Fatal(err)
	}
	if err := runCachedBeforeStart(svc, false, runner); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("want 1 run (second skipped), got %d", runs)
	}
}

func TestRunCachedBeforeStart_FailureNotPersisted(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	svcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(svcDir, "lock"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := utils.Service{
		ServiceName:  "api",
		AbsolutePath: svcDir + "/",
		BeforeStart:  utils.BeforeStartSteps{{Run: "yarn", CacheKey: []string{"lock"}}},
	}

	failing := func(string) error { return errors.New("boom") }
	if err := runCachedBeforeStart(svc, false, failing); err == nil {
		t.Fatal("want error from failing step")
	}

	var runs int
	if err := runCachedBeforeStart(svc, false, func(string) error { runs++; return nil }); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("failed step should not be cached; want re-run, got %d", runs)
	}
}

func TestRunCachedBeforeStart_ReinstallsAStaleRestoredOutputDir(t *testing.T) {
	prev := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = prev })

	svcDir := t.TempDir()
	lock := filepath.Join(svcDir, "package-lock.json")
	if err := os.WriteFile(lock, []byte(`{"deps":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := utils.Service{
		ServiceName:  "mobile",
		AbsolutePath: svcDir + "/",
		BeforeStart:  utils.BeforeStartSteps{{Run: "npm ci", CacheKey: []string{"package-lock.json"}}},
	}
	nodeModules := filepath.Join(svcDir, "node_modules")
	var runs int
	install := func(string) error {
		runs++
		return os.MkdirAll(nodeModules, 0o755)
	}

	if err := runCachedBeforeStart(svc, false, install); err != nil {
		t.Fatal(err)
	}
	if err := runCachedBeforeStart(svc, false, install); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("unchanged lockfile must skip, got %d runs", runs)
	}
	if _, err := os.Stat(filepath.Join(nodeModules, ".corgi-step-0")); err != nil {
		t.Fatalf("the marker must live inside node_modules: %v", err)
	}

	if err := os.WriteFile(lock, []byte(`{"deps":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCachedBeforeStart(svc, false, install); err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Fatalf("a restored node_modules with an older marker must reinstall, got %d runs", runs)
	}
}
