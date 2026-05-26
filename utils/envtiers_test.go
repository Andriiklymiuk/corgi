package utils

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResolveEnvSourceFile_TierConventionDir(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	// tier dir holds env/staging/api.env; no explicit copyEnvFromFilePath
	if err := os.MkdirAll(filepath.Join(dir, "env", "staging"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "env", "staging", "api.env"), "A=1")
	svc := Service{ServiceName: "api", AbsolutePath: dir + "/"}

	got := resolveEnvSourceFile(CorgiComposePathDir, svc, "", "staging", "env/staging")
	want := filepath.Join(dir, "env", "staging", "api.env")
	if got != want {
		t.Fatalf("want tier convention file %q, got %q", want, got)
	}
}

func TestResolveEnvSourceFile_TierTokenSubstitution(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	if err := os.MkdirAll(filepath.Join(dir, "creds"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "creds", "staging.env"), "A=1")
	svc := Service{ServiceName: "broker", AbsolutePath: dir + "/", CopyEnvFromFilePath: "creds/${tier}.env"}

	got := resolveEnvSourceFile(CorgiComposePathDir, svc, "", "staging", "env/staging")
	want := filepath.Join(dir, "creds", "staging.env")
	if got != want {
		t.Fatalf("want ${tier}-substituted path %q, got %q", want, got)
	}
}

func TestResolveEnvSourceFile_TierMissingFallsThrough(t *testing.T) {
	dir := t.TempDir()
	prev := CorgiComposePathDir
	CorgiComposePathDir = dir
	t.Cleanup(func() { CorgiComposePathDir = prev })

	// no tier file; .env-example present → fall through to Feature 5 chain
	writeFile(t, filepath.Join(dir, ".env-example"), "A=1")
	svc := Service{ServiceName: "api", AbsolutePath: dir + "/"}

	got := resolveEnvSourceFile(CorgiComposePathDir, svc, "", "staging", "env/staging")
	want := filepath.Join(dir, ".env-example")
	if got != want {
		t.Fatalf("want fall-through to .env-example %q, got %q", want, got)
	}
}

func TestEnvTiers_ParseAndCarry(t *testing.T) {
	data := []byte(`
envTiers:
  staging:
    dir: env/staging
    dbServices: none
  prod:
    dir: env/prod
    confirm: true
`)
	var y CorgiComposeYaml
	if err := yaml.Unmarshal(data, &y); err != nil {
		t.Fatal(err)
	}

	if got := y.EnvTiers["staging"].Dir; got != "env/staging" {
		t.Fatalf("staging dir: want env/staging, got %q", got)
	}
	if got := y.EnvTiers["staging"].DbServices; got != "none" {
		t.Fatalf("staging dbServices: want none, got %q", got)
	}
	if !y.EnvTiers["prod"].Confirm {
		t.Fatalf("prod confirm: want true")
	}

	corgi := buildBaseCorgi(y)
	if corgi.EnvTiers["prod"].Dir != "env/prod" {
		t.Fatalf("buildBaseCorgi did not carry EnvTiers: %+v", corgi.EnvTiers)
	}
}

func TestEnvTiers_AbsentIsNil(t *testing.T) {
	var y CorgiComposeYaml
	if err := yaml.Unmarshal([]byte("name: x\n"), &y); err != nil {
		t.Fatal(err)
	}
	if y.EnvTiers != nil {
		t.Fatalf("want nil EnvTiers when absent, got %+v", y.EnvTiers)
	}
}
