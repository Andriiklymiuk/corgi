package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestPsRowsJSONShape(t *testing.T) {
	rows := []psRow{{Name: "api", Kind: "service", Port: 8080, Status: "running", URL: "http://localhost:8080"}}
	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, rows)
	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got[0]["name"] != "api" || got[0]["status"] != "running" {
		t.Errorf("bad row: %v", got)
	}
}

func TestPsRowsEmptyIsArray(t *testing.T) {
	rows := make([]psRow, 0)
	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, rows)
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("empty rows must be [], got %q", buf.String())
	}
}

func TestPsRowOmitsPortAndURL(t *testing.T) {
	rows := []psRow{{Name: "worker", Kind: "service", Status: "unknown"}}
	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, rows)
	s := buf.String()
	if strings.Contains(s, "port") || strings.Contains(s, "url") {
		t.Errorf("port/url must be omitted when zero/empty, got %q", s)
	}
}

func TestPsRowsFromState(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Port: 8080, Status: "running"},
	}, DBServices: []utils.RunStateEntry{
		{Name: "pg", Kind: "db_service", Port: 5432, Status: "stopped"},
	}}
	rows := psRowsFromState(st)
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].Name != "api" || rows[0].Status != "running" || rows[0].Port != 8080 {
		t.Errorf("bad service row: %+v", rows[0])
	}
	if rows[0].URL != "http://localhost:8080" {
		t.Errorf("expected url, got %q", rows[0].URL)
	}
	if rows[1].Name != "pg" || rows[1].Status != "stopped" {
		t.Errorf("bad db row: %+v", rows[1])
	}
}

func TestPsRowFromEntryStartedAt(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	row := psRowFromEntry(utils.RunStateEntry{
		Name: "api", Kind: "service", Port: 8080, Status: "running", StartedAt: ts,
	})
	if row.StartedAt == nil || !row.StartedAt.Equal(ts) {
		t.Fatalf("startedAt not carried: %+v", row)
	}
	// zero startedAt → nil pointer → omitted from JSON
	row2 := psRowFromEntry(utils.RunStateEntry{Name: "x", Kind: "service", Status: "running"})
	if row2.StartedAt != nil {
		t.Errorf("zero startedAt must be nil, got %v", row2.StartedAt)
	}
	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, []psRow{row2})
	if strings.Contains(buf.String(), "startedAt") {
		t.Errorf("zero startedAt must be omitted, got %q", buf.String())
	}
}

func TestProbeDockerRunnerServices(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	old := now.Add(-time.Hour) // well past the boot grace
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "tracked", PID: 123, Port: 8080, Status: "running", StartedAt: old},         // pid-tracked → untouched
		{Name: "container-up", PID: 0, Port: 9000, Status: "stopped", StartedAt: old},      // port open → running (flip)
		{Name: "container-dead", PID: 0, Port: 9001, Status: "running", StartedAt: old},    // port closed, past grace → stopped (flip)
		{Name: "container-booting", PID: 0, Port: 9002, Status: "running", StartedAt: now}, // port closed, within grace → untouched
		{Name: "no-port", PID: 0, Status: "running", StartedAt: old},                       // no port → untouched
	}}
	out := probeDockerRunnerServices(st, func(p int) bool { return p == 9000 }, now)

	if out.Services[0].Status != "running" {
		t.Errorf("pid-tracked service must be untouched, got %q", out.Services[0].Status)
	}
	if out.Services[1].Status != "running" || !out.Services[1].StatusChangedAt.Equal(now) {
		t.Errorf("container-up should flip to running with StatusChangedAt=now, got %+v", out.Services[1])
	}
	if out.Services[2].Status != "stopped" || !out.Services[2].StatusChangedAt.Equal(now) {
		t.Errorf("container-dead should flip to stopped with StatusChangedAt=now, got %+v", out.Services[2])
	}
	if out.Services[3].Status != "running" || !out.Services[3].StatusChangedAt.IsZero() {
		t.Errorf("booting container within grace must be untouched, got %+v", out.Services[3])
	}
	if out.Services[4].Status != "running" {
		t.Errorf("pid 0 with no port must be untouched, got %q", out.Services[4].Status)
	}
}

func TestBuildPsRowsStatusFromProbe(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Driver: "postgres", Port: 5432},
		},
		Services: []utils.Service{
			{ServiceName: "api", Port: 8080},
			{ServiceName: "worker"},
		},
	}
	probe := func(port int) bool { return port == 8080 }

	rows := buildPsRows(corgi, probe)

	byName := map[string]psRow{}
	for _, r := range rows {
		byName[r.Name] = r
	}

	if r := byName["api"]; r.Status != "running" || r.Kind != "service" || r.URL != "http://localhost:8080" {
		t.Errorf("api row wrong: %+v", r)
	}
	if r := byName["db"]; r.Status != "stopped" || r.Kind != "db_service" {
		t.Errorf("db row wrong: %+v", r)
	}
	if r := byName["worker"]; r.Status != "unknown" || r.Port != 0 || r.URL != "" {
		t.Errorf("worker (no port) row wrong: %+v", r)
	}
}
