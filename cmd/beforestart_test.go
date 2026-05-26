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

	// failed step must not have cached → next run executes again
	var runs int
	if err := runCachedBeforeStart(svc, false, func(string) error { runs++; return nil }); err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("failed step should not be cached; want re-run, got %d", runs)
	}
}
