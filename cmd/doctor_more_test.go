package cmd

import (
	"andriiklymiuk/corgi/utils"
	"testing"
)

func TestRunRequiredEmpty(t *testing.T) {
	if !RunRequired(nil) {
		t.Error("want true for empty")
	}
}

func TestRunDockerCheckNoDb(t *testing.T) {
	if !runDockerCheck(&utils.CorgiCompose{}) {
		t.Error("want true when no db")
	}
}

func TestCollectDeclaredPortsNoServices(t *testing.T) {
	got := collectDeclaredPorts(&utils.CorgiCompose{})
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestCollectDeclaredPortsSorts(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Driver: "postgres", Port: 5432},
			{ServiceName: "redis", Driver: "redis", Port: 6379},
		},
		Services: []utils.Service{
			{ServiceName: "api", Port: 3000},
		},
	}
	got := collectDeclaredPorts(corgi)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if got[0].Port != 3000 || got[1].Port != 5432 || got[2].Port != 6379 {
		t.Errorf("not sorted: %v", got)
	}
}

func TestCollectDeclaredPortsSkipsZeroAndManual(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Driver: "postgres", Port: 0},
		},
		Services: []utils.Service{
			{ServiceName: "manual", Port: 9999, ManualRun: true},
			{ServiceName: "ok", Port: 3000},
		},
	}
	got := collectDeclaredPorts(corgi)
	if len(got) != 1 || got[0].Port != 3000 {
		t.Errorf("got %v", got)
	}
}

func TestRunPortChecksNoPorts(t *testing.T) {
	if !runPortChecks(&utils.CorgiCompose{}) {
		t.Error("want true when no ports")
	}
}

func TestCheckRequiredIsFoundExistingCmd(t *testing.T) {
	ok, desc := checkRequiredIsFound(utils.Required{Name: "echo", CheckCmd: "echo --version"})
	if !ok {
		t.Errorf("expected echo found, desc=%s", desc)
	}
}

func TestCheckRequiredIsFoundMissing(t *testing.T) {
	ok, _ := checkRequiredIsFound(utils.Required{Name: "this-tool-does-not-exist-zzz"})
	if ok {
		t.Error("expected not found")
	}
}
