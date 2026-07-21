package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
}

// ecosystem maps a lockfile to the directories a build of it produces: one
// inside the service, and one shared package-manager cache in $HOME.
type ecosystem struct {
	lockfile string
	// serviceDirs are relative to the service; homeDirs to the user's home.
	serviceDirs []string
	homeDirs    []string
}

// Ordered most specific first: a repo with both pnpm-lock.yaml and a stray
// package-lock.json should be read as pnpm.
var ecosystems = []ecosystem{
	{"bun.lock", []string{"node_modules"}, []string{"~/.bun/install/cache"}},
	{"bun.lockb", []string{"node_modules"}, []string{"~/.bun/install/cache"}},
	{"pnpm-lock.yaml", []string{"node_modules"}, []string{"~/.local/share/pnpm/store"}},
	{"yarn.lock", []string{"node_modules"}, []string{"~/.cache/yarn"}},
	{"package-lock.json", []string{"node_modules"}, []string{"~/.npm"}},
	{"uv.lock", []string{".venv"}, []string{"~/.cache/uv"}},
	{"poetry.lock", []string{".venv"}, []string{"~/.cache/pypoetry"}},
	{"Pipfile.lock", []string{".venv"}, []string{"~/.cache/pipenv"}},
	{"requirements.txt", []string{".venv"}, []string{"~/.cache/pip"}},
	{"go.sum", nil, []string{"~/go/pkg/mod"}},
	{"Cargo.lock", []string{"target"}, []string{"~/.cargo/registry", "~/.cargo/git"}},
	{"Gemfile.lock", []string{"vendor/bundle"}, []string{"~/.gem"}},
	{"composer.lock", []string{"vendor"}, []string{"~/.composer/cache"}},
	{"mix.lock", []string{"deps", "_build"}, []string{"~/.hex"}},
	{"pubspec.lock", nil, []string{"~/.pub-cache"}},
	{"Gemfile", []string{"vendor/bundle"}, []string{"~/.gem"}},
}

// CachePathsFor derives the cache plan from every service's beforeStart
// cacheKey files. A service opts in by declaring a cacheKey; without one corgi
// cannot skip the install anyway, so caching its output would be misleading.
func CachePathsFor(corgi *CorgiCompose) CachePlan {
	paths := map[string]bool{}
	hash := sha256.New()

	// corgi's own step markers. Restoring these without the dependency
	// directories would make corgi skip an install whose output is missing, so
	// they always travel together.
	paths[filepath.Join("corgi_services", cacheDirName)] = true

	for _, service := range sortedServices(corgi) {
		for _, step := range service.BeforeStart {
			for _, key := range step.CacheKey {
				addEcosystemPaths(paths, service, key)
				fmt.Fprintf(hash, "%s\x00%s\x00%s\n",
					service.ServiceName, key, hashFileContents(service.AbsolutePath, key))
			}
		}
	}

	return CachePlan{Paths: sortedPathSet(paths), Key: "corgi-deps-" + hex.EncodeToString(hash.Sum(nil))[:16]}
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
