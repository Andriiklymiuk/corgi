package utils

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CacheHint is an install step that could opt into caching but has not.
type CacheHint struct {
	Service string `json:"service"`
	// Command is the beforeStart step's run line, so the user can find it.
	Command string `json:"command"`
	// Lockfile is the cacheKey to add, relative to the service.
	Lockfile string `json:"lockfile"`
}

// Keyed by lockfile, not ecosystem: a repo carrying a stale bun.lock would
// otherwise have `yarn install` keyed on it. "bundle exec" is absent on
// purpose — keying db:migrate on Gemfile.lock would skip the migration.
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

// CacheOptInHints finds install steps that could skip on an unchanged lockfile
// but declare no cacheKey. Without it that cost is invisible.
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

// lockfileForStep returns the lockfile this command installs from, or "".
func lockfileForStep(service Service, run string) string {
	lower := strings.ToLower(run)
	// Most specific first, so pnpm wins over a stray package-lock.json.
	for _, eco := range ecosystems {
		verbs, known := installVerbs[eco.lockfile]
		if !known {
			continue
		}
		if !mentionsAny(lower, verbs) {
			continue
		}
		// A cacheKey on a missing file makes every run miss instead of skip.
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

// CacheHintLines renders the hints as pasteable yml, sorted for stability.
func CacheHintLines(hints []CacheHint) []string {
	lines := make([]string, 0, len(hints))
	for _, h := range hints {
		lines = append(lines, h.Service+": - run: "+h.Command+"  →  cacheKey: ["+h.Lockfile+"]")
	}
	sort.Strings(lines)
	return lines
}
