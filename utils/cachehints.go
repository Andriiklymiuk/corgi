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

// installVerbs are the commands that produce a dependency directory, keyed by
// the lockfile they install from. Keyed by lockfile rather than by ecosystem
// because a repo often carries a stale one from a previous package manager: a
// node-wide verb list would let `yarn install` be keyed on a leftover bun.lock,
// so the cache would never bust when yarn.lock changed.
//
// "bundle exec ..." is deliberately absent: it runs tasks such as db:migrate,
// and keying one on Gemfile.lock would skip a migration whenever the gems
// happened not to change.
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

// CacheOptInHints finds install steps that corgi could skip on an unchanged
// lockfile but cannot, because the step declares no cacheKey.
//
// Without this the cost is invisible: `corgi cache paths` returns only the step
// markers and every CI run reinstalls everything, with nothing saying why.
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

// lockfileForStep returns the lockfile this command installs from, or "" when
// the command is not an install corgi knows how to key.
func lockfileForStep(service Service, run string) string {
	lower := strings.ToLower(run)
	// ecosystems is ordered most specific first, so a repo holding both a
	// pnpm and an npm lockfile is read as pnpm.
	for _, eco := range ecosystems {
		verbs, known := installVerbs[eco.lockfile]
		if !known {
			continue
		}
		if !mentionsAny(lower, verbs) {
			continue
		}
		// The lockfile has to be there: suggesting a cacheKey for a file that
		// does not exist would make every run miss instead of skip.
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

// CacheHintLines renders the hints as the yml the user would paste, one block
// per step, sorted so the output is stable.
func CacheHintLines(hints []CacheHint) []string {
	lines := make([]string, 0, len(hints))
	for _, h := range hints {
		lines = append(lines, h.Service+": - run: "+h.Command+"  →  cacheKey: ["+h.Lockfile+"]")
	}
	sort.Strings(lines)
	return lines
}
