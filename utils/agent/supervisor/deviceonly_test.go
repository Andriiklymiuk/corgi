package supervisor

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeviceOnlyEmitsTheNoSessionFlag(t *testing.T) {
	c := baseConfig()
	c.Spawn = "worktree"
	c.DeviceOnly = true

	args, err := BuildArgs(c)
	if err != nil {
		t.Fatalf("BuildArgs() error = %v", err)
	}
	if !slices.Contains(args, DeviceOnlyFlag) {
		t.Errorf("a device-only workspace must pass %s, got %v", DeviceOnlyFlag, args)
	}

	c.DeviceOnly = false
	args, _ = BuildArgs(c)
	if slices.Contains(args, DeviceOnlyFlag) {
		t.Errorf("a workspace that wants its session must not pass %s, got %v", DeviceOnlyFlag, args)
	}
}

func TestDeviceOnlyIsRejectedForAKindHandedItsArgv(t *testing.T) {
	c := baseConfig()
	c.Kind = KindCustom
	c.Bin = "some-agent"
	c.Args = []string{"serve"}
	c.DeviceOnly = true

	err := ValidateSpawnConfig(c)
	if err == nil || !strings.Contains(err.Error(), "deviceOnly") {
		t.Fatalf("a setting a custom kind cannot honour must be rejected, got %v", err)
	}
}

func TestBuildEnvCarriesTheSessionNamePrefix(t *testing.T) {
	c := baseConfig()
	c.SessionNamePrefix = "acme"
	env := BuildEnv(c, []string{"PATH=/bin", "CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX=laptop"})

	if !slices.Contains(env, "CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX=acme") {
		t.Errorf("the workspace's prefix must reach the child, got %v", env)
	}
	if slices.Contains(env, "CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX=laptop") {
		t.Errorf("an inherited prefix must be replaced, not doubled: %v", env)
	}

	c.SessionNamePrefix = ""
	env = BuildEnv(c, []string{"CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX=laptop"})
	if !slices.Contains(env, "CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX=laptop") {
		t.Errorf("with no prefix of its own the inherited one stays: %v", env)
	}
}

func TestSessionNamePrefixIsKeptToWhatAnEnvEntryCanHold(t *testing.T) {
	for in, want := range map[string]string{
		"acme":          "acme",
		" acme-stack ":  "acme-stack",
		"a=b c":         "ab-c",
		"-.acme.-":      "acme",
		"работа":        "",
		"api_v2.stable": "api_v2-stable",
		"my stack.v2":   "my-stack-v2",
	} {
		if got := sanitizePrefix(in); got != want {
			t.Errorf("sanitizePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlagUnsupportedNeedsBothTheMarkerAndTheFlag(t *testing.T) {
	if !flagUnsupported("error: unknown option '--no-create-session-in-dir'", DeviceOnlyFlag) {
		t.Error("the CLI's own rejection must be recognised")
	}
	if flagUnsupported("unknown option '--frobnicate'", DeviceOnlyFlag) {
		t.Error("another flag's rejection is not ours")
	}
	if flagUnsupported("reading --no-create-session-in-dir from the docs", DeviceOnlyFlag) {
		t.Error("the flag's name alone, without a rejection, is just output")
	}
}

func TestRunnerDropsAFlagTheCLIDoesNotKnowAndRetriesAtOnce(t *testing.T) {
	start, _ := scriptedStarter(
		&fakeProcess{pid: 1, code: 1, exitNow: true, output: "error: unknown option '--no-create-session-in-dir'"},
		&fakeProcess{pid: 2, code: 0, uptime: 20 * time.Millisecond},
	)
	var mu sync.Mutex
	var seen []bool
	var starts atomic.Int32
	wrapped := func(ctx context.Context, cfg SpawnConfig) (Process, error) {
		mu.Lock()
		seen = append(seen, cfg.DeviceOnly)
		mu.Unlock()
		starts.Add(1)
		return start(ctx, cfg)
	}
	r := testRunner(t, wrapped)
	r.Config.DeviceOnly = true
	var slept atomic.Int32
	r.Sleep = func(context.Context, time.Duration) { slept.Add(1) }
	var events []RunEvent
	var evMu sync.Mutex
	r.OnEvent = func(e RunEvent) { evMu.Lock(); events = append(events, e); evMu.Unlock() }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()
	waitUntil(t, func() bool { return starts.Load() >= 3 })
	cancel()
	<-done

	if len(seen) < 2 || !seen[0] || seen[1] {
		t.Fatalf("the second start must drop the flag: device-only per start = %v", seen)
	}
	st := r.State()
	if st.Note == "" || !strings.Contains(st.Note, DeviceOnlyFlag) {
		t.Errorf("the state must say which flag was dropped, got note %q", st.Note)
	}
	if st.DeviceOnly {
		t.Error("a run without the flag is not device-only")
	}
	// The retry is not a restart: no backoff was slept before it, and it does
	// not count against the workspace.
	if n := slept.Load(); n > 1 {
		t.Errorf("the flag retry must not wait out a backoff; slept %d times", n)
	}
	evMu.Lock()
	defer evMu.Unlock()
	var explained bool
	for _, e := range events {
		if e.Kind == "exited" && e.Cause == string(CauseUnsupportedFlag) {
			explained = true
		}
	}
	if !explained {
		t.Errorf("the timeline must say why the first process ended, got %+v", events)
	}
}

func TestRunnerCountsSessionsPerRun(t *testing.T) {
	// One scripted run, then the starter's own blocked process, which the
	// context cancel stops.
	first := &fakeProcess{pid: 1, code: 0, uptime: 30 * time.Millisecond}
	start, _ := scriptedStarter(first)
	var links func(string)
	wrapped := func(ctx context.Context, cfg SpawnConfig) (Process, error) {
		links = cfg.OnSessionLink
		return start(ctx, cfg)
	}
	r := testRunner(t, wrapped)
	r.Config.DeviceOnly = true

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.Run(ctx) }()
	waitUntil(t, func() bool { return r.State().PID == 1 })
	links("session_a")
	links("session_b")
	if st := r.State(); st.SessionsThisRun != 2 || !st.DeviceOnly {
		t.Errorf("two links in one run = %d sessions (device-only %v)", st.SessionsThisRun, st.DeviceOnly)
	}

	waitUntil(t, func() bool { return r.State().PID == 9001 })
	if st := r.State(); st.SessionsThisRun != 0 {
		t.Errorf("a new process starts with no sessions of its own, got %d", st.SessionsThisRun)
	}
	// A session the new process brought back is one it serves now, even
	// though the cross-restart list already has it.
	links("session_a")
	if st := r.State(); st.SessionsThisRun != 1 || len(st.Sessions) != 2 {
		t.Errorf("resumed link: this run %d, all-time %d", st.SessionsThisRun, len(st.Sessions))
	}
	cancel()
	<-done
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}
