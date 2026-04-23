package cmd

import (
	"andriiklymiuk/corgi/utils"
	"testing"
)

func TestCollectStatusRows_SkipsZeroPortAndManualRun(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "with-port", Driver: "postgres", Port: 5432},
			{ServiceName: "no-port", Driver: "postgres", Port: 0},
			{ServiceName: "manual", Driver: "postgres", Port: 5433, ManualRun: true},
		},
		Services: []utils.Service{
			{ServiceName: "api", Port: 3030},
			{ServiceName: "cloned-only", Port: 0},
			{ServiceName: "manual-svc", Port: 9999, ManualRun: true},
		},
	}

	rows := collectStatusRows(corgi)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}

	labels := map[string]bool{}
	for _, r := range rows {
		labels[r.Label] = true
	}
	if !labels["db_services.with-port (postgres)"] {
		t.Errorf("expected db_services.with-port row, got %+v", rows)
	}
	if !labels["services.api"] {
		t.Errorf("expected services.api row, got %+v", rows)
	}
}

func TestCollectStatusRows_HealthCheckTriggersHTTP(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "api", Port: 3030, HealthCheck: "/health"},
			{ServiceName: "front", Port: 3010},
		},
	}
	rows := collectStatusRows(corgi)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	for _, r := range rows {
		switch r.Label {
		case "services.api":
			if r.Kind != "http" {
				t.Errorf("api with HealthCheck should be http, got %s", r.Kind)
			}
			if r.URL != "http://localhost:3030/health" {
				t.Errorf("unexpected URL: %s", r.URL)
			}
		case "services.front":
			if r.Kind != "tcp" {
				t.Errorf("front without HealthCheck should be tcp, got %s", r.Kind)
			}
		}
	}
}

func TestCollectStatusRows_LocalstackDefaultsToHTTP(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "shared-aws", Driver: "localstack", Port: 4566},
		},
	}
	rows := collectStatusRows(corgi)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	if r.Kind != "http" {
		t.Errorf("localstack should default to http probe, got %s", r.Kind)
	}
	if r.URL != "http://localhost:4566/_localstack/health" {
		t.Errorf("unexpected URL: %s", r.URL)
	}
}

func TestCollectStatusRows_ExplicitHealthCheckBeatsLocalstackDefault(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "shared-aws", Driver: "localstack", Port: 4566, HealthCheck: "/custom"},
		},
	}
	rows := collectStatusRows(corgi)
	if len(rows) != 1 || rows[0].URL != "http://localhost:4566/custom" {
		t.Fatalf("explicit HealthCheck should win over driver default, got %+v", rows)
	}
}

func TestCollectStatusRows_OtherDriversStayTCP(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Driver: "postgres", Port: 5432},
			{ServiceName: "pgv", Driver: "pgvector", Port: 5436},
		},
	}
	rows := collectStatusRows(corgi)
	for _, r := range rows {
		if r.Kind != "tcp" {
			t.Errorf("%s should be tcp probe, got %s", r.Label, r.Kind)
		}
	}
}
