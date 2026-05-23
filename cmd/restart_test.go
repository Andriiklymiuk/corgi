package cmd

import (
	"encoding/json"
	"testing"

	"path/filepath"

	"andriiklymiuk/corgi/utils"
)

func TestRestartCmdRegistered(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"restart"})
	if err != nil || c.Name() != "restart" {
		t.Fatalf("restart command not registered: %v", err)
	}
	if c.Flags().Lookup("service") == nil {
		t.Error("restart should have --service flag")
	}
}

func TestRestartCmdHasRunFlags(t *testing.T) {
	c, _, err := rootCmd.Find([]string{"restart"})
	if err != nil {
		t.Fatalf("restart command not found: %v", err)
	}
	for _, name := range []string{"detach", "force", "host"} {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("restart should have --%s flag (runRun reads it)", name)
		}
	}
}

func TestFindRestartEntry_NotStarted(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", Status: "running"},
	}}
	if _, err := findRestartEntry(st, "web"); err == nil {
		t.Fatal("expected error for service not in run-state")
	}
}

func TestFindRestartEntry_Found(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", Status: "running", PID: 123},
	}}
	e, err := findRestartEntry(st, "api")
	if err != nil || e.PID != 123 {
		t.Fatalf("expected api entry, got %+v err=%v", e, err)
	}
}

func TestUpdateServiceEntry(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Status: "crashed"},
		{Name: "web", Kind: "service", PID: 2, Status: "running"},
	}}
	out := updateServiceEntry(st, "api", 99, "npm start", 3000)
	var got utils.RunStateEntry
	for _, e := range out.Services {
		if e.Name == "api" {
			got = e
		}
	}
	if got.PID != 99 || got.Status != "running" || got.Port != 3000 {
		t.Fatalf("api not updated: %+v", got)
	}
	for _, e := range out.Services {
		if e.Name == "web" && e.PID != 2 {
			t.Fatalf("web should be untouched: %+v", e)
		}
	}
}

func TestResolveRestartTarget(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, ".state.json")
	corgi := &utils.CorgiCompose{Services: []utils.Service{
		{ServiceName: "api", Port: 3000, Start: []string{"echo hi"}},
	}}

	// 1. no state file -> E_NOT_RUNNING
	if _, _, _, code, err := resolveRestartTarget(statePath, corgi, "api"); err == nil || code != utils.ErrNotRunning {
		t.Fatalf("no-state: code=%q err=%v", code, err)
	}

	// write a run-state with api only
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", Status: "running", PID: 42},
	}}
	if err := utils.WriteRunState(statePath, st); err != nil {
		t.Fatal(err)
	}

	// 2. service not in state -> E_NOT_RUNNING
	if _, _, _, code, err := resolveRestartTarget(statePath, corgi, "web"); err == nil || code != utils.ErrNotRunning {
		t.Fatalf("not-in-state: code=%q err=%v", code, err)
	}

	// 3. in state but not in compose -> E_SERVICE_NOT_FOUND
	emptyCorgi := &utils.CorgiCompose{}
	if _, _, _, code, err := resolveRestartTarget(statePath, emptyCorgi, "api"); err == nil || code != utils.ErrServiceNotFound {
		t.Fatalf("not-in-compose: code=%q err=%v", code, err)
	}

	// 4. happy path -> no error, entry + svc resolved
	_, entry, svc, code, err := resolveRestartTarget(statePath, corgi, "api")
	if err != nil || code != "" {
		t.Fatalf("happy: code=%q err=%v", code, err)
	}
	if entry.PID != 42 || svc == nil || svc.ServiceName != "api" {
		t.Fatalf("happy resolution wrong: entry=%+v svc=%+v", entry, svc)
	}
}

func TestEmitRestartError(t *testing.T) {
	origJSON := utils.JSONOutput
	t.Cleanup(func() { utils.JSONOutput = origJSON })

	utils.JSONOutput = true
	out := captureStdout(t, func() { emitRestartError(utils.ErrNotRunning, "boom") })
	var e struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		t.Fatalf("emitRestartError json not pure: %q err=%v", out, err)
	}
	if e.Error.Code != utils.ErrNotRunning {
		t.Fatalf("wrong code: %q", e.Error.Code)
	}
}
