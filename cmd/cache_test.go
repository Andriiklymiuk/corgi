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
func TestWriteGeneratedFileCreatesTheDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitlab", "corgi-cache.yml")
	if err := writeGeneratedFile(path, "content\n"); err != nil {
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
	if err := writeGeneratedFile(path, rendered); err != nil {
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
		for _, name := range []string{"gitlab", "key", "strict"} {
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
	if !strings.Contains(out, "workspace/.corgi/corgi_services/.cache") {
		t.Errorf("expected every path under the prefix:\n%s", out)
	}
}

// chdirToCachedCompose writes a compose whose one service opts into caching,
// so the plan has a cacheKey file to be present or missing.
func chdirToCachedCompose(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	yml := "name: test\nservices:\n  web:\n    path: ./web\n    port: 3000\n" +
		"    beforeStart:\n      - run: npm ci\n        cacheKey: [package-lock.json]\n"
	if err := os.WriteFile(filepath.Join(dir, "corgi-compose.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeLockfile(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "web", "package-lock.json"), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The CI failure this guards: the action hashes the lockfiles before
// `corgi init` clones them, gets a stable key from nothing, and the cache never
// invalidates. Silence is what let that ship stale node_modules for weeks.
func TestCachePathsWarnsWhenCacheKeyFilesAreMissing(t *testing.T) {
	chdirToCachedCompose(t)
	resetCachePathsFlags(t)
	t.Setenv("GITHUB_ACTIONS", "")

	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { runRoot(t, "cache", "paths") })
	})
	if !strings.Contains(stderr, "web/package-lock.json") {
		t.Errorf("stderr must name the missing file:\n%s", stderr)
	}
	if !strings.Contains(stderr, "corgi init") {
		t.Errorf("stderr must say to run this after corgi init:\n%s", stderr)
	}
	if strings.Contains(stdout, "package-lock.json") {
		t.Errorf("the warning must not land in the path list on stdout:\n%s", stdout)
	}
}

func TestCachePathsKeyWarnsWhenCacheKeyFilesAreMissing(t *testing.T) {
	chdirToCachedCompose(t)
	resetCachePathsFlags(t)
	t.Setenv("GITHUB_ACTIONS", "")

	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { runRoot(t, "cache", "paths", "--key") })
	})
	if !strings.Contains(stderr, "web/package-lock.json") {
		t.Errorf("--key must warn on stderr too:\n%s", stderr)
	}
	if strings.TrimSpace(stdout) == "" || strings.Contains(stdout, "\n\n") || !strings.HasPrefix(stdout, "corgi-deps-") {
		t.Errorf("--key stdout must stay the bare key:\n%q", stdout)
	}
}

// Under GitHub Actions the warning becomes a job annotation, which is where a
// person looks when a nightly goes red.
func TestCachePathsWarnsAsGitHubAnnotation(t *testing.T) {
	chdirToCachedCompose(t)
	resetCachePathsFlags(t)
	t.Setenv("GITHUB_ACTIONS", "true")

	var stderr string
	captureStdout(t, func() {
		stderr = captureStderr(t, func() { runRoot(t, "cache", "paths") })
	})
	if !strings.Contains(stderr, "::warning::") || !strings.Contains(stderr, "web/package-lock.json") {
		t.Errorf("expected a ::warning:: annotation naming the file:\n%s", stderr)
	}
}

func TestCachePathsJSONReportsMissingFiles(t *testing.T) {
	chdirToCachedCompose(t)
	resetCachePathsFlags(t)

	out := captureStdout(t, func() { runRoot(t, "cache", "paths", "--json") })
	if !strings.Contains(out, `"complete": false`) {
		t.Errorf("expected complete: false:\n%s", out)
	}
	if !strings.Contains(out, `"web/package-lock.json"`) {
		t.Errorf("expected the missing file in missingFiles:\n%s", out)
	}
}

func TestCachePathsIsQuietWhenEveryCacheKeyFileExists(t *testing.T) {
	dir := chdirToCachedCompose(t)
	writeLockfile(t, dir)
	resetCachePathsFlags(t)
	t.Setenv("GITHUB_ACTIONS", "true")

	var stderr string
	captureStdout(t, func() {
		stderr = captureStderr(t, func() { runRoot(t, "cache", "paths") })
	})
	if strings.Contains(stderr, "warning") || strings.Contains(stderr, "package-lock.json") {
		t.Errorf("nothing missing, nothing to warn about:\n%s", stderr)
	}

	out := captureStdout(t, func() { runRoot(t, "cache", "paths", "--json") })
	if !strings.Contains(out, `"complete": true`) || !strings.Contains(out, `"missingFiles": []`) {
		t.Errorf("expected complete: true with an empty missingFiles:\n%s", out)
	}
}

// exitCodeOf runs fn with osExit stubbed and returns the code it exited with,
// or -1 when it returned normally.
func exitCodeOf(t *testing.T, fn func()) int {
	t.Helper()
	previous := osExit
	code := -1
	osExit = func(c int) { code = c; panic("exit") }
	t.Cleanup(func() { osExit = previous })
	func() {
		defer func() {
			if r := recover(); r != nil && r != "exit" {
				panic(r)
			}
		}()
		fn()
	}()
	return code
}

// --strict is what the post-init cache action runs: a plan hashed from nothing
// must fail the step instead of feeding actions/cache a frozen key.
func TestCachePathsStrictExitsOneWhenFilesAreMissing(t *testing.T) {
	chdirToCachedCompose(t)
	resetCachePathsFlags(t)
	t.Setenv("GITHUB_ACTIONS", "")

	var code int
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			code = exitCodeOf(t, func() { runRoot(t, "cache", "paths", "--json", "--strict") })
		})
	})
	if code != 1 {
		t.Errorf("--strict with a missing cacheKey file must exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "web/package-lock.json") {
		t.Errorf("the failure must say which file is missing:\n%s", stderr)
	}
	if !strings.Contains(stdout, `"complete": false`) {
		t.Errorf("the plan is still printed so a log shows what was seen:\n%s", stdout)
	}
}

func TestCachePathsStrictPassesWhenFilesExist(t *testing.T) {
	dir := chdirToCachedCompose(t)
	writeLockfile(t, dir)
	resetCachePathsFlags(t)

	code := -1
	out := captureStdout(t, func() {
		code = exitCodeOf(t, func() { runRoot(t, "cache", "paths", "--json", "--strict") })
	})
	if code != -1 {
		t.Errorf("--strict with every file present must not exit, got %d", code)
	}
	if !strings.Contains(out, `"complete": true`) {
		t.Errorf("expected the complete plan:\n%s", out)
	}
}

// The two states must not share a key, or a present lockfile could hit a
// cache entry saved when it was absent.
func TestCachePathsKeyDiffersOnceTheLockfileExists(t *testing.T) {
	dir := chdirToCachedCompose(t)
	resetCachePathsFlags(t)

	missing := captureStdout(t, func() { runRoot(t, "cache", "paths", "--key") })
	writeLockfile(t, dir)
	present := captureStdout(t, func() { runRoot(t, "cache", "paths", "--key") })
	if strings.TrimSpace(missing) == strings.TrimSpace(present) {
		t.Errorf("key must change once the file exists: %q", missing)
	}
}
