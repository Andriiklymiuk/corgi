package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CachePlan is what a CI cache action needs: the directories worth persisting
// and a key that changes when any dependency declaration changes.
type CachePlan struct {
	Paths []string `json:"paths"`
	Key   string   `json:"key"`
	// Groups splits the same plan per ecosystem, so a change to one language's
	// lockfile does not evict every other language's packages.
	Groups []CacheGroup `json:"groups"`
	// Hints are install steps that could opt into caching but have not. An
	// empty plan is otherwise indistinguishable from a workspace with nothing
	// worth caching.
	Hints []CacheHint `json:"hints,omitempty"`
	// MissingFiles are cacheKey files that did not exist when the key was
	// hashed, relative to the compose file. A key hashed from nothing is stable
	// across runs — in CI that means the cache never invalidates — so a plan
	// with any is not one to feed a cache action.
	MissingFiles []string `json:"missingFiles"`
	// Complete is false when MissingFiles is not empty.
	Complete bool `json:"complete"`
}

// CacheGroup is one independently keyed slice of the plan.
type CacheGroup struct {
	ID    string   `json:"id"`
	Paths []string `json:"paths"`
	Key   string   `json:"key"`
	// PathsText is Paths joined by newlines. A GitHub Actions expression cannot
	// build a newline-separated string, and that is the only separator
	// actions/cache accepts, so the join has to happen here.
	PathsText string `json:"pathsText"`
	// RestorePrefix feeds actions/cache's restore-keys: it matches any earlier
	// key of the same ecosystem, so a lockfile change starts from the previous
	// packages instead of empty. Empty for the markers group — corgi re-hashes
	// every cacheKey before trusting a marker, so restoring stale ones buys
	// nothing.
	RestorePrefix string `json:"restorePrefix,omitempty"`
	// MissingFiles are this group's cacheKey files that did not exist at hash
	// time; see CachePlan.MissingFiles.
	MissingFiles []string `json:"missingFiles"`
}

// ecosystem maps a lockfile to the directories a build of it produces: one
// inside the service, and one shared package-manager cache in $HOME.
type ecosystem struct {
	lockfile string
	// group buckets lockfiles that install into the same place, so npm and bun
	// share one cache entry while pip keeps its own.
	group string
	// serviceDirs are relative to the service; homeDirs to the user's home.
	serviceDirs []string
	homeDirs    []string
}

// Ordered most specific first: a repo with both pnpm-lock.yaml and a stray
// package-lock.json should be read as pnpm.
var ecosystems = []ecosystem{
	{"bun.lock", "node", []string{"node_modules"}, []string{"~/.bun/install/cache"}},
	{"bun.lockb", "node", []string{"node_modules"}, []string{"~/.bun/install/cache"}},
	{"pnpm-lock.yaml", "node", []string{"node_modules"}, []string{"~/.local/share/pnpm/store"}},
	{"yarn.lock", "node", []string{"node_modules"}, []string{"~/.cache/yarn"}},
	{"package-lock.json", "node", []string{"node_modules"}, []string{"~/.npm"}},
	{"uv.lock", "python", []string{".venv"}, []string{"~/.cache/uv"}},
	{"poetry.lock", "python", []string{".venv"}, []string{"~/.cache/pypoetry"}},
	{"Pipfile.lock", "python", []string{".venv"}, []string{"~/.cache/pipenv"}},
	{"requirements.txt", "python", []string{".venv"}, []string{"~/.cache/pip"}},
	{"go.sum", "go", nil, []string{"~/go/pkg/mod"}},
	{"Cargo.lock", "rust", []string{"target"}, []string{"~/.cargo/registry", "~/.cargo/git"}},
	{"Gemfile.lock", "ruby", []string{"vendor/bundle"}, []string{"~/.gem"}},
	{"composer.lock", "php", []string{"vendor"}, []string{"~/.composer/cache"}},
	{"mix.lock", "elixir", []string{"deps", "_build"}, []string{"~/.hex"}},
	{"pubspec.lock", "dart", nil, []string{"~/.pub-cache"}},
	{"Gemfile", "ruby", []string{"vendor/bundle"}, []string{"~/.gem"}},
}

// CachePathsFor derives the cache plan from every service's beforeStart
// cacheKey files. A service opts in by declaring a cacheKey; without one corgi
// cannot skip the install anyway, so caching its output would be misleading.
func CachePathsFor(corgi *CorgiCompose) CachePlan {
	acc := collectCacheKeys(corgi)
	aggregate := "corgi-deps-" + hex.EncodeToString(acc.aggregateHash.Sum(nil))[:16]
	groups := acc.cacheGroups()

	// corgi's own step markers say "this install already ran". A step with a
	// dependency directory keeps its marker inside that directory, so the two
	// travel in one cache entry; the central markers serve the steps without
	// one (go.sum). Restoring those next to output that did NOT come back
	// would make corgi skip an install whose output is missing, so they are
	// keyed on every lockfile at once: any change and the markers stay behind
	// while each unchanged ecosystem still restores its packages.
	markers := filepath.Join(CorgiServicesRel(), cacheDirName)
	acc.paths[markers] = true
	if len(groups) > 0 {
		groups = append(groups, CacheGroup{
			ID:           "markers",
			Paths:        []string{markers},
			Key:          aggregate + "-markers",
			PathsText:    markers,
			MissingFiles: sortedPathSet(acc.missing),
		})
	}

	return CachePlan{
		Paths:        sortedPathSet(acc.paths),
		Key:          aggregate,
		Groups:       groups,
		Hints:        CacheOptInHints(corgi),
		MissingFiles: sortedPathSet(acc.missing),
		Complete:     len(acc.missing) == 0,
	}
}

type cacheAccumulator struct {
	paths         map[string]bool
	missing       map[string]bool
	aggregateHash hash.Hash
	groupPaths    map[string]map[string]bool
	groupHash     map[string]hash.Hash
	groupMissing  map[string]map[string]bool
}

func collectCacheKeys(corgi *CorgiCompose) *cacheAccumulator {
	acc := &cacheAccumulator{
		paths:         map[string]bool{},
		missing:       map[string]bool{},
		aggregateHash: sha256.New(),
		groupPaths:    map[string]map[string]bool{},
		groupHash:     map[string]hash.Hash{},
		groupMissing:  map[string]map[string]bool{},
	}
	for _, service := range sortedServices(corgi) {
		for _, step := range service.BeforeStart {
			for _, key := range step.CacheKey {
				acc.recordCacheKey(service, key)
			}
		}
	}
	return acc
}

func (acc *cacheAccumulator) recordCacheKey(service Service, key string) {
	addEcosystemPaths(acc.paths, service, key)
	digest, present := hashFileContents(service.AbsolutePath, key)
	line := fmt.Sprintf("%s\x00%s\x00%s\n", service.ServiceName, key, digest)
	fmt.Fprint(acc.aggregateHash, line)
	missingRel := filepath.Join(serviceRelativeDir(service), key)
	if !present {
		acc.missing[missingRel] = true
	}

	id := groupFor(key)
	if id == "" {
		return
	}
	if acc.groupPaths[id] == nil {
		acc.groupPaths[id] = map[string]bool{}
		acc.groupHash[id] = sha256.New()
		acc.groupMissing[id] = map[string]bool{}
	}
	addEcosystemPaths(acc.groupPaths[id], service, key)
	fmt.Fprint(acc.groupHash[id], line)
	if !present {
		acc.groupMissing[id][missingRel] = true
	}
}

func (acc *cacheAccumulator) cacheGroups() []CacheGroup {
	var groups []CacheGroup
	for _, id := range sortedGroupIDs(acc.groupPaths) {
		p := sortedPathSet(acc.groupPaths[id])
		groups = append(groups, CacheGroup{
			ID:            id,
			Paths:         p,
			Key:           fmt.Sprintf("corgi-deps-%s-%s", id, hex.EncodeToString(acc.groupHash[id].Sum(nil))[:16]),
			PathsText:     strings.Join(p, "\n"),
			RestorePrefix: fmt.Sprintf("corgi-deps-%s-", id),
			MissingFiles:  sortedPathSet(acc.groupMissing[id]),
		})
	}
	return groups
}

// serviceOutputDirs returns the directories installing this lockfile produces
// inside the service. Nil when the key is not a lockfile corgi knows, or when
// the ecosystem installs only into a shared home cache.
func serviceOutputDirs(cacheKey string) []string {
	name := filepath.Base(cacheKey)
	for _, eco := range ecosystems {
		if name == eco.lockfile {
			return eco.serviceDirs
		}
	}
	return nil
}

func groupFor(cacheKey string) string {
	name := filepath.Base(cacheKey)
	for _, eco := range ecosystems {
		if name == eco.lockfile {
			return eco.group
		}
	}
	return ""
}

func sortedGroupIDs(m map[string]map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func addEcosystemPaths(paths map[string]bool, service Service, cacheKey string) {
	name := filepath.Base(cacheKey)
	for _, eco := range ecosystems {
		if name != eco.lockfile {
			continue
		}
		// Relative to the compose file, which is where a CI cache action runs.
		dir := serviceRelativeDir(service)
		for _, d := range eco.serviceDirs {
			paths[filepath.Join(dir, d)] = true
		}
		for _, d := range eco.homeDirs {
			paths[d] = true
		}
		return
	}
}

// serviceRelativeDir prefers the declared path so the result is portable
// between a laptop and a runner; AbsolutePath would bake in a home directory.
func serviceRelativeDir(service Service) string {
	if service.Path != "" {
		return filepath.Clean(strings.TrimPrefix(service.Path, "./"))
	}
	return service.ServiceName
}

// hashFileContents returns the file's digest and whether it exists. A missing
// file hashes to a fixed marker so the key stays deterministic; the caller
// decides whether that is worth warning about.
func hashFileContents(baseDir, rel string) (digest string, present bool) {
	data, err := os.ReadFile(filepath.Join(baseDir, rel))
	if err != nil {
		return "MISSING", false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true
}

func sortedServices(corgi *CorgiCompose) []Service {
	out := make([]Service, len(corgi.Services))
	copy(out, corgi.Services)
	sort.Slice(out, func(i, j int) bool { return out[i].ServiceName < out[j].ServiceName })
	return out
}

func sortedPathSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
