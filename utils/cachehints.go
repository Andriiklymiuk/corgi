package utils

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CacheHint struct {
	Service  string `json:"service"`
	Command  string `json:"command"`
	Lockfile string `json:"lockfile"`
}

var installVerbs = map[string][]string{
	"package-lock.json": {"npm ci", "npm install"},
	"yarn.lock":         {"yarn install", "yarn --"},
	"pnpm-lock.yaml":    {"pnpm install", "pnpm i "},
	"bun.lock":          {"bun install"},
	"bun.lockb":         {"bun install"},
	"uv.lock":           {"uv sync", "uv pip install"},
	"poetry.lock":       {"poetry install"},
	"Pipfile.lock":      {"pipenv install"},
	"requirements.txt":  {"pip install"},
	"go.sum":            {"go mod download", "go mod tidy"},
	"Cargo.lock":        {"cargo build", "cargo fetch"},
	"Gemfile.lock":      {"bundle install"},
	"Gemfile":           {"bundle install"},
	"composer.lock":     {"composer install"},
	"mix.lock":          {"mix deps.get"},
	"pubspec.lock":      {"pub get"},
}

func CacheOptInHints(corgi *CorgiCompose) []CacheHint {
	var hints []CacheHint
	for _, service := range sortedServices(corgi) {
		for _, step := range service.BeforeStart {
			if len(step.CacheKey) > 0 || strings.TrimSpace(step.Run) == "" {
				continue
			}
			if lockfile := lockfileForStep(service, step.Run); lockfile != "" {
				hints = append(hints, CacheHint{
					Service:  service.ServiceName,
					Command:  strings.TrimSpace(step.Run),
					Lockfile: lockfile,
				})
			}
		}
	}
	return hints
}

func lockfileForStep(service Service, run string) string {
	lower := strings.ToLower(run)
	for _, eco := range ecosystems {
		verbs, known := installVerbs[eco.lockfile]
		if !known {
			continue
		}
		if !mentionsAny(lower, verbs) {
			continue
		}
		if _, err := os.Stat(filepath.Join(service.AbsolutePath, eco.lockfile)); err == nil {
			return eco.lockfile
		}
	}
	return ""
}

func mentionsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func CacheHintLines(hints []CacheHint) []string {
	lines := make([]string, 0, len(hints))
	for _, h := range hints {
		lines = append(lines, h.Service+": - run: "+h.Command+"  →  cacheKey: ["+h.Lockfile+"]")
	}
	sort.Strings(lines)
	return lines
}
