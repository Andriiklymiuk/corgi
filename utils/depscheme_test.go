package utils

import (
	"strings"
	"testing"
)

func TestAppendDependentServiceEnv_Scheme(t *testing.T) {
	corgi := CorgiCompose{Services: []Service{{ServiceName: "core", Port: 3000}}}

	https := appendDependentServiceEnv("", DependsOnService{Name: "core", EnvAlias: "API", Scheme: "https"}, corgi)
	if !strings.Contains(https, "API=https://localhost:3000") {
		t.Fatalf("want https URL, got %q", https)
	}

	def := appendDependentServiceEnv("", DependsOnService{Name: "core", EnvAlias: "API"}, corgi)
	if !strings.Contains(def, "API=http://localhost:3000") {
		t.Fatalf("default should stay http, got %q", def)
	}
}
