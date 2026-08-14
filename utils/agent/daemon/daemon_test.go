package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/supervisor"
)

// blockingProcess runs until stopped, like a healthy remote-control session.
type blockingProcess struct {
	pid     int
	stopped chan struct{}
	once    sync.Once
}

func (b *blockingProcess) Pid() int            { return b.pid }
func (b *blockingProcess) Wait() (int, string) { <-b.stopped; return 0, "" }
func (b *blockingProcess) Stop()               { b.once.Do(func() { close(b.stopped) }) }

func blockingStarter() supervisor.Starter {
	var n int
	var mu sync.Mutex
	return func(ctx context.Context, _ supervisor.SpawnConfig) (supervisor.Process, error) {
		mu.Lock()
		n++
		p := &blockingProcess{pid: 1000 + n, stopped: make(chan struct{})}
		mu.Unlock()
		go func() { <-ctx.Done(); p.Stop() }()
		return p, nil
	}
}

func testDaemon(t *testing.T) *Daemon {
	t.Helper()
	d := New("test", t.TempDir())
	d.Start = blockingStarter()
	d.Notify = func(string, string) {}
	return d
}

func cfg(id, dir string) supervisor.SpawnConfig {
	return supervisor.SpawnConfig{
		WorkspaceID: id,
		Dir:         dir,
		Spawn:       "worktree",
		WakeLock:    supervisor.WakeLockOff,
	}
}

func TestRunRejectsAnEmptyWorkspaceListWithAdvice(t *testing.T) {
	err := testDaemon(t).Run(context.Background(), nil)

	if err == nil {
		t.Fatal("running with nothing configured must be an error, not a silent no-op")
	}
	if !strings.Contains(err.Error(), "corgi agent init") {
		t.Errorf("error should say how to fix it, got %q", err)
	}
}

func TestRunValidatesEveryConfigBeforeLaunchingAnything(t *testing.T) {
	d := testDaemon(t)
	started := 0
	d.Start = func(context.Context, supervisor.SpawnConfig) (supervisor.Process, error) {
		started++
		return nil, nil
	}

	bad := cfg("bad", "/tmp")
	bad.PermissionMode = "bypassPermissions"

	err := d.Run(context.Background(), []supervisor.SpawnConfig{cfg("good", "/tmp"), bad})

	if err == nil {
		t.Fatal("an invalid config must stop startup")
	}
	if started != 0 {
		t.Error("nothing may launch before every config has been validated")
	}
	if _, statErr := os.Stat(d.InfoPath()); !os.IsNotExist(statErr) {
		t.Error("a failed startup must not leave a daemon record claiming to be running")
	}
}

func TestRunSupervisesEveryWorkspaceAndCleansUp(t *testing.T) {
	d := testDaemon(t)
	configs := []supervisor.SpawnConfig{cfg("acme", "/tmp"), cfg("side", "/tmp")}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, configs) }()

	waitFor(t, func() bool {
		s := d.Status()
		if len(s.Workspaces) != 2 {
			return false
		}
		return s.Workspaces[0].Running && s.Workspaces[1].Running
	})

	if _, err := os.Stat(d.InfoPath()); err != nil {
		t.Errorf("daemon.json should exist while running: %v", err)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run() must return once every supervisor has finished")
	}

	if _, err := os.Stat(d.InfoPath()); !os.IsNotExist(err) {
		t.Error("daemon.json must be removed on shutdown, or status would report a daemon that is gone")
	}
}

func TestStatusIsSortedForStableOutput(t *testing.T) {
	d := testDaemon(t)
	configs := []supervisor.SpawnConfig{cfg("zulu", "/tmp"), cfg("alpha", "/tmp")}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, configs) }()
	waitFor(t, func() bool { return len(d.Status().Workspaces) == 2 })

	s := d.Status()
	if s.Workspaces[0].WorkspaceID != "alpha" {
		t.Errorf("workspaces should sort by id for stable output, got %s first", s.Workspaces[0].WorkspaceID)
	}

	cancel()
	<-done
}

func TestDiagnosticNamesTheConfigDirAndStrippedCredentials(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ambient")
	d := testDaemon(t)
	c := cfg("acme", "/tmp")
	c.ConfigDir = "~/.claude-work"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{c}) }()
	waitFor(t, func() bool { return len(d.Status().Diagnostics) == 1 })

	diag := d.Status().Diagnostics[0]
	if diag.ConfigDir != "~/.claude-work" {
		t.Errorf("configDir = %q; status must say which account will actually run", diag.ConfigDir)
	}
	if len(diag.Stripped) == 0 {
		t.Error("status must report that an ambient credential was stripped — otherwise the user cannot tell which account ran")
	}
	if diag.Bin != "claude" {
		t.Errorf("bin = %q, want the resolved default", diag.Bin)
	}

	cancel()
	<-done
}

func TestDiagnosticWarnsWhenInheritingAnAmbientKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ambient")
	c := cfg("acme", "/tmp")
	c.InheritAPIKey = true

	got := diagnose(c, os.Environ())

	if got.Warning == "" {
		t.Fatal("inheriting an ambient api key must warn: remote control refuses to start with one set")
	}
	if len(got.Stripped) != 0 {
		t.Error("an opted-in credential is not stripped, so it must not be reported as such")
	}
}

func TestDiagnosticShowsDefaultConfigDirExplicitly(t *testing.T) {
	if got := diagnose(cfg("acme", "/tmp"), nil); got.ConfigDir != "<default>" {
		t.Errorf("configDir = %q, want an explicit marker rather than a blank the user must interpret", got.ConfigDir)
	}
}

func TestReadInfoTreatsADeadDaemonAsAbsent(t *testing.T) {
	dir := t.TempDir()
	// PID 0 is never a live user process; a record left by a machine that lost
	// power must not look like a running daemon.
	if err := writeJSONAtomic(filepath.Join(dir, "daemon.json"), Info{PID: 0}); err != nil {
		t.Fatal(err)
	}

	info, err := ReadInfo(dir)

	if err != nil {
		t.Fatalf("ReadInfo() = %v", err)
	}
	if info != nil {
		t.Error("a record for a dead process must read as no daemon")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "daemon.json")); !os.IsNotExist(statErr) {
		t.Error("the stale record should be cleaned up")
	}
}

func TestReadInfoFindsALiveDaemon(t *testing.T) {
	dir := t.TempDir()
	if err := writeJSONAtomic(filepath.Join(dir, "daemon.json"), Info{PID: os.Getpid(), Workspaces: []string{"acme"}}); err != nil {
		t.Fatal(err)
	}

	info, err := ReadInfo(dir)

	if err != nil || info == nil {
		t.Fatalf("ReadInfo() = %+v, %v; want the live record", info, err)
	}
	if len(info.Workspaces) != 1 || info.Workspaces[0] != "acme" {
		t.Errorf("workspaces = %v", info.Workspaces)
	}
}

func TestReadInfoMissingIsNotAnError(t *testing.T) {
	info, err := ReadInfo(t.TempDir())

	if err != nil {
		t.Fatalf("no daemon running must not be an error, got %v", err)
	}
	if info != nil {
		t.Error("want nil info")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
