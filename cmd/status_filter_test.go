package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestFilterRows(t *testing.T) {
	rows := []statusRow{
		{Label: "db_services.pg (postgres)", Port: 5432},
		{Label: "db_services.redis (redis)", Port: 6379},
		{Label: "services.api", Port: 3000},
		{Label: "services.web", Port: 3001},
	}

	t.Run("filters services by bare name", func(t *testing.T) {
		got := filterRows(rows, []string{"api"})
		if len(got) != 1 || got[0].Label != "services.api" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("filters db_services by bare name", func(t *testing.T) {
		got := filterRows(rows, []string{"pg"})
		if len(got) != 1 || got[0].Label != "db_services.pg (postgres)" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("csv multiple", func(t *testing.T) {
		got := filterRows(rows, []string{"api,redis"})
		if len(got) != 2 {
			t.Errorf("want 2, got %+v", got)
		}
	})

	t.Run("multiple flag args", func(t *testing.T) {
		got := filterRows(rows, []string{"api", "web"})
		if len(got) != 2 {
			t.Errorf("want 2, got %+v", got)
		}
	})

	t.Run("trim spaces in csv", func(t *testing.T) {
		got := filterRows(rows, []string{" api , redis "})
		if len(got) != 2 {
			t.Errorf("want 2, got %+v", got)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		got := filterRows(rows, []string{"nonexistent"})
		if len(got) != 0 {
			t.Errorf("want empty, got %+v", got)
		}
	})
}

func TestAnyDown(t *testing.T) {
	if !anyDown(nil, []probeResult{{Healthy: false}}) {
		t.Error("anyDown=true expected when down>0")
	}
	if anyDown([]probeResult{{Healthy: true}}, nil) {
		t.Error("anyDown=false expected when down=0")
	}
}

func TestSplitResults(t *testing.T) {
	rows := []statusRow{
		{Label: "a"},
		{Label: "b"},
	}
	results := map[string]probeResult{
		"a": {Healthy: true, Detail: "up"},
		"b": {Healthy: false, Detail: "down"},
	}
	up, down := splitResults(rows, results)
	if len(up) != 1 || up[0].Detail != "up" {
		t.Errorf("up = %+v", up)
	}
	if len(down) != 1 || down[0].Detail != "down" {
		t.Errorf("down = %+v", down)
	}
}

func TestBuildWatchFrame(t *testing.T) {
	rows := []statusRow{
		{Label: "svc-up", Port: 3000, Kind: "tcp"},
		{Label: "svc-down", Port: 3001, Kind: "tcp"},
	}
	results := map[string]probeResult{
		"svc-up":   {Row: rows[0], Healthy: true, Detail: "ok"},
		"svc-down": {Row: rows[1], Healthy: false, Detail: "nope"},
	}
	got := buildWatchFrame(rows, results, time.Second, time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))
	if !strings.Contains(got, "🩺 corgi status") {
		t.Errorf("missing header: %q", got)
	}
	if !strings.Contains(got, "svc-up") || !strings.Contains(got, "svc-down") {
		t.Errorf("missing rows: %q", got)
	}
	if !strings.Contains(got, "1 up, 1 down") {
		t.Errorf("counts wrong: %q", got)
	}
	if !strings.Contains(got, "12:00:00") {
		t.Errorf("missing timestamp: %q", got)
	}
}
