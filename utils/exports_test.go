package utils

import (
	"errors"
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

func TestResolveExportsFixedPoint_BidirectionalLiterals(t *testing.T) {
	// Real codependency that topo-sort can't order: both services have
	// hard ${other.VAR} refs but the export VALUES themselves are static
	// literals so resolution converges in one fixed-point pass.
	c := &CorgiCompose{
		Services: []Service{
			{
				ServiceName:       "a",
				DependsOnServices: []DependsOnService{{Name: "b"}},
				Environment:       []string{"FROM_B=${b.B_VAL}", "A_VAL=hello-from-a"},
				Exports:           []string{"A_VAL"},
			},
			{
				ServiceName:       "b",
				DependsOnServices: []DependsOnService{{Name: "a"}},
				Environment:       []string{"FROM_A=${a.A_VAL}", "B_VAL=hello-from-b"},
				Exports:           []string{"B_VAL"},
			},
		},
	}
	resolved, err := resolveExportsFixedPoint(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["a"]["A_VAL"] != "hello-from-a" {
		t.Fatalf("a.A_VAL = %q", resolved["a"]["A_VAL"])
	}
	if resolved["b"]["B_VAL"] != "hello-from-b" {
		t.Fatalf("b.B_VAL = %q", resolved["b"]["B_VAL"])
	}
}

func TestResolveExportsFixedPoint_TransitiveResolution(t *testing.T) {
	// A exports a literal that references B's export. B's export references
	// A's literal-only export (not the ${b.X} one). Fixed-point should
	// resolve in a couple of iterations.
	c := &CorgiCompose{
		Services: []Service{
			{
				ServiceName:       "a",
				DependsOnServices: []DependsOnService{{Name: "b"}},
				Exports:           []string{"COMBINED=${b.B_HOST}:7000", "A_TAG=tag-a"},
			},
			{
				ServiceName:       "b",
				DependsOnServices: []DependsOnService{{Name: "a"}},
				Exports:           []string{"B_HOST=localhost", "ECHO_A=${a.A_TAG}"},
			},
		},
	}
	resolved, err := resolveExportsFixedPoint(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved["a"]["COMBINED"] != "localhost:7000" {
		t.Fatalf("a.COMBINED = %q", resolved["a"]["COMBINED"])
	}
	if resolved["b"]["ECHO_A"] != "tag-a" {
		t.Fatalf("b.ECHO_A = %q", resolved["b"]["ECHO_A"])
	}
}

func TestResolveExportsFixedPoint_TrueVarLevelCycle(t *testing.T) {
	// A.X = ${b.Y}, B.Y = ${a.X}. Genuine cycle — fixed-point cannot resolve.
	c := &CorgiCompose{
		Services: []Service{
			{
				ServiceName:       "a",
				DependsOnServices: []DependsOnService{{Name: "b"}},
				Exports:           []string{"X=${b.Y}"},
			},
			{
				ServiceName:       "b",
				DependsOnServices: []DependsOnService{{Name: "a"}},
				Exports:           []string{"Y=${a.X}"},
			},
		},
	}
	_, err := resolveExportsFixedPoint(c)
	if err == nil {
		t.Fatal("expected error: var-level cycle should be unresolvable")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle message, got %v", err)
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

func TestSubstituteCrossServiceRefs_ProducerSkipped(t *testing.T) {
	defer func() { SkippedServices = map[string]bool{} }()
	SkippedServices = map[string]bool{"notifier": true}
	consumer := Service{
		ServiceName:       "app",
		DependsOnServices: []DependsOnService{{Name: "notifier"}},
	}
	exports := ExportsMap{}
	_, err := substituteCrossServiceRefs("X=${notifier.TOKEN}", consumer, exports)
	if err == nil {
		t.Fatal("expected producerSkippedError")
	}
	var skipped *producerSkippedError
	if !errors.As(err, &skipped) {
		t.Fatalf("expected producerSkippedError, got %T: %v", err, err)
	}
	if skipped.producer != "notifier" || skipped.varName != "TOKEN" {
		t.Fatalf("unexpected fields: %+v", skipped)
	}
}

func TestAppendEnvironmentLines_SkipsSkippedProducerLine(t *testing.T) {
	defer func() {
		SkippedServices = map[string]bool{}
		currentExportsMap = nil
	}()
	SkippedServices = map[string]bool{"notifier": true}
	currentExportsMap = ExportsMap{}
	service := Service{
		ServiceName:       "app",
		DependsOnServices: []DependsOnService{{Name: "notifier"}},
		Environment: []string{
			"KEEP=ok",
			"DROP=${notifier.TOKEN}",
		},
	}
	out, err := appendEnvironmentLines("", service)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "KEEP=ok") {
		t.Fatalf("expected KEEP=ok in output, got %q", out)
	}
	if strings.Contains(out, "DROP=") || strings.Contains(out, "notifier.TOKEN") {
		t.Fatalf("expected DROP line to be omitted, got %q", out)
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
