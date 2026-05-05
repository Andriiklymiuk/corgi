package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvVarsIntoMap(t *testing.T) {
	in := "FOO=bar\nBAZ=qux=more\n# comment\nNOEQ\n"
	got := parseEnvVarsIntoMap(in)
	if got["FOO"] != "bar" {
		t.Errorf("FOO = %q", got["FOO"])
	}
	if got["BAZ"] != "qux=more" {
		t.Errorf("BAZ = %q want qux=more (only first = splits)", got["BAZ"])
	}
	if _, ok := got["NOEQ"]; ok {
		t.Errorf("NOEQ should not be set")
	}
}

func TestSubstituteEnvVarReferences(t *testing.T) {
	envMap := map[string]string{"HOST": "localhost", "PORT": "8080"}
	tests := []struct {
		in, want string
	}{
		{"http://${HOST}:${PORT}", "http://localhost:8080"},
		{"plain", "plain"},
		{"${MISSING}", "${MISSING}"},
	}
	for _, tt := range tests {
		if got := substituteEnvVarReferences(tt.in, envMap); got != tt.want {
			t.Errorf("substitute(%q) = %q want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitStringForEnv(t *testing.T) {
	tests := map[string]string{
		"my-service":      "MY_SERVICE",
		"foo/bar":         "FOO_BAR",
		"camelCaseStr":    "CAMEL_CASE_STR",
		"already_lowered": "ALREADY_LOWERED",
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			if got := splitStringForEnv(in); got != want {
				t.Errorf("splitStringForEnv(%q) = %q want %q", in, got, want)
			}
		})
	}
}

func TestGetPathToEnv(t *testing.T) {
	tests := []struct {
		name string
		svc  Service
		want string
	}{
		{"default", Service{AbsolutePath: "/srv/api"}, "/srv/api/.env"},
		{"trailing slash absorbed", Service{AbsolutePath: "/srv/api/"}, "/srv/api/.env"},
		{"custom env path", Service{AbsolutePath: "/srv/api", EnvPath: "/configs/.env.local"}, "/srv/api/configs/.env.local"},
		{"short abs path", Service{AbsolutePath: "/"}, ".env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetPathToEnv(tt.svc); got != tt.want {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestRemoveFromToIndexes(t *testing.T) {
	got := removeFromToIndexes([]string{"a", "b", "c", "d"}, 1, 2)
	if len(got) != 2 || got[0] != "a" || got[1] != "d" {
		t.Errorf("got %v", got)
	}
}

func TestCreateEnvString(t *testing.T) {
	got := createEnvString("PREFIX_\n", "API_URL", "localhost", "3030", "/v1")
	want := "PREFIX_\nAPI_URL=http://localhost:3030/v1\n"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFindServiceByName(t *testing.T) {
	services := []Service{{ServiceName: "a"}, {ServiceName: "b"}}
	if got := findServiceByName(services, "b"); got == nil || got.ServiceName != "b" {
		t.Errorf("got %+v", got)
	}
	if got := findServiceByName(services, "missing"); got != nil {
		t.Errorf("expected nil")
	}
}

func TestEnsurePathExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "nested")
	if err := EnsurePathExists(dir); err != nil {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
	if err := EnsurePathExists(dir); err != nil {
		t.Errorf("idempotent err: %v", err)
	}
}

func TestHandleDependentServicesUsesPort(t *testing.T) {
	corgi := CorgiCompose{
		Services: []Service{
			{ServiceName: "auth", Port: 4000},
		},
	}
	consumer := Service{
		DependsOnServices: []DependsOnService{{Name: "auth"}},
	}
	got := handleDependentServices(consumer, corgi)
	if !strings.Contains(got, "AUTH_URL=http://localhost:4000") {
		t.Errorf("missing dep URL: %q", got)
	}
}

func TestHandleDependentServicesEnvAlias(t *testing.T) {
	corgi := CorgiCompose{
		Services: []Service{
			{ServiceName: "auth", Port: 4000},
		},
	}
	consumer := Service{
		DependsOnServices: []DependsOnService{{Name: "auth", EnvAlias: "AUTH"}},
	}
	got := handleDependentServices(consumer, corgi)
	if !strings.Contains(got, "AUTH=http://localhost:4000") {
		t.Errorf("got %q", got)
	}
}

func TestHandleDependentServicesSkipsManualRunUnlessForced(t *testing.T) {
	corgi := CorgiCompose{
		Services: []Service{{ServiceName: "auth", Port: 4000, ManualRun: true}},
	}
	consumer := Service{
		DependsOnServices: []DependsOnService{{Name: "auth"}},
	}
	got := handleDependentServices(consumer, corgi)
	if got != "" {
		t.Errorf("manual run should produce no env: %q", got)
	}

	consumer.DependsOnServices[0].ForceUseEnv = true
	got = handleDependentServices(consumer, corgi)
	if !strings.Contains(got, "AUTH_URL") {
		t.Errorf("forceUseEnv should emit: %q", got)
	}
}

func TestHandleDependsOnDbBuildsEnv(t *testing.T) {
	corgi := CorgiCompose{
		DatabaseServices: []DatabaseService{
			{ServiceName: "main-db", Driver: "postgres", Host: "localhost", Port: 5432, User: "u", Password: "p", DatabaseName: "d"},
		},
	}
	svc := Service{
		DependsOnDb: []DependsOnDb{{Name: "main-db"}},
	}
	got := handleDependsOnDb(svc, corgi)
	if !strings.Contains(got, "DB_HOST=localhost") || !strings.Contains(got, "DB_PORT=5432") {
		t.Errorf("got %q", got)
	}
}

func TestHandleDependsOnDbAliasOverride(t *testing.T) {
	corgi := CorgiCompose{
		DatabaseServices: []DatabaseService{
			{ServiceName: "main-db", Driver: "postgres", Host: "h", Port: 1, User: "u", Password: "p", DatabaseName: "d"},
		},
	}
	svc := Service{
		DependsOnDb: []DependsOnDb{
			{Name: "main-db", EnvAlias: "MAIN"},
			{Name: "main-db", EnvAlias: "OTHER"},
		},
	}
	got := handleDependsOnDb(svc, corgi)
	if !strings.Contains(got, "MAIN_DB_HOST=h") {
		t.Errorf("alias should prefix HOST: %q", got)
	}
	if !strings.Contains(got, "OTHER_DB_HOST=h") {
		t.Errorf("second alias missing: %q", got)
	}
}

func TestTopoSortServicesNoDeps(t *testing.T) {
	services := []Service{{ServiceName: "a"}, {ServiceName: "b"}}
	got, err := topoSortServices(services)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d", len(got))
	}
}

func TestTopoSortServicesProducerFirst(t *testing.T) {
	services := []Service{
		{
			ServiceName:       "consumer",
			DependsOnServices: []DependsOnService{{Name: "producer"}},
			Environment:       []string{"X=${producer.URL}"},
		},
		{ServiceName: "producer"},
	}
	got, err := topoSortServices(services)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ServiceName != "producer" || got[1].ServiceName != "consumer" {
		t.Errorf("order wrong: %+v", got)
	}
}

func TestTopoSortServicesCycle(t *testing.T) {
	services := []Service{
		{
			ServiceName:       "a",
			DependsOnServices: []DependsOnService{{Name: "b"}},
			Environment:       []string{"X=${b.X}"},
		},
		{
			ServiceName:       "b",
			DependsOnServices: []DependsOnService{{Name: "a"}},
			Environment:       []string{"X=${a.X}"},
		},
	}
	_, err := topoSortServices(services)
	if err == nil {
		t.Error("want cycle error")
	}
}

func TestResolveExportsLiteral(t *testing.T) {
	svc := Service{
		ServiceName: "s",
		Exports:     []string{"VAR=hello-${OWN}"},
	}
	got, err := resolveExports(svc, map[string]string{"OWN": "world"})
	if err != nil {
		t.Fatal(err)
	}
	if got["VAR"] != "hello-world" {
		t.Errorf("got %q", got["VAR"])
	}
}

func TestResolveExportsBareReexport(t *testing.T) {
	svc := Service{ServiceName: "s", Exports: []string{"X"}}
	got, err := resolveExports(svc, map[string]string{"X": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if got["X"] != "v" {
		t.Errorf("got %v", got)
	}
}

func TestResolveExportsBareMissingErrors(t *testing.T) {
	svc := Service{ServiceName: "s", Exports: []string{"NOPE"}}
	_, err := resolveExports(svc, map[string]string{})
	if err == nil {
		t.Error("want err")
	}
}

func TestSubstituteCrossServiceRefsValid(t *testing.T) {
	consumer := Service{
		DependsOnServices: []DependsOnService{{Name: "producer"}},
	}
	exports := ExportsMap{
		"producer": {"URL": "http://prod:8080"},
	}
	got, err := substituteCrossServiceRefs("API=${producer.URL}", consumer, exports)
	if err != nil {
		t.Fatal(err)
	}
	if got != "API=http://prod:8080" {
		t.Errorf("got %q", got)
	}
}

func TestSubstituteCrossServiceRefsRejectsUndeclared(t *testing.T) {
	consumer := Service{}
	exports := ExportsMap{"producer": {"URL": "x"}}
	_, err := substituteCrossServiceRefs("${producer.URL}", consumer, exports)
	if err == nil {
		t.Error("want err for missing depends_on_services")
	}
}

func TestSubstituteCrossServiceRefsNilExports(t *testing.T) {
	got, err := substituteCrossServiceRefs("plain", Service{}, nil)
	if err != nil {
		t.Errorf("err: %v", err)
	}
	if got != "plain" {
		t.Errorf("got %q", got)
	}
}

func TestGenerateEnvForDbDependentServicePrefix(t *testing.T) {
	db := DatabaseService{
		Driver: "postgres", Host: "h", Port: 5432, User: "u", Password: "p", DatabaseName: "d",
	}
	got := generateEnvForDbDependentService(
		Service{DependsOnDb: []DependsOnDb{{}}},
		DependsOnDb{},
		db,
	)
	if !strings.Contains(got, "DB_HOST=h") {
		t.Errorf("got %q", got)
	}
}

func TestGenerateEnvForDbDependentServiceFallsBackToDefault(t *testing.T) {
	db := DatabaseService{Driver: "no-such-driver", Host: "h", Port: 1, User: "u", Password: "p", DatabaseName: "d"}
	got := generateEnvForDbDependentService(Service{DependsOnDb: []DependsOnDb{{}}}, DependsOnDb{}, db)
	if !strings.Contains(got, "DB_HOST=h") {
		t.Errorf("expected fallback to default driver, got %q", got)
	}
}

func TestBuildLocalEnvIncludesPort(t *testing.T) {
	svc := Service{ServiceName: "api", Port: 3000}
	got := buildLocalEnv(svc, CorgiCompose{Services: []Service{svc}})
	if !strings.Contains(got, "PORT=3000") {
		t.Errorf("got %q", got)
	}
}

func TestBuildLocalEnvCustomPortAlias(t *testing.T) {
	svc := Service{ServiceName: "api", Port: 3000, PortAlias: "API_PORT"}
	got := buildLocalEnv(svc, CorgiCompose{Services: []Service{svc}})
	if !strings.Contains(got, "API_PORT=3000") {
		t.Errorf("got %q", got)
	}
}
