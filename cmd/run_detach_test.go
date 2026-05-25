package cmd

import (
	"andriiklymiuk/corgi/utils"
	"testing"

	"github.com/spf13/cobra"
)

func TestDetachedDBEntries_SkipsManualRun(t *testing.T) {
	corgi := &utils.CorgiCompose{DatabaseServices: []utils.DatabaseService{
		{ServiceName: "api-db", Driver: "postgres", Port: 5432},
		{ServiceName: "manual", Driver: "redis", Port: 6379, ManualRun: true},
	}}
	got := detachedDBEntries(corgi)
	if len(got) != 1 || got[0].Name != "api-db" {
		t.Fatalf("expected only api-db, got %+v", got)
	}
	e := got[0]
	if e.Kind != "db_service" || e.Container != "postgres-api-db" || e.Status != "running" {
		t.Errorf("unexpected entry %+v", e)
	}
}

func TestApplyRunFlags_CIEnablesCIMode(t *testing.T) {
	orig := utils.CIMode
	defer func() { utils.CIMode = orig; utils.SetOnServiceCrash(nil) }()
	utils.CIMode = false

	c := &cobra.Command{}
	c.Flags().Bool("ci", false, "")
	c.Flags().Bool("notify", false, "")
	_ = c.Flags().Set("ci", "true")

	applyRunFlags(c)
	if !utils.CIMode {
		t.Error("expected CIMode true after --ci")
	}
}

func TestBuildDetachState_StatusFromProc(t *testing.T) {
	procs := []detachedProc{
		{name: "api", pid: 111, pgid: 111, status: "running"},
		{name: "web", pid: 222, pgid: 222, status: "crashed"},
		{name: "dockersvc", pid: 0, command: "make up"}, // empty status → running
	}
	st := buildDetachState("/tmp/corgi-compose.yml", procs, nil)
	if len(st.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(st.Services))
	}
	want := map[string]string{"api": "running", "web": "crashed", "dockersvc": "running"}
	for _, s := range st.Services {
		if want[s.Name] != s.Status {
			t.Errorf("%s status = %q, want %q", s.Name, s.Status, want[s.Name])
		}
	}
}
