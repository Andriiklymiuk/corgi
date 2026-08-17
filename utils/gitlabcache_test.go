package utils

import (
	"strings"
	"testing"
)

func gitlabYAMLFor(t *testing.T, services []Service, opts GitLabCacheOptions) string {
	t.Helper()
	return GitLabCacheYAML(CachePathsFor(&CorgiCompose{Services: services}), opts)
}

func nodeService(name string, lockfile string) Service {
	return Service{
		ServiceName: name,
		Path:        "./" + name,
		BeforeStart: BeforeStartSteps{{Run: "npm ci", CacheKey: []string{lockfile}}},
	}
}

// GitLab refuses to cache anything outside the project directory, so every
// "~/..." path corgi emits has to become an in-project directory plus the env
// var that puts the package manager's cache there.
func TestGitLabCacheNeverEmitsHomePaths(t *testing.T) {
	out := gitlabYAMLFor(t, []Service{
		nodeService("api", "package-lock.json"),
		{
			ServiceName: "ela",
			Path:        "./ela",
			BeforeStart: BeforeStartSteps{{Run: "uv sync", CacheKey: []string{"uv.lock"}}},
		},
	}, GitLabCacheOptions{})

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "~/") || strings.Contains(line, "$HOME") {
			t.Errorf("home path leaked into GitLab cache config: %q", line)
		}
	}
	for _, want := range []string{"npm_config_cache:", "UV_CACHE_DIR:", "$CI_PROJECT_DIR/.corgi-cache/npm"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
}

// The service repos are cloned during the job, so no lockfile exists when
// GitLab computes a key. Branch-scoped keys with a fallback to the default
// branch are what is actually available; corgi's own markers re-validate.
func TestGitLabCacheKeysAreBranchScopedWithFallbacks(t *testing.T) {
	out := gitlabYAMLFor(t, []Service{
		nodeService("api", "package-lock.json"),
		nodeService("web", "package-lock.json"),
	}, GitLabCacheOptions{})

	if !strings.Contains(out, `key: "corgi-deps-node-$CI_COMMIT_REF_SLUG"`) {
		t.Errorf("expected a branch-scoped key:\n%s", out)
	}
	if !strings.Contains(out, `- "corgi-deps-node-$CI_DEFAULT_BRANCH"`) {
		t.Errorf("a new branch must start from the default branch's cache:\n%s", out)
	}
	if strings.Contains(stripComments(out), "files:") {
		t.Errorf("key:files cannot work on lockfiles cloned mid-job:\n%s", out)
	}
}

// A red e2e run still paid for every install. Dropping the cache on failure
// makes the retry pay again.
func TestGitLabCacheSavesEvenWhenTheJobFails(t *testing.T) {
	out := gitlabYAMLFor(t, []Service{nodeService("api", "package-lock.json")}, GitLabCacheOptions{})
	if !strings.Contains(out, "when: always") {
		t.Errorf("expected the cache to be saved on failure too:\n%s", out)
	}
}

// GitLab allows four cache entries per job. The markers must always be one of
// them, so the ecosystems are what gets merged when there are too many.
func TestGitLabCacheNeverExceedsFourEntries(t *testing.T) {
	out := gitlabYAMLFor(t, []Service{
		nodeService("api", "package-lock.json"),
		{ServiceName: "py", Path: "./py", BeforeStart: BeforeStartSteps{{CacheKey: []string{"uv.lock"}}}},
		{ServiceName: "go", Path: "./go", BeforeStart: BeforeStartSteps{{CacheKey: []string{"go.sum"}}}},
		{ServiceName: "rb", Path: "./rb", BeforeStart: BeforeStartSteps{{CacheKey: []string{"Gemfile.lock"}}}},
		{ServiceName: "rs", Path: "./rs", BeforeStart: BeforeStartSteps{{CacheKey: []string{"Cargo.lock"}}}},
	}, GitLabCacheOptions{})

	if got := strings.Count(out, "\n    - key:"); got > 4 {
		t.Errorf("GitLab caps a job at four caches, got %d:\n%s", got, out)
	}
	if !strings.Contains(out, "corgi_services/.cache") {
		t.Errorf("the step markers must survive the merge:\n%s", out)
	}
}

// A workspace with no cacheKey anywhere still has markers worth keeping, and
// the file must stay valid rather than render an empty cache list.
func TestGitLabCacheHandlesAPlanWithNoGroups(t *testing.T) {
	out := gitlabYAMLFor(t, nil, GitLabCacheOptions{})

	if !strings.Contains(out, "corgi_services/.cache") {
		t.Errorf("expected the markers entry:\n%s", out)
	}
	if strings.Contains(out, "cache: []") || strings.Contains(out, "cache:\n\n") {
		t.Errorf("expected a usable cache block:\n%s", out)
	}
	if !strings.Contains(out, "cacheKey") {
		t.Errorf("expected a note telling the user how to get real caching:\n%s", out)
	}
}

// The workspace repo is usually cloned into a subdirectory of the job, while
// GitLab resolves every cache path against the project root.
func TestGitLabCachePrefixesEveryPath(t *testing.T) {
	out := gitlabYAMLFor(t, []Service{nodeService("api", "package-lock.json")},
		GitLabCacheOptions{PathPrefix: "workspace"})

	if !strings.Contains(out, "workspace/api/node_modules") {
		t.Errorf("expected the prefix on service paths:\n%s", out)
	}
	if !strings.Contains(out, "workspace/corgi_services/.cache") {
		t.Errorf("expected the prefix on the markers:\n%s", out)
	}
}

// The whole point of generating the file is that it can be diffed against the
// compose file later, so the same input must render byte-identically.
func TestGitLabCacheIsDeterministic(t *testing.T) {
	services := []Service{nodeService("api", "package-lock.json"), nodeService("web", "package-lock.json")}
	first := gitlabYAMLFor(t, services, GitLabCacheOptions{})
	second := gitlabYAMLFor(t, services, GitLabCacheOptions{})
	if first != second {
		t.Errorf("render is not deterministic:\n%s\n---\n%s", first, second)
	}
}

// The generated file is included by the pipeline, so the job names it defines
// are part of the contract with gitlab/corgi.yml.
func TestGitLabCacheDefinesTheExpectedTemplateName(t *testing.T) {
	out := gitlabYAMLFor(t, []Service{nodeService("api", "package-lock.json")}, GitLabCacheOptions{})
	if !strings.HasPrefix(strings.TrimSpace(stripComments(out)), ".corgi-cache:") {
		t.Errorf("expected a .corgi-cache template:\n%s", out)
	}
}

func stripComments(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// A home directory corgi knows but the GitLab table does not must be dropped,
// not emitted: GitLab rejects any cache path outside the project.
func TestGitLabCacheDropsAnUnmappedHomePath(t *testing.T) {
	plan := CachePlan{
		Paths: []string{"api/node_modules", "~/.some-new-tool"},
		Key:   "corgi-deps-x",
		Groups: []CacheGroup{
			{ID: "node", Paths: []string{"api/node_modules", "~/.some-new-tool"}},
			{ID: "markers", Paths: []string{"corgi_services/.cache"}},
		},
	}
	out := GitLabCacheYAML(plan, GitLabCacheOptions{})
	if strings.Contains(out, ".some-new-tool") {
		t.Errorf("an unmapped home path must be dropped:\n%s", out)
	}
	if !strings.Contains(out, "api/node_modules") {
		t.Errorf("the in-project path must survive:\n%s", out)
	}
}

// Merging the tail can bring the same shared directory in twice; a duplicated
// cache path is a config GitLab has to be handed only once.
func TestGitLabCacheDedupesPathsWhenGroupsMerge(t *testing.T) {
	shared := "packages/node_modules"
	groups := []CacheGroup{
		{ID: "node", Paths: []string{"api/node_modules"}},
		{ID: "python", Paths: []string{"py/.venv"}},
		{ID: "ruby", Paths: []string{shared}},
		{ID: "rust", Paths: []string{shared}},
		{ID: "markers", Paths: []string{"corgi_services/.cache"}},
	}
	out := GitLabCacheYAML(CachePlan{Paths: []string{shared}, Groups: groups}, GitLabCacheOptions{})

	if got := strings.Count(out, "- "+shared+"\n"); got != 1 {
		t.Errorf("expected the shared path once, got %d:\n%s", got, out)
	}
}
