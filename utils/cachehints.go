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
// the ecosystem group the lockfile belongs to. A step matches when its command
// mentions one of its ecosystem's verbs, so `make setup` is not guessed at.
var installVerbs = map[string][]string{
	"node":   {"npm ci", "npm install", "yarn install", "yarn --", "pnpm install", "pnpm i ", "bun install"},
	"python": {"pip install", "poetry install", "uv sync", "uv pip install", "pipenv install"},
	"go":     {"go mod download", "go mod tidy"},
	"rust":   {"cargo build", "cargo fetch"},
	// "bundle exec ..." is deliberately absent: it runs tasks such as
	// db:migrate, and keying one on Gemfile.lock would skip a migration
	// whenever the gems happened not to change.
	"ruby":   {"bundle install"},
	"php":    {"composer install"},
	"elixir": {"mix deps.get"},
	"dart":   {"pub get"},
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
	for _, eco := range ecosystems {
		verbs, known := installVerbs[eco.group]
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
