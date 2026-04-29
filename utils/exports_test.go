package utils

import (
	"strings"
	"testing"
)

func TestTopoSortServices_NoDeps(t *testing.T) {
	services := []Service{
		{ServiceName: "a"},
		{ServiceName: "b"},
	}
	ordered, err := topoSortServices(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ordered) != 2 {
		t.Fatalf("expected 2 services, got %d", len(ordered))
	}
}

func TestTopoSortServices_LinearDep(t *testing.T) {
	// Only cross-service ${producer.VAR} refs add ordering edges.
	services := []Service{
		{
			ServiceName:       "consumer",
			DependsOnServices: []DependsOnService{{Name: "producer"}},
			Environment:       []string{"X=${producer.TOKEN}"},
		},
		{ServiceName: "producer"},
	}
	ordered, err := topoSortServices(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ordered[0].ServiceName != "producer" || ordered[1].ServiceName != "consumer" {
		t.Fatalf("expected [producer, consumer], got [%s, %s]", ordered[0].ServiceName, ordered[1].ServiceName)
	}
}

func TestTopoSortServices_Cycle(t *testing.T) {
	// Real cycle: both sides reference each other's exports.
	services := []Service{
		{
			ServiceName:       "a",
			DependsOnServices: []DependsOnService{{Name: "b"}},
			Environment:       []string{"X=${b.TOKEN}"},
		},
		{
			ServiceName:       "b",
			DependsOnServices: []DependsOnService{{Name: "a"}},
			Environment:       []string{"Y=${a.TOKEN}"},
		},
	}
	_, err := topoSortServices(services)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle message, got %v", err)
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "b") {
		t.Fatalf("expected cycle error to name services a and b, got %v", err)
	}
}

func TestTopoSortServices_SoftCodependency(t *testing.T) {
	// Two services depend on each other via envAlias only — no cross-ref.
	// Should NOT cycle: emitted values are static localhost:port.
	services := []Service{
		{
			ServiceName:       "api",
			DependsOnServices: []DependsOnService{{Name: "notif", EnvAlias: "NOTIF_URL"}},
		},
		{
			ServiceName:       "notif",
			DependsOnServices: []DependsOnService{{Name: "api", EnvAlias: "API_URL"}},
		},
	}
	ordered, err := topoSortServices(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ordered) != 2 {
		t.Fatalf("expected 2 services, got %d", len(ordered))
	}
}

func TestTopoSortServices_SelfDep(t *testing.T) {
	// Service listing itself as dep (for own BASE_URL alias) must not cycle.
	services := []Service{
		{
			ServiceName:       "a",
			DependsOnServices: []DependsOnService{{Name: "a", EnvAlias: "BASE_URL"}},
		},
	}
	ordered, err := topoSortServices(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ordered) != 1 {
		t.Fatalf("expected 1 service, got %d", len(ordered))
	}
}

func TestTopoSortServices_MixedHardSoft(t *testing.T) {
	// api consumes notif's TOKEN (hard); notif aliases api's URL (soft).
	// Order must be notif → api. No cycle.
	services := []Service{
		{
			ServiceName:       "api",
			DependsOnServices: []DependsOnService{{Name: "notif"}},
			Environment:       []string{"TOKEN=${notif.TOKEN}"},
		},
		{
			ServiceName:       "notif",
			DependsOnServices: []DependsOnService{{Name: "api", EnvAlias: "API_URL"}},
		},
	}
	ordered, err := topoSortServices(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ordered[0].ServiceName != "notif" || ordered[1].ServiceName != "api" {
		t.Fatalf("expected [notif, api], got [%s, %s]", ordered[0].ServiceName, ordered[1].ServiceName)
	}
}

func TestTopoSortServices_UnknownDepIgnored(t *testing.T) {
	services := []Service{
		{ServiceName: "a", DependsOnServices: []DependsOnService{{Name: "ghost"}}},
	}
	ordered, err := topoSortServices(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ordered) != 1 {
		t.Fatalf("expected 1 service, got %d", len(ordered))
	}
}

func TestResolveExports_PlainAndLiteral(t *testing.T) {
	service := Service{
		ServiceName: "notifier",
		Exports:     []string{"TOKEN", "URL=http://localhost:${PORT}/x"},
	}
	producerEnv := map[string]string{
		"TOKEN": "secret-xyz",
		"PORT":  "7000",
	}
	out, err := resolveExports(service, producerEnv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out["TOKEN"] != "secret-xyz" {
		t.Fatalf("TOKEN = %q", out["TOKEN"])
	}
	if out["URL"] != "http://localhost:7000/x" {
		t.Fatalf("URL = %q", out["URL"])
	}
}

func TestResolveExports_MissingVar(t *testing.T) {
	service := Service{
		ServiceName: "x",
		Exports:     []string{"DOES_NOT_EXIST"},
	}
	_, err := resolveExports(service, map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing export")
	}
}

func TestSubstituteCrossServiceRefs_OK(t *testing.T) {
	consumer := Service{
		ServiceName:       "app",
		DependsOnServices: []DependsOnService{{Name: "notifier"}},
	}
	exports := ExportsMap{
		"notifier": {"TOKEN": "abc", "URL": "http://localhost:7000"},
	}
	got, err := substituteCrossServiceRefs("X=${notifier.TOKEN}/${notifier.URL}", consumer, exports)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "X=abc/http://localhost:7000"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSubstituteCrossServiceRefs_NotInDeps(t *testing.T) {
	consumer := Service{ServiceName: "app"}
	exports := ExportsMap{"notifier": {"TOKEN": "abc"}}
	_, err := substituteCrossServiceRefs("X=${notifier.TOKEN}", consumer, exports)
	if err == nil {
		t.Fatal("expected error: not in depends_on_services")
	}
	if !strings.Contains(err.Error(), "depends_on_services") {
		t.Fatalf("expected depends_on_services message, got %v", err)
	}
}

func TestSubstituteCrossServiceRefs_VarNotExported(t *testing.T) {
	consumer := Service{
		ServiceName:       "app",
		DependsOnServices: []DependsOnService{{Name: "notifier"}},
	}
	exports := ExportsMap{"notifier": {"TOKEN": "abc"}}
	_, err := substituteCrossServiceRefs("X=${notifier.MISSING}", consumer, exports)
	if err == nil {
		t.Fatal("expected error: not exported")
	}
	if !strings.Contains(err.Error(), "not exported") {
		t.Fatalf("expected 'not exported' message, got %v", err)
	}
}

func TestSubstituteCrossServiceRefs_NoExportsMap(t *testing.T) {
	consumer := Service{ServiceName: "app"}
	got, err := substituteCrossServiceRefs("X=${notifier.TOKEN}", consumer, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "X=${notifier.TOKEN}" {
		t.Fatalf("expected passthrough, got %q", got)
	}
}

func TestSubstituteCrossServiceRefs_OwnVarUnchanged(t *testing.T) {
	consumer := Service{ServiceName: "app"}
	exports := ExportsMap{}
	got, err := substituteCrossServiceRefs("X=${PORT}", consumer, exports)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "X=${PORT}" {
		t.Fatalf("own ${VAR} must not be touched, got %q", got)
	}
}
