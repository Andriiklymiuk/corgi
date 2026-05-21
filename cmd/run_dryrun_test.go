package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func sampleDryRunCompose(dir string) *utils.CorgiCompose {
	return &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "app-db", Driver: "postgres", Host: "localhost", Port: 5432, User: "u", Password: "p", DatabaseName: "app"},
		},
		Services: []utils.Service{
			{
				ServiceName:  "api",
				AbsolutePath: filepath.Join(dir, "api"),
				Port:         8080,
				Start:        []string{"echo api"},
				DependsOnDb:  []utils.DependsOnDb{{Name: "app-db", EnvAlias: "APP"}},
				DependsOnServices: []utils.DependsOnService{
					{Name: "auth", EnvAlias: "AUTH_URL"},
				},
				Environment: []string{"FEATURE=on"},
			},
			{
				ServiceName:  "auth",
				AbsolutePath: filepath.Join(dir, "auth"),
				Port:         9000,
				Start:        []string{"echo auth"},
				DependsOnDb:  []utils.DependsOnDb{{Name: "app-db", EnvAlias: "AUTH"}},
			},
		},
	}
}

func TestComputeDryRunPlan_Valid(t *testing.T) {
	dir := t.TempDir()
	plan := computeDryRunPlan(sampleDryRunCompose(dir))

	if !plan.Valid {
		t.Fatalf("expected valid plan, got errors: %v", plan.Errors)
	}
	if len(plan.Errors) != 0 {
		t.Errorf("valid plan should omit errors, got %v", plan.Errors)
	}

	// Topological order: db before both services; auth before api.
	idx := map[string]int{}
	for i, id := range plan.Order {
		idx[id] = i
	}
	for _, id := range []string{"db:app-db", "svc:auth", "svc:api"} {
		if _, ok := idx[id]; !ok {
			t.Fatalf("order missing %q: %v", id, plan.Order)
		}
	}
	if !(idx["db:app-db"] < idx["svc:auth"] && idx["db:app-db"] < idx["svc:api"]) {
		t.Errorf("db must precede services: %v", plan.Order)
	}
	if !(idx["svc:auth"] < idx["svc:api"]) {
		t.Errorf("auth must precede api: %v", plan.Order)
	}
}

func TestComputeDryRunPlan_ServiceDetails(t *testing.T) {
	dir := t.TempDir()
	plan := computeDryRunPlan(sampleDryRunCompose(dir))

	var api dryRunService
	for _, s := range plan.Services {
		if s.Name == "api" {
			api = s
		}
	}
	if api.Name == "" {
		t.Fatal("api service missing from plan")
	}
	if api.Port != 8080 {
		t.Errorf("port: want 8080, got %d", api.Port)
	}
	if api.WillClone {
		t.Error("api has no cloneFrom; willClone must be false")
	}
	wantDeps := []string{"db:app-db", "svc:auth"}
	if len(api.DependsOn) != len(wantDeps) {
		t.Fatalf("deps: want %v, got %v", wantDeps, api.DependsOn)
	}
	for i := range wantDeps {
		if api.DependsOn[i] != wantDeps[i] {
			t.Errorf("deps: want %v, got %v", wantDeps, api.DependsOn)
		}
	}
	// Env keys derived from db (APP_DB_*) and service env entries.
	if !containsAll(api.EnvKeys, []string{"APP_DB_HOST", "APP_DB_PORT", "AUTH_URL", "PORT", "FEATURE"}) {
		t.Errorf("env keys missing expected entries: %v", api.EnvKeys)
	}
}

func TestComputeDryRunPlan_DatabaseEntries(t *testing.T) {
	dir := t.TempDir()
	plan := computeDryRunPlan(sampleDryRunCompose(dir))
	if len(plan.Databases) != 1 {
		t.Fatalf("want 1 db, got %d", len(plan.Databases))
	}
	db := plan.Databases[0]
	if db.Name != "app-db" || db.Driver != "postgres" || db.Port != 5432 || !db.WillStart {
		t.Errorf("unexpected db entry: %+v", db)
	}
}

func TestComputeDryRunPlan_Invalid(t *testing.T) {
	// Dangling dependency -> validation error -> valid=false, errors present.
	c := &utils.CorgiCompose{
		Services: []utils.Service{
			{
				ServiceName:       "api",
				Port:              8080,
				Start:             []string{"echo"},
				DependsOnServices: []utils.DependsOnService{{Name: "ghost"}},
			},
		},
	}
	plan := computeDryRunPlan(c)
	if plan.Valid {
		t.Error("expected invalid plan for dangling dep")
	}
	if len(plan.Errors) == 0 {
		t.Error("expected errors for dangling dep")
	}
	if plan.Order == nil || plan.Databases == nil || plan.Services == nil || plan.Warnings == nil {
		t.Error("arrays must never be nil")
	}
}

func TestComputeStartOrder_Cycle(t *testing.T) {
	// a -> b -> a cycle; order is best-effort but never empty and includes all.
	c := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "a", DependsOnServices: []utils.DependsOnService{{Name: "b"}}},
			{ServiceName: "b", DependsOnServices: []utils.DependsOnService{{Name: "a"}}},
		},
	}
	order := computeStartOrder(c)
	if len(order) != 2 {
		t.Fatalf("want 2 nodes in best-effort order, got %v", order)
	}
	seen := map[string]bool{}
	for _, id := range order {
		seen[id] = true
	}
	if !seen["svc:a"] || !seen["svc:b"] {
		t.Errorf("cycle order must include all nodes: %v", order)
	}
}

func TestWillClone(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "present")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}

	if willClone(utils.Service{}) {
		t.Error("no cloneFrom -> false")
	}
	if willClone(utils.Service{CloneFrom: "x", AbsolutePath: existing}) {
		t.Error("path exists -> false")
	}
	if !willClone(utils.Service{CloneFrom: "x", AbsolutePath: filepath.Join(dir, "absent")}) {
		t.Error("cloneFrom set and path absent -> true")
	}
}

// emitDryRunPlan must not create corgi_services/ or write any .env. This guards
// the no-side-effect contract from the print path.
func TestEmitDryRunPlan_NoSideEffects(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	plan := computeDryRunPlan(sampleDryRunCompose(dir))
	if code := emitDryRunPlan(plan); code != 0 {
		t.Errorf("valid plan should exit 0, got %d", code)
	}

	if _, err := os.Stat(filepath.Join(dir, "corgi_services")); !os.IsNotExist(err) {
		t.Errorf("corgi_services/ must not be created in dry-run")
	}
	for _, svc := range []string{"api", "auth"} {
		if _, err := os.Stat(filepath.Join(dir, svc, ".env")); !os.IsNotExist(err) {
			t.Errorf(".env must not be written for %s in dry-run", svc)
		}
	}
}

func containsAll(haystack, needles []string) bool {
	set := map[string]bool{}
	for _, h := range haystack {
		set[h] = true
	}
	for _, n := range needles {
		if !set[n] {
			return false
		}
	}
	return true
}
