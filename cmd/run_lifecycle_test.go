package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"andriiklymiuk/corgi/utils"
)

// --- spawnDetachedServices (seam-injected, no real processes) ---

func TestSpawnDetachedServices_StartsServiceViaSeam(t *testing.T) {
	os, ds := startDetachedFn, dockerRunnerUp
	t.Cleanup(func() { startDetachedFn, dockerRunnerUp = os, ds })

	var started []string
	startDetachedFn = func(name, command, path string, envFile ...string) (*osProcess, error) {
		started = append(started, name)
		return fakeProcess(4242), nil
	}

	corgi := &utils.CorgiCompose{Services: []utils.Service{
		{ServiceName: "api", Port: 3000, Start: []string{"go run ."}},
	}}
	procs := spawnDetachedServices(corgi)
	if len(procs) != 1 || procs[0].name != "api" {
		t.Fatalf("expected one api proc, got %+v", procs)
	}
	if procs[0].pid != 4242 || procs[0].pgid != 4242 {
		t.Errorf("pid/pgid not recorded: %+v", procs[0])
	}
	if len(started) != 1 {
		t.Errorf("startDetachedFn called %d times, want 1", len(started))
	}
}

func TestSpawnDetachedServices_SkipsNoStartCommand(t *testing.T) {
	osf := startDetachedFn
	t.Cleanup(func() { startDetachedFn = osf })
	called := false
	startDetachedFn = func(string, string, string, ...string) (*osProcess, error) {
		called = true
		return fakeProcess(1), nil
	}
	// Service with no Start and no docker runner port → nothing to spawn.
	corgi := &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "noop"}}}
	if got := spawnDetachedServices(corgi); len(got) != 0 {
		t.Fatalf("expected no procs, got %+v", got)
	}
	if called {
		t.Error("startDetachedFn must not be called for a service with no start")
	}
}

func TestSpawnDetachedServices_DockerRunnerViaSeam(t *testing.T) {
	dr := dockerRunnerUp
	t.Cleanup(func() { dockerRunnerUp = dr })
	var up []string
	dockerRunnerUp = func(name string) error { up = append(up, name); return nil }

	corgi := &utils.CorgiCompose{Services: []utils.Service{
		{ServiceName: "dbx", Port: 5432, Runner: utils.Runner{Name: "docker"}},
	}}
	procs := spawnDetachedServices(corgi)
	if len(procs) != 1 || procs[0].command != "make up" || procs[0].pid != 0 {
		t.Fatalf("expected one docker-runner proc with pid 0, got %+v", procs)
	}
	if len(up) != 1 || up[0] != "dbx" {
		t.Errorf("dockerRunnerUp not invoked for dbx: %v", up)
	}
}

func TestSpawnDetachedServices_StartErrorIsSkipped(t *testing.T) {
	osf := startDetachedFn
	t.Cleanup(func() { startDetachedFn = osf })
	startDetachedFn = func(string, string, string, ...string) (*osProcess, error) {
		return nil, errors.New("boom")
	}
	corgi := &utils.CorgiCompose{Services: []utils.Service{
		{ServiceName: "api", Port: 3000, Start: []string{"go run ."}},
	}}
	if got := spawnDetachedServices(corgi); len(got) != 0 {
		t.Fatalf("a failed start must not be recorded, got %+v", got)
	}
}

// --- runDetachedBeforeStart / runServiceAfterStop (omit-flag behavior) ---

func TestRunDetachedBeforeStart_NilBeforeStartIsNoop(t *testing.T) {
	// No BeforeStart → returns without touching the filesystem or shelling out.
	runDetachedBeforeStart(utils.Service{ServiceName: "api"})
}

func TestRunServiceAfterStop_MissingServiceIsNoop(t *testing.T) {
	corgi := &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "api"}}}
	// Unknown name → findService returns nil → no-op, no panic.
	runServiceAfterStop(corgi, "ghost")
	// Known service with no AfterStart → also a no-op.
	runServiceAfterStop(corgi, "api")
}

// --- settleDetached (status classification) ---

func TestSettleDetached_EmptyIsNoop(t *testing.T) {
	settleDetached(nil) // must not sleep or panic on empty input
}

func TestSettleDetached_SkipsPidZero(t *testing.T) {
	// pid==0 (docker runner) is left with whatever status it had.
	procs := []detachedProc{{name: "dbx", pid: 0, status: "running"}}
	settleDetached(procs)
	if procs[0].status != "running" {
		t.Errorf("pid-0 proc status changed to %q", procs[0].status)
	}
}

// --- killDetached (only kills real process groups) ---

func TestKillDetached_SkipsPgidZero(t *testing.T) {
	// pgid<=0 must be skipped; with no positive pgids this is a safe no-op.
	killDetached([]detachedProc{{name: "dbx", pgid: 0}})
}

// --- markStarted / markReady (idempotent channel close) ---

func TestReadySignal_MarkIsIdempotent(t *testing.T) {
	s := &readySignal{started: make(chan struct{}), ready: make(chan struct{})}
	s.markStarted()
	s.markStarted() // second close must not panic (sync.Once)
	s.markReady()
	s.markReady()
	select {
	case <-s.started:
	default:
		t.Error("started channel not closed")
	}
	select {
	case <-s.ready:
	default:
		t.Error("ready channel not closed")
	}
}

// --- emitDepReady / emitDepTimeout (JSON vs human output) ---

func TestEmitDepReady_HumanAndJSON(t *testing.T) {
	orig := utils.JSONOutput
	t.Cleanup(func() { utils.JSONOutput = orig })

	utils.JSONOutput = false
	emitDepReady("api", "pg", "") // empty condition defaults to "ready"; must not panic
	emitDepReady("api", "pg", "started")

	utils.JSONOutput = true
	emitDepReady("api", "pg", "ready")
	emitDepTimeout("api", "pg")
}

// --- runPreflight / runBeforeStart (no-side-effect branches) ---

func TestRunPreflight_NoDockerNoVPNIsNoop(t *testing.T) {
	c := newRootedCmd()
	// Neither UseAwsVpn nor docker runners → neither init path is taken.
	runPreflight(c, &utils.CorgiCompose{})
}

func TestRunBeforeStart_EmptyIsNoop(t *testing.T) {
	runBeforeStart(&utils.CorgiCompose{}) // empty BeforeStart → no commands run
}

// --- runDetached: already-running guard returns without writing state ---

func TestRunDetached_BlockedWhenAlreadyRunning(t *testing.T) {
	dir := chdirToTempCompose(t, "name: x\n")
	statePath := utils.RunStatePath(dir)
	// Seed a running state for a docker-runner entry (pid 0 stays "running"
	// after reconcile via ContainerRunning — but we stub neither; instead use a
	// live pid so PidAlive keeps it running).
	if err := utils.WriteRunState(statePath, utils.RunState{
		ComposePath: filepath.Join(dir, "corgi-compose.yml"),
		Services: []utils.RunStateEntry{{
			Name: "api", Kind: "service", PID: syscall.Getpid(), Status: "running",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	// detachAlreadyRunning calls os.Exit(1) on a live running service without
	// --force, so assert on the guard helper directly rather than runDetached.
	// blocked==false means "proceed"; with a live pid and force=true it should
	// reconcile + clear and allow proceed.
	if blocked := detachAlreadyRunning(statePath, true); blocked {
		t.Error("force should clear prior state and allow proceed")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("force should remove stale state file, stat err=%v", err)
	}
}

// osProcess aliases *os.Process so the seam signature in tests reads cleanly.
type osProcess = os.Process

// fakeProcess builds a minimal *os.Process for seam stubs; only Pid is read.
func fakeProcess(pid int) *osProcess { return &os.Process{Pid: pid} }
