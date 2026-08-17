package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
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

// resetCachePathsFlags undoes the flag state a previous run left on the shared
// cobra command, so one subtest cannot leak --out into the next.
func resetCachePathsFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, name := range []string{"gitlab", "key"} {
			_ = cachePathsCmd.Flags().Set(name, "false")
		}
		for _, name := range []string{"out", "check", "path-prefix"} {
			_ = cachePathsCmd.Flags().Set(name, "")
		}
		// --json is a persistent flag on the shared rootCmd, so leaving it set
		// makes every later runRoot in this package emit JSON.
		_ = rootCmd.PersistentFlags().Set("json", "false")
		utils.PayloadOnStdout = false
		utils.JSONOutput = false
	})
}

func TestCachePathsPrintsThePlan(t *testing.T) {
	chdirToCompose(t)
	resetCachePathsFlags(t)

	out := captureStdout(t, func() { runRoot(t, "cache", "paths") })
	if !strings.Contains(out, filepath.Join("corgi_services", ".cache")) {
		t.Errorf("expected the step markers in the path list:\n%s", out)
	}
}

func TestCachePathsKeyOnly(t *testing.T) {
	chdirToCompose(t)
	resetCachePathsFlags(t)

	out := captureStdout(t, func() { runRoot(t, "cache", "paths", "--key") })
	if !strings.HasPrefix(strings.TrimSpace(out), "corgi-deps-") {
		t.Errorf("expected only the key:\n%s", out)
	}
}

func TestCachePathsJSON(t *testing.T) {
	chdirToCompose(t)
	resetCachePathsFlags(t)

	out := captureStdout(t, func() { runRoot(t, "cache", "paths", "--json") })
	if !strings.Contains(out, `"paths"`) || !strings.Contains(out, `"key"`) {
		t.Errorf("expected the plan as JSON:\n%s", out)
	}
}

func TestCachePathsGitLabToStdout(t *testing.T) {
	chdirToCompose(t)
	resetCachePathsFlags(t)

	out := captureStdout(t, func() { runRoot(t, "cache", "paths", "--gitlab") })
	if !strings.Contains(out, ".corgi-cache:") {
		t.Errorf("expected the GitLab job template:\n%s", out)
	}
	// The "using compose file" line belongs on stderr, or a redirect into the
	// committed file would capture it as YAML.
	if strings.Contains(out, "Using corgi-compose file") {
		t.Errorf("the compose banner leaked into stdout:\n%s", out)
	}
}

// The two flags a repo actually runs: one writes the file, the other is the
// pipeline's guard against it going stale.
func TestCachePathsGitLabOutThenCheck(t *testing.T) {
	dir := chdirToCompose(t)
	resetCachePathsFlags(t)
	out := filepath.Join(dir, ".gitlab", "corgi-cache.yml")

	runRoot(t, "cache", "paths", "--gitlab", "--out", out)
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), ".corgi-cache:") {
		t.Fatalf("unexpected content:\n%s", written)
	}
	runRoot(t, "cache", "paths", "--gitlab", "--check", out)
}

func TestCachePathsGitLabPathPrefix(t *testing.T) {
	chdirToCompose(t)
	resetCachePathsFlags(t)

	out := captureStdout(t, func() {
		runRoot(t, "cache", "paths", "--gitlab", "--path-prefix", "workspace")
	})
	if !strings.Contains(out, "workspace/corgi_services/.cache") {
		t.Errorf("expected every path under the prefix:\n%s", out)
	}
}
