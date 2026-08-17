package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func serviceWithLockfile(t *testing.T, name, lockfile, run string, cacheKey []string) Service {
	t.Helper()
	dir := t.TempDir()
	if lockfile != "" {
		if err := os.WriteFile(filepath.Join(dir, lockfile), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Service{
		ServiceName:  name,
		Path:         "./" + name,
		AbsolutePath: dir,
		BeforeStart:  BeforeStartSteps{{Run: run, CacheKey: cacheKey}},
	}
}

// An install with no cacheKey is the difference between a five-minute CI run
// and a twenty-minute one, and nothing said so before.
func TestCacheOptInHintsFindsAnUnkeyedInstall(t *testing.T) {
	svc := serviceWithLockfile(t, "api", "package-lock.json", "npm ci", nil)
	hints := CacheOptInHints(&CorgiCompose{Services: []Service{svc}})

	if len(hints) != 1 {
		t.Fatalf("expected one hint, got %+v", hints)
	}
	if hints[0].Service != "api" || hints[0].Lockfile != "package-lock.json" {
		t.Errorf("unexpected hint: %+v", hints[0])
	}
}

// A step that already opts in is not a hint.
func TestCacheOptInHintsIgnoresAKeyedStep(t *testing.T) {
	svc := serviceWithLockfile(t, "api", "package-lock.json", "npm ci", []string{"package-lock.json"})
	if hints := CacheOptInHints(&CorgiCompose{Services: []Service{svc}}); len(hints) != 0 {
		t.Errorf("expected no hints, got %+v", hints)
	}
}

// Suggesting a cacheKey for a file that is not there would make every run miss
// instead of skip, which is worse than saying nothing.
func TestCacheOptInHintsNeedsTheLockfileToExist(t *testing.T) {
	svc := serviceWithLockfile(t, "api", "", "npm ci", nil)
	if hints := CacheOptInHints(&CorgiCompose{Services: []Service{svc}}); len(hints) != 0 {
		t.Errorf("expected no hints without a lockfile, got %+v", hints)
	}
}

// corgi cannot know what `make setup` installs, so it must not guess.
func TestCacheOptInHintsDoesNotGuessAtOpaqueCommands(t *testing.T) {
	svc := serviceWithLockfile(t, "api", "package-lock.json", "make setup", nil)
	if hints := CacheOptInHints(&CorgiCompose{Services: []Service{svc}}); len(hints) != 0 {
		t.Errorf("expected no hints for an opaque command, got %+v", hints)
	}
}

func TestCacheOptInHintsCoversEachEcosystem(t *testing.T) {
	cases := []struct{ lockfile, run string }{
		{"package-lock.json", "npm ci"},
		{"yarn.lock", "yarn install"},
		{"bun.lock", "bun install"},
		{"uv.lock", "uv sync"},
		{"requirements.txt", "pip install -r requirements.txt"},
		{"Gemfile.lock", "bundle install"},
		{"go.sum", "go mod download"},
		{"composer.lock", "composer install"},
		{"mix.lock", "mix deps.get"},
	}
	for _, c := range cases {
		svc := serviceWithLockfile(t, "svc", c.lockfile, c.run, nil)
		hints := CacheOptInHints(&CorgiCompose{Services: []Service{svc}})
		if len(hints) != 1 || hints[0].Lockfile != c.lockfile {
			t.Errorf("%s / %q: expected a hint for that lockfile, got %+v", c.lockfile, c.run, hints)
		}
	}
}

// The hints ride along on the plan, so every output format can show them.
func TestCachePlanCarriesTheHints(t *testing.T) {
	svc := serviceWithLockfile(t, "api", "package-lock.json", "npm ci", nil)
	plan := CachePathsFor(&CorgiCompose{Services: []Service{svc}})

	if len(plan.Groups) != 0 {
		t.Errorf("an unkeyed step must not produce a cache group: %+v", plan.Groups)
	}
	if len(plan.Hints) != 1 {
		t.Errorf("expected the hint on the plan, got %+v", plan.Hints)
	}
}

func TestCacheHintLinesAreStable(t *testing.T) {
	hints := []CacheHint{
		{Service: "web", Command: "npm ci", Lockfile: "package-lock.json"},
		{Service: "api", Command: "npm ci", Lockfile: "package-lock.json"},
	}
	first := CacheHintLines(hints)
	second := CacheHintLines(hints)
	if len(first) != 2 || first[0] != second[0] || first[1] != second[1] {
		t.Errorf("expected a stable rendering, got %v then %v", first, second)
	}
	if first[0] > first[1] {
		t.Errorf("expected sorted output, got %v", first)
	}
}

// `bundle exec rake db:migrate` is a task, not an install. Keying it on
// Gemfile.lock would make corgi skip a migration whenever the gems happened
// not to change — worse than no hint at all.
func TestCacheOptInHintsNeverSuggestsCachingATask(t *testing.T) {
	for _, run := range []string{
		"bundle exec rake db:migrate",
		"bundle exec rails db:seed",
		"npm run build",
		"yarn lint",
	} {
		svc := serviceWithLockfile(t, "billing", "Gemfile.lock", run, nil)
		svc.BeforeStart = BeforeStartSteps{{Run: run}}
		if hints := CacheOptInHints(&CorgiCompose{Services: []Service{svc}}); len(hints) != 0 {
			t.Errorf("%q must not be suggested for caching, got %+v", run, hints)
		}
	}
}

// A repo that moved from bun to yarn often still carries the old lockfile.
// Keying `yarn install` on it would mean the cache never busts when yarn.lock
// changes — a stale node_modules restored on every run.
func TestCacheOptInHintsPicksTheLockfileTheCommandActuallyUses(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"bun.lock", "yarn.lock"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	svc := Service{
		ServiceName:  "web",
		Path:         "./web",
		AbsolutePath: dir,
		BeforeStart:  BeforeStartSteps{{Run: "yarn install"}},
	}

	hints := CacheOptInHints(&CorgiCompose{Services: []Service{svc}})
	if len(hints) != 1 {
		t.Fatalf("expected one hint, got %+v", hints)
	}
	if hints[0].Lockfile != "yarn.lock" {
		t.Errorf("yarn install must be keyed on yarn.lock, got %q", hints[0].Lockfile)
	}
}
