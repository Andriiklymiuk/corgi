package utils

import "testing"

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
