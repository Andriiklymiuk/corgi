package cmd

import (
	"andriiklymiuk/corgi/utils"
	"strings"
	"testing"
)

func TestStopTargets(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Status: "running"},
		{Name: "web", Kind: "service", PID: 2, Status: "crashed"},
	}}
	all := stopTargets(st, "")
	if len(all) != 2 {
		t.Errorf("want 2 targets, got %d", len(all))
	}
	one := stopTargets(st, "api")
	if len(one) != 1 || one[0].Name != "api" {
		t.Errorf("want only api, got %+v", one)
	}
	none := stopTargets(st, "nope")
	if len(none) != 0 {
		t.Errorf("unknown service should yield 0 targets, got %d", len(none))
	}
}

func TestAnythingRunning(t *testing.T) {
	running := utils.RunState{
		Services:   []utils.RunStateEntry{{Name: "api", Status: "crashed"}},
		DBServices: []utils.RunStateEntry{{Name: "db", Status: "running"}},
	}
	if !anythingRunning(running) {
		t.Error("expected true when a db_service is running")
	}
	idle := utils.RunState{Services: []utils.RunStateEntry{{Name: "api", Status: "stopped"}}}
	if anythingRunning(idle) {
		t.Error("expected false when nothing is running")
	}
}

func TestRemoveStateEntry(t *testing.T) {
	entries := []utils.RunStateEntry{{Name: "api"}, {Name: "web"}, {Name: "api"}}
	out := removeStateEntry(entries, "api")
	if len(out) != 1 || out[0].Name != "web" {
		t.Errorf("expected only web to remain, got %+v", out)
	}
}

func TestEmitStopSummary_JSON(t *testing.T) {
	origJSON, origStderr := utils.JSONOutput, stopSummaryToStderr
	defer func() { utils.JSONOutput, stopSummaryToStderr = origJSON, origStderr }()
	utils.JSONOutput = true
	stopSummaryToStderr = false

	out := captureStdout(t, func() {
		emitStopSummary(stopSummary{Stopped: []string{"api"}, Failed: []stopFailure{}})
	})
	if !strings.Contains(out, `"api"`) || !strings.Contains(out, "stopped") {
		t.Errorf("expected JSON summary with api, got %q", out)
	}
}
