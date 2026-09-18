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

type CachePlan struct {
	Paths        []string     `json:"paths"`
	Key          string       `json:"key"`
	Groups       []CacheGroup `json:"groups"`
	Hints        []CacheHint  `json:"hints,omitempty"`
	MissingFiles []string     `json:"missingFiles"`
	Complete     bool         `json:"complete"`
}

type CacheGroup struct {
	ID            string   `json:"id"`
	Paths         []string `json:"paths"`
	Key           string   `json:"key"`
	PathsText     string   `json:"pathsText"`
	RestorePrefix string   `json:"restorePrefix,omitempty"`
	MissingFiles  []string `json:"missingFiles"`
}

type ecosystem struct {
	lockfile    string
	group       string
	serviceDirs []string
	homeDirs    []string
}

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

func CachePathsFor(corgi *CorgiCompose) CachePlan {
	acc := collectCacheKeys(corgi)
	aggregate := "corgi-deps-" + hex.EncodeToString(acc.aggregateHash.Sum(nil))[:16]
	groups := acc.cacheGroups()

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

func serviceRelativeDir(service Service) string {
	if service.Path != "" {
		return filepath.Clean(strings.TrimPrefix(service.Path, "./"))
	}
	return service.ServiceName
}

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
