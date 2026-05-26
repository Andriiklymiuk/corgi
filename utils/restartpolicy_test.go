package utils

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRestartPolicy_Parse(t *testing.T) {
	data := `
restartPolicy:
  mode: on-failure
  maxRetries: 3
  backoffSeconds: 5
`
	var y Service
	if err := yaml.Unmarshal([]byte(data), &y); err != nil {
		t.Fatal(err)
	}
	svc := buildService("x", y)
	if svc.RestartPolicy == nil {
		t.Fatal("RestartPolicy not parsed")
	}
	if svc.RestartPolicy.Mode != "on-failure" || svc.RestartPolicy.MaxRetries != 3 || svc.RestartPolicy.BackoffSeconds != 5 {
		t.Fatalf("got %+v", svc.RestartPolicy)
	}
}

func TestValidateRestartPolicy(t *testing.T) {
	if err := ValidateRestartPolicy(nil); err != nil {
		t.Fatalf("nil should be valid: %v", err)
	}
	if err := ValidateRestartPolicy(&RestartPolicy{Mode: "on-failure", MaxRetries: 3}); err != nil {
		t.Fatalf("valid policy errored: %v", err)
	}
	if err := ValidateRestartPolicy(&RestartPolicy{Mode: "bogus"}); err == nil {
		t.Fatal("bad mode should error")
	}
	if err := ValidateRestartPolicy(&RestartPolicy{Mode: "on-failure", MaxRetries: -1}); err == nil {
		t.Fatal("negative maxRetries should error")
	}
}
