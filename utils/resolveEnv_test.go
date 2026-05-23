package utils

import (
	"os"
	"path/filepath"
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
