package cmd

import (
	"andriiklymiuk/corgi/utils"
	"testing"
)

func TestCollectDeclaredPorts_IncludesDbAndServicesSorted(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "api-db", Driver: "postgres", Port: 5432},
			{ServiceName: "shared-aws", Driver: "localstack", Port: 4566},
		},
		Services: []utils.Service{
			{ServiceName: "api-secondary", Port: 3010},
			{ServiceName: "api", Port: 3030},
		},
	}
	ports := collectDeclaredPorts(corgi)
	want := []int{3010, 3030, 4566, 5432}
	if len(ports) != len(want) {
		t.Fatalf("expected %d ports, got %d: %+v", len(want), len(ports), ports)
	}
	for i, p := range ports {
		if p.Port != want[i] {
			t.Errorf("index %d: want %d, got %d (full: %+v)", i, want[i], p.Port, ports)
		}
	}
}

func TestCollectDeclaredPorts_SkipsZeroPortAndManualRun(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "with-port", Driver: "postgres", Port: 5432},
			{ServiceName: "no-port", Driver: "postgres", Port: 0},
		},
		Services: []utils.Service{
			{ServiceName: "normal", Port: 3030},
			{ServiceName: "manual", Port: 9999, ManualRun: true},
			{ServiceName: "zero", Port: 0},
		},
	}
	ports := collectDeclaredPorts(corgi)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %d: %+v", len(ports), ports)
	}
	for _, p := range ports {
		if p.Port == 0 || p.Port == 9999 {
			t.Errorf("unexpected port %d slipped through: %+v", p.Port, p)
		}
	}
}

func TestCollectDeclaredPorts_Empty(t *testing.T) {
	corgi := &utils.CorgiCompose{}
	ports := collectDeclaredPorts(corgi)
	if len(ports) != 0 {
		t.Fatalf("expected no ports for empty compose, got %+v", ports)
	}
}

func TestCollectDeclaredPorts_DescIncludesDriver(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "shared-aws", Driver: "localstack", Port: 4566},
		},
	}
	ports := collectDeclaredPorts(corgi)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Desc != "db_services.shared-aws (localstack)" {
		t.Errorf("unexpected desc: %q", ports[0].Desc)
	}
}
