package cmd

import (
	"andriiklymiuk/corgi/utils"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMissionSnapshotJSON(t *testing.T) {
	snap := MissionSnapshot{
		ComposePath: "/abs/corgi-compose.yml",
		Services: []MissionService{
			{
				Name: "api", Kind: "service", Port: 8080,
				RunState: "running", Healthy: true, Detail: "localhost:8080 listening",
				AgentWork: &utils.AgentWork{
					RepoPath: "/abs/api", Branch: "feature/login", Dirty: true,
					PR: &utils.PullRequestState{
						Provider: "github", Number: 42, State: "open",
						Draft: true, URL: "https://x/pull/42", CI: "passing",
					},
				},
			},
			{Name: "db", Kind: "db_service", Port: 5432, RunState: "stopped"},
		},
		Summary: MissionSummary{Total: 2, Up: 1, Down: 1, WithOpenPR: 1},
	}

	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, want := range []string{
		`"composePath":"/abs/corgi-compose.yml"`,
		`"runState":"running"`,
		`"branch":"feature/login"`,
		`"draft":true`,
		`"ci":"passing"`,
		`"withOpenPR":1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("snapshot JSON missing %s\ngot: %s", want, out)
		}
	}
	// db_service has no agentWork -> field omitted.
	if strings.Contains(out, `"name":"db"`) && strings.Contains(out, `"db","kind":"db_service","port":5432,"runState":"stopped","agentWork"`) {
		t.Errorf("db_service should omit agentWork, got: %s", out)
	}
}

func TestBuildMissionSnapshot_MapsRunStateAndSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rows := []statusRow{
		{Label: "services.api", Port: 1, Kind: "http", URL: srv.URL},
		{Label: "services.web", Port: 2, Kind: "tcp"}, // down
	}
	// Inject a fake agent-work probe so the test doesn't shell out.
	probe := func(name string) *utils.AgentWork {
		if name == "api" {
			return &utils.AgentWork{
				Branch: "feature/x",
				PR:     &utils.PullRequestState{State: "open", Draft: true},
			}
		}
		return nil
	}
	snap := buildMissionSnapshot("/abs/corgi-compose.yml", rows, probe)

	if snap.Summary.Total != 2 || snap.Summary.Up != 1 || snap.Summary.Down != 1 {
		t.Errorf("summary = %+v", snap.Summary)
	}
	if snap.Summary.WithOpenPR != 1 {
		t.Errorf("withOpenPR = %d, want 1", snap.Summary.WithOpenPR)
	}
	var api MissionService
	for _, s := range snap.Services {
		if s.Name == "api" {
			api = s
		}
	}
	if api.RunState != "running" || !api.Healthy {
		t.Errorf("api run state = %q healthy=%v", api.RunState, api.Healthy)
	}
	if api.AgentWork == nil || api.AgentWork.Branch != "feature/x" {
		t.Errorf("api agent work = %+v", api.AgentWork)
	}
}

func TestBuildMissionFrame_ShowsRunStateAndPR(t *testing.T) {
	snap := MissionSnapshot{
		Services: []MissionService{
			{Name: "api", Kind: "service", Port: 8080, RunState: "running", Healthy: true,
				AgentWork: &utils.AgentWork{Branch: "feature/login",
					PR: &utils.PullRequestState{State: "open", Draft: true, Number: 42, CI: "passing"}}},
			{Name: "db", Kind: "db_service", Port: 5432, RunState: "stopped"},
		},
		Summary: MissionSummary{Total: 2, Up: 1, Down: 1, WithOpenPR: 1},
	}
	out := buildMissionFrame(snap, 2*time.Second, time.Now())
	for _, want := range []string{"api", "feature/login", "#42", "draft", "passing", "db", "1 up", "1 down"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame missing %q\n%s", want, out)
		}
	}
}

func TestRunMissionLoop_TerminatesOnCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "services.api", Kind: "http", URL: srv.URL, Port: 1}}
	probe := func(string) *utils.AgentWork { return nil }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runMissionLoop(ctx, "/abs/c.yml", rows, probe, 50*time.Millisecond, false)
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("mission loop did not stop after cancel")
	}
}

func TestRunMissionLoop_JSONEmitsOneSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "services.api", Kind: "http", URL: srv.URL, Port: 1}}
	probe := func(string) *utils.AgentWork { return nil }
	out := captureStdout(t, func() {
		// jsonOnce=true: emit one snapshot, no loop.
		runMissionOnce("/abs/c.yml", rows, probe, true)
	})
	var got MissionSnapshot
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("expected one JSON snapshot on stdout, got %q: %v", out, err)
	}
	if got.Summary.Total != 1 {
		t.Errorf("summary = %+v", got.Summary)
	}
}
