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
	paths := map[string]bool{}
	aggregateHash := sha256.New()

	groupPaths := map[string]map[string]bool{}
	groupHash := map[string]hash.Hash{}

	for _, service := range sortedServices(corgi) {
		for _, step := range service.BeforeStart {
			for _, key := range step.CacheKey {
				addEcosystemPaths(paths, service, key)
				line := fmt.Sprintf("%s\x00%s\x00%s\n",
					service.ServiceName, key, hashFileContents(service.AbsolutePath, key))
				fmt.Fprint(aggregateHash, line)

				id := groupFor(key)
				if id == "" {
					continue
				}
				if groupPaths[id] == nil {
					groupPaths[id] = map[string]bool{}
					groupHash[id] = sha256.New()
				}
				addEcosystemPaths(groupPaths[id], service, key)
				fmt.Fprint(groupHash[id], line)
			}
		}
	}

	aggregate := "corgi-deps-" + hex.EncodeToString(aggregateHash.Sum(nil))[:16]

	var groups []CacheGroup
	for _, id := range sortedGroupIDs(groupPaths) {
		p := sortedPathSet(groupPaths[id])
		groups = append(groups, CacheGroup{
			ID:        id,
			Paths:     p,
			Key:       fmt.Sprintf("corgi-deps-%s-%s", id, hex.EncodeToString(groupHash[id].Sum(nil))[:16]),
			PathsText: strings.Join(p, "\n"),
		})
	}

	// corgi's own step markers say "this install already ran". Restoring them
	// next to a dependency directory that did NOT come back would make corgi
	// skip an install whose output is missing, so they are keyed on every
	// lockfile at once: any change and the markers stay behind while each
	// unchanged ecosystem still restores its packages.
	markers := filepath.Join("corgi_services", cacheDirName)
	paths[markers] = true
	if len(groups) > 0 {
		groups = append(groups, CacheGroup{
			ID:        "markers",
			Paths:     []string{markers},
			Key:       aggregate + "-markers",
			PathsText: markers,
		})
	}

	return CachePlan{Paths: sortedPathSet(paths), Key: aggregate, Groups: groups}
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

func hashFileContents(baseDir, rel string) string {
	data, err := os.ReadFile(filepath.Join(baseDir, rel))
	if err != nil {
		return "MISSING"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
