package utils

import (
	"testing"

	"gopkg.in/yaml.v3"
)

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
