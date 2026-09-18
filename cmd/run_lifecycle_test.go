package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"andriiklymiuk/corgi/utils"
)

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
	if len(procs) != 1 || procs[0].command != "make upd" || procs[0].pid != 0 {
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

func TestRunDetachedBeforeStart_NilBeforeStartIsNoop(t *testing.T) {
	runDetachedBeforeStart(utils.Service{ServiceName: "api"})
}

func TestRunServiceAfterStop_MissingServiceIsNoop(t *testing.T) {
	corgi := &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "api"}}}
	runServiceAfterStop(corgi, "ghost")
	runServiceAfterStop(corgi, "api")
}

func TestSettleDetached_EmptyIsNoop(t *testing.T) {
	settleDetached(nil)
}

func TestSettleDetached_SkipsPidZero(t *testing.T) {
	procs := []detachedProc{{name: "dbx", pid: 0, status: "running"}}
	settleDetached(procs)
	if procs[0].status != "running" {
		t.Errorf("pid-0 proc status changed to %q", procs[0].status)
	}
}

func TestKillDetached_SkipsPgidZero(t *testing.T) {
	killDetached([]detachedProc{{name: "dbx", pgid: 0}})
}

func TestReadySignal_MarkIsIdempotent(t *testing.T) {
	s := &readySignal{started: make(chan struct{}), ready: make(chan struct{})}
	s.markStarted()
	s.markStarted()
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

func TestEmitDepReady_HumanAndJSON(t *testing.T) {
	orig := utils.JSONOutput
	t.Cleanup(func() { utils.JSONOutput = orig })

	utils.JSONOutput = false
	emitDepReady("api", "pg", "")
	emitDepReady("api", "pg", "started")

	utils.JSONOutput = true
	emitDepReady("api", "pg", "ready")
	emitDepTimeout("api", "pg")
}

func TestRunPreflight_NoDockerNoVPNIsNoop(t *testing.T) {
	c := newRootedCmd()
	runPreflight(c, &utils.CorgiCompose{})
}

func TestRunBeforeStart_EmptyIsNoop(t *testing.T) {
	runBeforeStart(&utils.CorgiCompose{})
}

func TestRunDetached_BlockedWhenAlreadyRunning(t *testing.T) {
	dir := chdirToTempCompose(t, "name: x\n")
	statePath := utils.RunStatePath(dir)
	if err := utils.WriteRunState(statePath, utils.RunState{
		ComposePath: filepath.Join(dir, "corgi-compose.yml"),
		Services: []utils.RunStateEntry{{
			Name: "api", Kind: "service", PID: syscall.Getpid(), Status: "running",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if blocked := detachAlreadyRunning(statePath, true); blocked {
		t.Error("force should clear prior state and allow proceed")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("force should remove stale state file, stat err=%v", err)
	}
}

type osProcess = os.Process

func fakeProcess(pid int) *osProcess { return &os.Process{Pid: pid} }

func TestOmitted_FlagAndEnv(t *testing.T) {
	prev := omitItems
	t.Cleanup(func() { omitItems = prev })

	omitItems = nil
	t.Setenv("CORGI_OMIT", "")
	if omitted(utils.UseAwsVpnInConfig) {
		t.Error("nothing omitted → false")
	}

	omitItems = []string{utils.UseAwsVpnInConfig}
	if !omitted(utils.UseAwsVpnInConfig) || omitted(utils.UseDockerInConfig) {
		t.Error("--omit should match exactly the listed key")
	}

	omitItems = nil
	t.Setenv("CORGI_OMIT", " useDocker , useAwsVpn ")
	if !omitted(utils.UseAwsVpnInConfig) || !omitted(utils.UseDockerInConfig) {
		t.Error("CORGI_OMIT should be split on commas and trimmed")
	}
	if omitted(utils.BeforeStartInConfig) {
		t.Error("unlisted key must not be omitted")
	}
}

func TestRunPreflight_OmitUseAwsVpnSkipsInit(t *testing.T) {
	prevInit, prevOmit := awsVpnInit, omitItems
	t.Cleanup(func() { awsVpnInit, omitItems = prevInit, prevOmit })
	t.Setenv("CORGI_OMIT", "")

	calls := 0
	awsVpnInit = func() error { calls++; return nil }
	c := newRootedCmd()
	corgi := &utils.CorgiCompose{UseAwsVpn: true}

	omitItems = nil
	runPreflight(c, corgi)
	if calls != 1 {
		t.Fatalf("useAwsVpn: true should init the VPN once, got %d", calls)
	}

	omitItems = []string{utils.UseAwsVpnInConfig}
	runPreflight(c, corgi)
	if calls != 1 {
		t.Fatalf("--omit useAwsVpn must skip VPN init, got %d calls", calls)
	}
}

func TestWithOmit_RestoresPrevious(t *testing.T) {
	prev := omitItems
	t.Cleanup(func() { omitItems = prev })
	omitItems = []string{"beforeStart"}

	withOmit([]string{utils.UseAwsVpnInConfig}, func() {
		if !omitted("beforeStart") || !omitted(utils.UseAwsVpnInConfig) {
			t.Error("inside withOmit both the previous and the extra keys are omitted")
		}
	})
	if omitted(utils.UseAwsVpnInConfig) || !omitted("beforeStart") {
		t.Error("withOmit must restore the previous list on return")
	}
}

func TestUnknownOmitKeys(t *testing.T) {
	prev := omitItems
	t.Cleanup(func() { omitItems = prev })

	omitItems = []string{utils.UseAwsVpnInConfig, "useAWSVpn"}
	t.Setenv("CORGI_OMIT", "useDocker,api")
	got := unknownOmitKeys()
	if len(got) != 2 || got[0] != "useAWSVpn" || got[1] != "api" {
		t.Fatalf("expected the two typos, got %v", got)
	}

	omitItems = nil
	t.Setenv("CORGI_OMIT", "")
	if got := unknownOmitKeys(); len(got) != 0 {
		t.Fatalf("nothing requested → nothing unknown, got %v", got)
	}
}
