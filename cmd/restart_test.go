package cmd

import (
	"testing"

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
