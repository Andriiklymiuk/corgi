package cmd

import (
	"andriiklymiuk/corgi/utils"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
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

func TestInitStateMap(t *testing.T) {
	rows := []statusRow{{Label: "a"}, {Label: "b"}}
	got := initStateMap(rows)
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	for _, r := range rows {
		if got[r.Label] != false {
			t.Errorf("%s = true, want false", r.Label)
		}
	}
}

func TestProbeAllSplitsUpAndDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rows := []statusRow{
		{Label: "good", Port: 1, Kind: "http", URL: srv.URL},
		{Label: "bad", Port: 2, Kind: "http", URL: "http://127.0.0.1:1"},
	}
	up, down := probeAll(rows)
	if len(up) != 1 || up[0].Row.Label != "good" {
		t.Errorf("up = %+v", up)
	}
	if len(down) != 1 || down[0].Row.Label != "bad" {
		t.Errorf("down = %+v", down)
	}
}

func TestProbeTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	ok, detail := probe(statusRow{Port: port, Kind: "tcp"})
	if !ok {
		t.Errorf("expected listening, got %s", detail)
	}
	if !strings.Contains(detail, "listening") {
		t.Errorf("detail = %q", detail)
	}
}

func TestProbeTCPDown(t *testing.T) {
	ok, detail := probe(statusRow{Port: 1, Kind: "tcp"})
	if ok {
		t.Error("port 1 should not be listening")
	}
	if !strings.Contains(detail, "not listening") {
		t.Errorf("detail = %q", detail)
	}
}

func TestProbeHTTPHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ok, detail := probe(statusRow{Kind: "http", URL: srv.URL, Port: 80})
	if !ok {
		t.Errorf("expected healthy, got %q", detail)
	}
	if !strings.Contains(detail, "HTTP 200") {
		t.Errorf("detail = %q", detail)
	}
}

func TestProbeHTTPDown(t *testing.T) {
	ok, _ := probe(statusRow{Kind: "http", URL: "http://127.0.0.1:1/x"})
	if ok {
		t.Error("expected down")
	}
}

func TestEmitJSON(t *testing.T) {
	rows := []probeResult{
		{Row: statusRow{Label: "a", Port: 100}, Healthy: true, Detail: "ok"},
	}
	emitJSON(rows, nil)
}

func TestRenderProbeResults(t *testing.T) {
	up := []probeResult{{Row: statusRow{Label: "ok", Port: 1}, Healthy: true, Detail: "fine"}}
	down := []probeResult{{Row: statusRow{Label: "bad", Port: 2}, Healthy: false, Detail: "boom"}}
	renderProbeResults(up, down)
}

func TestEmitTransitionVariants(t *testing.T) {
	r := statusRow{Label: "svc", Port: 100}
	emitTransition(r, true, "ok", false, false)
	emitTransition(r, false, "down", false, false)
	emitTransition(r, true, "ok", true, false)
	emitTransition(r, true, "ok", false, true)
}

func TestProbeAllParallel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rows := []statusRow{
		{Label: "a", Kind: "http", URL: srv.URL, Port: 1},
		{Label: "b", Kind: "http", URL: srv.URL, Port: 2},
	}
	results := probeAllParallel(rows)
	if len(results) != 2 {
		t.Errorf("got %d", len(results))
	}
	if !results["a"].Healthy || !results["b"].Healthy {
		t.Errorf("results = %+v", results)
	}
}

func TestCheckAllHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	state := initStateMap(rows)
	if !checkAllHealthy(rows, state, false, true) {
		t.Error("expected all healthy")
	}
	if !state["x"] {
		t.Error("state should be true")
	}
}

func TestCheckAllHealthyNotAll(t *testing.T) {
	rows := []statusRow{{Label: "down", Kind: "tcp", Port: 1}}
	state := initStateMap(rows)
	if checkAllHealthy(rows, state, false, true) {
		t.Error("expected not all healthy")
	}
}

func TestIsStdoutTTY(t *testing.T) {
	_ = isStdoutTTY()
}

func TestRunStatusOnceQuietAllUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	runStatusOnce(rows, statusFlags{quiet: true})
}

func TestRunStatusOnceJSONAllUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	runStatusOnce(rows, statusFlags{jsonOut: true})
}

func TestRunStatusOnceDefaultAllUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	runStatusOnce(rows, statusFlags{})
}

func TestFilterRowsMatchesPrefix(t *testing.T) {
	rows := []statusRow{
		{Label: "db_services.postgres (postgres)"},
		{Label: "services.api"},
		{Label: "services.worker"},
	}
	got := filterRows(rows, []string{"postgres", "worker"})
	if len(got) != 2 {
		t.Errorf("expected 2, got %d: %+v", len(got), got)
	}
}

func TestBuildWatchFrameOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	rows := []statusRow{{Label: "svc", Kind: "http", URL: srv.URL, Port: 1}}
	results := probeAllParallel(rows)
	out := buildWatchFrame(rows, results, 2000000000, time.Now())
	if !strings.Contains(out, "svc") {
		t.Errorf("frame missing svc: %q", out)
	}
}

func TestSplitResultsEmpty(t *testing.T) {
	up, down := splitResults(nil, nil)
	if up != nil || down != nil {
		t.Errorf("expected nil, got up=%v down=%v", up, down)
	}
}

func newTestStatusCommand() (*cobra.Command, *cobra.Command) {
	root := &cobra.Command{Use: "corgi"}
	c := &cobra.Command{Use: "status"}
	root.AddCommand(c)
	for _, f := range []string{"filename", "fromTemplate", "fromTemplateName", "privateToken", "dockerContext"} {
		root.Flags().String(f, "", "")
	}
	for _, f := range []string{"exampleList", "describe", "fromScratch", "runOnce"} {
		root.Flags().Bool(f, false, "")
	}
	c.Flags().Bool("global", false, "")
	c.Flags().StringSlice("service", nil, "")
	return root, c
}

func TestResolveStatusRowsNoPortServices(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	content := "name: test\nservices:\n  nosvc:\n    port: 0\n"
	if err := os.WriteFile(yml, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestStatusCommand()
	rows := resolveStatusRows(c)
	if rows != nil {
		t.Errorf("expected nil rows for no-port services, got %v", rows)
	}
}

func TestResolveStatusRowsWithPort(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	content := "name: test\nservices:\n  api:\n    port: 3000\n"
	if err := os.WriteFile(yml, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestStatusCommand()
	rows := resolveStatusRows(c)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %v", len(rows), rows)
	}
	if rows[0].Label != "services.api" {
		t.Errorf("unexpected label: %s", rows[0].Label)
	}
}

func TestResolveStatusRowsWithFilter(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	content := "name: test\nservices:\n  api:\n    port: 3000\n  worker:\n    port: 3001\n"
	if err := os.WriteFile(yml, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })

	_, c := newTestStatusCommand()
	c.Flags().Set("service", "api")
	rows := resolveStatusRows(c)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after filter, got %d", len(rows))
	}
}

func TestRunStatusUntilHealthyAllUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "svc", Kind: "http", URL: srv.URL, Port: 1}}
	runStatusUntilHealthy(rows, 50*time.Millisecond, 5*time.Second, false, true)
}

func TestRunStatusUntilHealthyAllUpJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "svc", Kind: "http", URL: srv.URL, Port: 1}}
	runStatusUntilHealthy(rows, 50*time.Millisecond, 5*time.Second, true, false)
}

func TestFinalizeHealthyQuiet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	finalize(rows, false, true, true)
}

func TestFinalizeHealthyVerbose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	finalize(rows, false, false, true)
}

func TestRunStatusWatchJSONSingleIteration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		runStatusWatch(rows, 50*time.Millisecond, false, true)
	}()
	select {
	case <-time.After(200 * time.Millisecond):
	case <-done:
	}
}

func TestRunWatchAppendWithSeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	rows := []statusRow{{Label: "x", Kind: "http", URL: srv.URL, Port: 1}}
	seed := probeAllParallel(rows)

	done := make(chan struct{})
	go func() {
		defer close(done)
		runWatchAppend(rows, seed, 50*time.Millisecond, false, false)
	}()
	select {
	case <-time.After(200 * time.Millisecond):
	case <-done:
	}
}
