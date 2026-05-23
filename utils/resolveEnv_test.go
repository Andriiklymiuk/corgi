package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveServiceEnv_DbSource(t *testing.T) {
	corgi := &CorgiCompose{
		DatabaseServices: []DatabaseService{
			{ServiceName: "pg", Driver: "postgres", Port: 5432, User: "u", Password: "p", DatabaseName: "d"},
		},
		Services: []Service{
			{ServiceName: "api", Port: 3000, DependsOnDb: []DependsOnDb{{Name: "pg", EnvAlias: "PG"}}},
		},
	}
	got, err := ResolveServiceEnv(corgi.Services[0], corgi)
	if err != nil {
		t.Fatalf("ResolveServiceEnv: %v", err)
	}
	var sawDb bool
	for _, e := range got {
		if e.Source == "db:pg" {
			sawDb = true
		}
	}
	if !sawDb {
		t.Fatalf("no var attributed to db:pg; got %+v", got)
	}
}

func TestResolveServiceEnv_ServiceSource(t *testing.T) {
	corgi := &CorgiCompose{
		Services: []Service{
			{ServiceName: "api", Port: 8080},
			{ServiceName: "web", Port: 3000, DependsOnServices: []DependsOnService{{Name: "api", EnvAlias: "API"}}},
		},
	}
	got, err := ResolveServiceEnv(corgi.Services[1], corgi)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Source == "service:api" {
			return
		}
	}
	t.Fatalf("no var attributed to service:api; got %+v", got)
}

func TestResolveServiceEnv_PortAndLiteral(t *testing.T) {
	corgi := &CorgiCompose{
		Services: []Service{
			{ServiceName: "api", Port: 8080, PortAlias: "API_PORT",
				Environment: []string{"LOG_LEVEL=debug"}},
		},
	}
	got, err := ResolveServiceEnv(corgi.Services[0], corgi)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]EnvVar{}
	for _, e := range got {
		m[e.Key] = e
	}
	if m["API_PORT"].Source != "self:port" || m["API_PORT"].Value != "8080" {
		t.Fatalf("port entry wrong: %+v", m["API_PORT"])
	}
	if m["LOG_LEVEL"].Source != "literal" || m["LOG_LEVEL"].Value != "debug" {
		t.Fatalf("literal entry wrong: %+v", m["LOG_LEVEL"])
	}
}

func TestResolveServiceEnv_FileSource(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "seed.env"), []byte("FROM_FILE=yes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old := CorgiComposePathDir
	CorgiComposePathDir = dir
	defer func() { CorgiComposePathDir = old }()

	corgi := &CorgiCompose{
		Services: []Service{{ServiceName: "api", Port: 3000, CopyEnvFromFilePath: "seed.env"}},
	}
	got, err := ResolveServiceEnv(corgi.Services[0], corgi)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Key == "FROM_FILE" && e.Source == "file:seed.env" {
			return
		}
	}
	t.Fatalf("no var attributed to file:seed.env; got %+v", got)
}

func TestResolveAllEnv_AllServices(t *testing.T) {
	corgi := &CorgiCompose{
		Services: []Service{
			{ServiceName: "api", Port: 8080},
			{ServiceName: "web", Port: 3000},
		},
	}
	all, err := ResolveAllEnv(corgi)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := all["api"]; !ok {
		t.Fatal("missing api")
	}
	if _, ok := all["web"]; !ok {
		t.Fatal("missing web")
	}
}

func TestResolveServiceEnv_LocalhostNameInEnvRewritesValues(t *testing.T) {
	corgi := &CorgiCompose{
		DatabaseServices: []DatabaseService{
			{ServiceName: "pg", Driver: "postgres", Host: "localhost", Port: 5432, User: "u", Password: "p", DatabaseName: "d"},
		},
		Services: []Service{
			{ServiceName: "api", Port: 3000, LocalhostNameInEnv: "myhost",
				DependsOnDb: []DependsOnDb{{Name: "pg", EnvAlias: "PG"}}},
		},
	}
	got, err := ResolveServiceEnv(corgi.Services[0], corgi)
	if err != nil {
		t.Fatal(err)
	}
	var sawMyhost bool
	for _, e := range got {
		if strings.Contains(e.Value, "localhost") {
			t.Errorf("value still contains localhost after LocalhostNameInEnv rewrite: %+v", e)
		}
		if strings.Contains(e.Value, "myhost") {
			sawMyhost = true
		}
	}
	if !sawMyhost {
		t.Fatalf("no value rewritten to myhost; got %+v", got)
	}
}

func TestResolveServiceEnv_HostOverrideRewritesValues(t *testing.T) {
	defer func() { HostOverride = "" }()
	HostOverride = "10.0.0.5"

	corgi := &CorgiCompose{
		DatabaseServices: []DatabaseService{
			{ServiceName: "pg", Driver: "postgres", Host: "localhost", Port: 5432, User: "u", Password: "p", DatabaseName: "d"},
		},
		Services: []Service{
			// LocalhostNameInEnv empty -> HostOverride applies.
			{ServiceName: "api", Port: 3000, DependsOnDb: []DependsOnDb{{Name: "pg", EnvAlias: "PG"}}},
		},
	}
	got, err := ResolveServiceEnv(corgi.Services[0], corgi)
	if err != nil {
		t.Fatal(err)
	}
	var sawOverride bool
	for _, e := range got {
		if strings.Contains(e.Value, "localhost") {
			t.Errorf("value still contains localhost after HostOverride rewrite: %+v", e)
		}
		if strings.Contains(e.Value, "10.0.0.5") {
			sawOverride = true
		}
	}
	if !sawOverride {
		t.Fatalf("no value rewritten to HostOverride; got %+v", got)
	}
}

// Value-level anti-drift guard: richer fixture exercising a service dependency,
// a cross-service ${producer.VAR} reference (resolved via ResolveAllEnv so the
// exports map is primed), and a file source — asserting VALUES, not just keys.
func TestResolveAllEnv_RichValueParity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "seed.env"), []byte("FROM_FILE=seeded\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old := CorgiComposePathDir
	CorgiComposePathDir = dir
	defer func() { CorgiComposePathDir = old }()

	corgi := &CorgiCompose{
		Services: []Service{
			{ServiceName: "producer", Port: 9000,
				Environment: []string{"TOKEN=secret123"},
				Exports:     []string{"TOKEN"}},
			{ServiceName: "consumer", Port: 3000,
				CopyEnvFromFilePath: "seed.env",
				DependsOnServices:   []DependsOnService{{Name: "producer", EnvAlias: "PROD"}},
				Environment:         []string{"UPSTREAM_TOKEN=${producer.TOKEN}"}},
		},
	}
	all, err := ResolveAllEnv(corgi)
	if err != nil {
		t.Fatal(err)
	}

	cons := map[string]EnvVar{}
	for _, e := range all["consumer"] {
		cons[e.Key] = e
	}
	if cons["FROM_FILE"].Value != "seeded" || cons["FROM_FILE"].Source != "file:seed.env" {
		t.Errorf("file source wrong: %+v", cons["FROM_FILE"])
	}
	if cons["UPSTREAM_TOKEN"].Value != "secret123" || cons["UPSTREAM_TOKEN"].Source != "literal" {
		t.Errorf("cross-service ref value wrong: %+v", cons["UPSTREAM_TOKEN"])
	}
	var sawService bool
	for _, e := range all["consumer"] {
		if e.Source == "service:producer" {
			sawService = true
		}
	}
	if !sawService {
		t.Errorf("no var attributed to service:producer; got %+v", all["consumer"])
	}
}

func TestResolveServiceEnv_ParityWithComputeKeys(t *testing.T) {
	corgi := &CorgiCompose{
		DatabaseServices: []DatabaseService{
			{ServiceName: "pg", Driver: "postgres", Port: 5432, User: "u", Password: "p", DatabaseName: "d"},
		},
		Services: []Service{
			{ServiceName: "api", Port: 8080, PortAlias: "API_PORT",
				DependsOnDb: []DependsOnDb{{Name: "pg", EnvAlias: "PG"}},
				Environment: []string{"LOG_LEVEL=debug"}},
		},
	}
	resolved, err := ResolveServiceEnv(corgi.Services[0], corgi)
	if err != nil {
		t.Fatal(err)
	}
	gotKeys := map[string]bool{}
	for _, e := range resolved {
		gotKeys[e.Key] = true
	}
	for _, k := range ComputeEnvKeysForService(corgi.Services[0], corgi) {
		if !gotKeys[k] {
			t.Errorf("ComputeEnvKeysForService has %q but resolver does not", k)
		}
	}
}
