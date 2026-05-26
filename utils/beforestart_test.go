package utils

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBeforeStartSteps_ParseMixed(t *testing.T) {
	data := `
beforeStart:
  - yarn install
  - run: bundle install
    cacheKey: [Gemfile.lock, .ruby-version]
`
	var s struct {
		BeforeStart BeforeStartSteps `yaml:"beforeStart"`
	}
	if err := yaml.Unmarshal([]byte(data), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.BeforeStart) != 2 {
		t.Fatalf("want 2 steps, got %d", len(s.BeforeStart))
	}
	if s.BeforeStart[0].Run != "yarn install" || s.BeforeStart[0].CacheKey != nil {
		t.Fatalf("string form wrong: %+v", s.BeforeStart[0])
	}
	if s.BeforeStart[1].Run != "bundle install" {
		t.Fatalf("object run wrong: %+v", s.BeforeStart[1])
	}
	if !reflect.DeepEqual(s.BeforeStart[1].CacheKey, []string{"Gemfile.lock", ".ruby-version"}) {
		t.Fatalf("object cacheKey wrong: %+v", s.BeforeStart[1].CacheKey)
	}
	if !reflect.DeepEqual(s.BeforeStart.Commands(), []string{"yarn install", "bundle install"}) {
		t.Fatalf("Commands() wrong: %v", s.BeforeStart.Commands())
	}
}

// Old all-string beforeStart must parse + behave exactly as before.
func TestBeforeStartSteps_LegacyAllStrings(t *testing.T) {
	data := `
beforeStart:
  - npm install
  - npm run build
`
	var y Service
	if err := yaml.Unmarshal([]byte(data), &y); err != nil {
		t.Fatal(err)
	}
	if y.BeforeStart.HasCacheKeys() {
		t.Fatal("legacy strings must not opt into caching")
	}
	if !reflect.DeepEqual(y.BeforeStart.Commands(), []string{"npm install", "npm run build"}) {
		t.Fatalf("Commands() must match the original strings, got %v", y.BeforeStart.Commands())
	}
}
