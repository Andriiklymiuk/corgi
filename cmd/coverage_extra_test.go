package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

// chdirToCompose drops a minimal loadable corgi-compose.yml in a temp dir and
// chdirs there, so commands that resolve the compose (autopilot, mission-control)
// succeed. Returns the dir.
func chdirToCompose(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	if err := os.WriteFile(yml, []byte("name: test\nservices:\n  api:\n    port: 3000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runRoot(t *testing.T, args ...string) {
	t.Helper()
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("%v failed: %v", args, err)
	}
}

// --- autopilot.go: pure renderers + the cobra command paths ---

func TestPrintAutopilotStatus(t *testing.T) {
	// No heartbeat yet.
	out := captureStdout(t, func() {
		printAutopilotStatus(utils.AutopilotState{Mode: utils.AutopilotUninitialized})
	})
	if out == "" {
		t.Fatal("expected status output")
	}
	// With a heartbeat + last-iteration summary.
	out = captureStdout(t, func() {
		printAutopilotStatus(utils.AutopilotState{
			Mode:          utils.AutopilotRunning,
			Iteration:     3,
			LastHeartbeat: time.Now().Add(-time.Minute),
			LastSummary:   utils.AutopilotIteration{Phase: "built", Built: 2, Skipped: 1, Awaiting: 0, Note: "ok"},
		})
	})
	for _, want := range []string{"running", "heartbeat", "built"} {
		if !contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestLoadAutopilotStatusReadError(t *testing.T) {
	dir := t.TempDir()
	// Make the state path a directory so ReadAutopilotState fails with a
	// non-NotExist error (exercises loadAutopilotStatus's error return).
	if err := os.MkdirAll(utils.AutopilotStatePath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAutopilotStatus(dir); err == nil {
		t.Fatal("a non-missing read error should propagate")
	}
}

func TestAutopilotCommandsViaCobra(t *testing.T) {
	dir := chdirToCompose(t)

	// Mode transitions (human mode): resume -> pause -> stop.
	captureStdout(t, func() { runRoot(t, "autopilot", "resume") })
	captureStdout(t, func() { runRoot(t, "autopilot", "pause") })
	captureStdout(t, func() { runRoot(t, "autopilot", "stop") })

	// Heartbeat records an iteration summary.
	captureStdout(t, func() {
		runRoot(t, "autopilot", "heartbeat", "--phase", "idle", "--built", "1", "--skipped", "2", "--note", "tick")
	})

	// Status (human) reads it back.
	captureStdout(t, func() { runRoot(t, "autopilot", "status") })

	// Status (JSON) emits the state object on stdout.
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })
	out := captureStdout(t, func() { runRoot(t, "autopilot", "status") })
	if !contains(out, `"mode"`) {
		t.Fatalf("json status missing mode field: %q", out)
	}

	// The state file landed under the compose dir.
	if _, err := os.Stat(utils.AutopilotStatePath(dir)); err != nil {
		t.Fatalf("expected an autopilot state file: %v", err)
	}
}

// --- memory.go: human + json branches (no os.Exit paths) ---

func TestMemoryListHumanAndTypeFilter(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)

	captureStdout(t, func() {
		_, _ = runMemory(t, "add", "--type", "decision", "--name", "pg-choice", "--desc", "Postgres for JSONB")
		_, _ = runMemory(t, "add", "--type", "fix", "--name", "retry-429", "--desc", "Backoff on 429")
	})

	// Human list shows both facts.
	out := captureStdout(t, func() { _, _ = runMemory(t, "list") })
	if !contains(out, "pg-choice") || !contains(out, "retry-429") {
		t.Fatalf("human list missing facts:\n%s", out)
	}
	// --type narrows to one.
	out = captureStdout(t, func() { _, _ = runMemory(t, "list", "--type", "fix") })
	if !contains(out, "retry-429") || contains(out, "pg-choice") {
		t.Fatalf("type filter wrong:\n%s", out)
	}
}

func TestMemoryAddAndIndexJSON(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })

	out := captureStdout(t, func() {
		_, _ = runMemory(t, "add", "--type", "domain", "--name", "tenants", "--desc", "Multi-tenant model")
	})
	if !contains(out, `"created"`) {
		t.Fatalf("add --json missing created path: %q", out)
	}
	out = captureStdout(t, func() { _, _ = runMemory(t, "index") })
	if !contains(out, `"facts"`) {
		t.Fatalf("index --json missing facts count: %q", out)
	}
}

func TestMemoryLintWarnsOnDanglingLink(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)

	// A fact whose [[ghost]] link has no target → a lint WARNING (not an error),
	// so the command prints the warning and exits 0.
	factDir := filepath.Join(dir, utils.MemoryDirName, "decisions")
	if err := os.MkdirAll(factDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: linker\ndescription: links nowhere\ntype: decision\nlinks: [\"[[ghost]]\"]\n---\n"
	if err := os.WriteFile(filepath.Join(factDir, "linker.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if _, err := runMemory(t, "lint"); err != nil {
			t.Fatalf("a warning-only store must lint clean, got %v", err)
		}
	})
	if !contains(out, "memory ok") {
		t.Fatalf("expected the warning summary line:\n%s", out)
	}
}

// --- suggest_history.go: human output, config, --workspace, cooldown default ---

func TestSuggestHistoryHumanFlows(t *testing.T) {
	withTempHome(t)
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)

	// record (human) then list (human) shows the entry.
	captureStdout(t, func() {
		runSuggestHistory(t, "record", "--slug", "demo", "--status", "proposed", "--title", "Demo", "--lens", "eng")
	})
	out := captureStdout(t, func() { runSuggestHistory(t, "list") })
	if !contains(out, "demo") {
		t.Fatalf("human list missing the recorded entry:\n%s", out)
	}

	// check (human): a recorded proposed slug is skipped...
	out = captureStdout(t, func() { runSuggestHistory(t, "check", "--slug", "demo") })
	if !contains(out, "skip") {
		t.Fatalf("expected a skip line:\n%s", out)
	}
	// ...and an unknown slug is ok. --cooldown 0 falls back to the default window.
	out = captureStdout(t, func() { runSuggestHistory(t, "check", "--slug", "fresh", "--cooldown", "0") })
	if !contains(out, "ok") {
		t.Fatalf("expected an ok line:\n%s", out)
	}
}

func TestSuggestHistoryConfig(t *testing.T) {
	withTempHome(t)
	// Human mode.
	out := captureStdout(t, func() { runSuggestHistory(t, "config") })
	if !contains(out, "proactive suggest") {
		t.Fatalf("human config missing the mode line:\n%s", out)
	}
	// JSON mode.
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })
	out = captureStdout(t, func() { runSuggestHistory(t, "config") })
	if !contains(out, "maxPerWeek") {
		t.Fatalf("json config missing maxPerWeek:\n%s", out)
	}
}

func TestSuggestHistoryWorkspaceFlag(t *testing.T) {
	withTempHome(t)
	ws := t.TempDir()
	// --workspace points the state file at an explicit root (cron's path).
	captureStdout(t, func() {
		runSuggestHistory(t, "record", "--slug", "ws", "--status", "filed", "--ticket", "ABC-1", "--workspace", ws)
	})
	if _, err := os.Stat(utils.SuggestHistoryPath(ws)); err != nil {
		t.Fatalf("--workspace state file not written under %s: %v", ws, err)
	}
}

// --- missioncontrol.go: label parsing, the agent-work prober, command path ---

func TestLabelToNameKind(t *testing.T) {
	cases := []struct {
		label, name, kind string
	}{
		{"services.api", "api", "service"},
		{"db_services.pg (postgres)", "pg", "db_service"},
		{"bareword", "bareword", "service"},
	}
	for _, c := range cases {
		name, kind := labelToNameKind(c.label)
		if name != c.name || kind != c.kind {
			t.Errorf("labelToNameKind(%q) = (%q,%q), want (%q,%q)", c.label, name, kind, c.name, c.kind)
		}
	}
}

func TestBuildMissionFrameShowsDirty(t *testing.T) {
	snap := MissionSnapshot{
		Services: []MissionService{
			{Name: "api", Kind: "service", RunState: "running", Healthy: true,
				AgentWork: &utils.AgentWork{Branch: "feat", Dirty: true}},
		},
		Summary: MissionSummary{Total: 1, Up: 1},
	}
	out := buildMissionFrame(snap, 0, time.Now())
	if !contains(out, "feat") || !contains(out, "*") {
		t.Fatalf("frame should mark a dirty tree:\n%s", out)
	}
}

func TestAgentWorkProber(t *testing.T) {
	dir := t.TempDir()
	yml := filepath.Join(dir, "corgi-compose.yml")
	// A service with a path so the prober resolves a repo dir for it.
	if err := os.WriteFile(yml, []byte("name: test\nservices:\n  api:\n    port: 3000\n    path: ./api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)

	// Disabled → every lookup yields nil.
	if p := agentWorkProber(missionControlCmd, true); p("api") != nil {
		t.Error("disabled prober must yield nil")
	}
	// Enabled → resolves known services; a non-git repo just probes to nil, and
	// an unknown service is nil (not in the resolved map).
	p := agentWorkProber(missionControlCmd, false)
	_ = p("api")           // exercises the resolve + ProbeAgentWork path
	if p("ghost") != nil { // unknown service
		t.Error("unknown service must yield nil")
	}
}

func TestRunMissionControlOnce(t *testing.T) {
	chdirToCompose(t)
	// Human, no agent-work probe — exercises the no-watch single-frame path.
	captureStdout(t, func() { runRoot(t, "mission-control", "--no-agent-work") })

	// JSON snapshot path.
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = false })
	out := captureStdout(t, func() { runRoot(t, "mission-control", "--no-agent-work", "--json") })
	if !contains(out, `"services"`) {
		t.Fatalf("json snapshot missing services field: %q", out)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
