package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/brief"
	"andriiklymiuk/corgi/utils/agent/command"
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
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(dir, "daemon.json"), Info{
		PID: os.Getpid(), Executable: exe, Workspaces: []string{"acme"},
	}); err != nil {
		t.Fatal(err)
	}

	info, rerr := ReadInfo(dir)

	if rerr != nil || info == nil {
		t.Fatalf("ReadInfo() = %+v, %v; want the live record", info, rerr)
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

// Pids are recycled. A record left by an unclean exit must not make
// `corgi agent stop` signal whatever now holds that number.
func TestReadInfoRejectsARecycledPid(t *testing.T) {
	dir := t.TempDir()
	if err := writeJSONAtomic(filepath.Join(dir, "daemon.json"), Info{
		PID:        os.Getpid(),
		Executable: "/usr/local/bin/something-else-entirely",
	}); err != nil {
		t.Fatal(err)
	}

	info, err := ReadInfo(dir)

	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Error("a live pid running a different binary must read as no daemon, not as one to signal")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "daemon.json")); !os.IsNotExist(statErr) {
		t.Error("the stale record should be cleaned up")
	}
}

// exitingStarter hands out processes that exit cleanly after a moment, which
// the supervisor classifies as the documented network timeout and restarts.
func exitingStarter() supervisor.Starter {
	var n int
	var mu sync.Mutex
	return func(ctx context.Context, _ supervisor.SpawnConfig) (supervisor.Process, error) {
		mu.Lock()
		n++
		p := &blockingProcess{pid: 2000 + n, stopped: make(chan struct{})}
		mu.Unlock()
		go func() {
			select {
			case <-time.After(10 * time.Millisecond):
			case <-ctx.Done():
			}
			p.Stop()
		}()
		return p, nil
	}
}

func TestDaemonWritesABriefWhenASessionIsReplaced(t *testing.T) {
	// The whole point of the feature: a restarted session is a NEW session, and
	// what the old one left on disk has to be recorded while it is still there.
	d := testDaemon(t)
	d.Start = exitingStarter()

	d.CaptureBrief = func(p brief.Params) *brief.Brief {
		b := brief.Capture(p, []brief.RepoState{
			{Service: "api", Branch: "feature/referral", Dirty: true},
		})
		return &b
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{cfg("acme", t.TempDir())}) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := brief.Read(d.Dir, "acme"); b != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	got, err := brief.Read(d.Dir, "acme")
	if err != nil {
		t.Fatalf("brief.Read() error = %v", err)
	}
	if got == nil {
		t.Fatal("no brief written after a restart — the handover note is the feature")
	}
	if got.Cause == "" {
		t.Error("brief must carry the exit cause, so it explains why this is a new session")
	}
	if len(got.Repos) != 1 || got.Repos[0].Branch != "feature/referral" {
		t.Errorf("brief repos = %+v, want the probed state", got.Repos)
	}
	// Whether the summary reaches the notification is the supervisor's job and
	// is asserted there, where a healthy run can be simulated without waiting
	// out MinHealthyUptime.
}

func TestDaemonWithoutABriefProbeStillRuns(t *testing.T) {
	// CaptureBrief is injected, and a nil one must disable the feature rather
	// than take the daemon down on the first restart.
	d := testDaemon(t)
	d.Start = exitingStarter()
	d.CaptureBrief = nil

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if err := d.Run(ctx, []supervisor.SpawnConfig{cfg("acme", t.TempDir())}); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() = %v", err)
	}
	if b, _ := brief.Read(d.Dir, "acme"); b != nil {
		t.Error("a nil probe must write no brief at all")
	}
}

func TestRunWaitsForTheStatusPublisherBeforeReturning(t *testing.T) {
	// Without an explicit wait, Run's deferred delete of status.json races the
	// status publisher goroutine still mid-write, resurrecting the file. That
	// surfaced under -race as a TempDir cleanup failure in the next test.
	// The delay makes the ordering deterministic.
	d := testDaemon(t)
	var publisherFinished atomic.Bool
	d.publishStopped = func() {
		time.Sleep(50 * time.Millisecond)
		publisherFinished.Store(true)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{cfg("acme", "/tmp")}) }()
	waitFor(t, func() bool { return len(d.Status().Workspaces) == 1 })

	cancel()
	<-done

	if !publisherFinished.Load() {
		t.Error("Run returned while the status publisher was still going; its cleanup can now race the publisher's next write")
	}
}

func dynDaemon(t *testing.T) *Daemon {
	t.Helper()
	d := testDaemon(t)
	d.CommandTick = 10 * time.Millisecond
	d.ResolveWorkspace = func(id, profile, name string) (supervisor.SpawnConfig, error) {
		if id == "ghost" {
			return supervisor.SpawnConfig{}, errors.New("no workspace called \"ghost\" in the registry")
		}
		c := cfg(id, "/tmp")
		c.Origin = supervisor.OriginRemote
		c.Profile = profile
		return c, nil
	}
	return d
}

func TestDynamicDaemonStartsAWorkspaceFromASpoolCommand(t *testing.T) {
	d := dynDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	if _, err := command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme", Profile: "work"}); err != nil {
		t.Fatal(err)
	}
	d.Nudge()

	waitFor(t, func() bool {
		s := d.Status()
		return len(s.Workspaces) == 1 && s.Workspaces[0].Running
	})
	w := d.Status().Workspaces[0]
	if w.Origin != supervisor.OriginRemote || w.Profile != "work" {
		t.Errorf("origin/profile = %q/%q", w.Origin, w.Profile)
	}
	cancel()
	<-done
}

func TestDynamicDaemonStartIsIdempotent(t *testing.T) {
	d := dynDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	for i := 0; i < 3; i++ {
		_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	}
	d.Nudge()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })
	time.Sleep(50 * time.Millisecond)
	if n := len(d.Status().Workspaces); n != 1 {
		t.Fatalf("duplicate starts made %d runners", n)
	}
	cancel()
	<-done
}

func TestDynamicDaemonStopsAndRestartsAWorkspace(t *testing.T) {
	d := dynDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStop, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && !s.Workspaces[0].Running })

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })

	cancel()
	<-done
}

func TestDynamicDaemonReportsAFailedResolveInDiagnostics(t *testing.T) {
	d := dynDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "ghost"})
	d.Nudge()
	waitFor(t, func() bool {
		for _, diag := range d.Status().Diagnostics {
			if diag.WorkspaceID == "ghost" && strings.Contains(diag.Warning, "ghost") {
				return true
			}
		}
		return false
	})
	if len(d.Status().Workspaces) != 0 {
		t.Error("a failed resolve must not create a runner")
	}
	cancel()
	<-done
}

func TestDynamicDaemonNotifiesOnRemoteStart(t *testing.T) {
	d := dynDaemon(t)
	var mu sync.Mutex
	var titles []string
	d.Notify = func(title, _ string) { mu.Lock(); titles = append(titles, title); mu.Unlock() }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme", Source: "mcp"})
	d.Nudge()
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(titles) > 0 && strings.Contains(titles[0], "acme")
	})
	cancel()
	<-done
}

func TestDynamicDaemonAllowsAnEmptyStartupSet(t *testing.T) {
	d := dynDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := d.Run(ctx, nil); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a command-capable daemon must stay up with zero autostart workspaces, got %v", err)
	}
}

func TestDaemonSurvivesASIGUSR1Nudge(t *testing.T) {
	// SIGUSR1's default disposition is to terminate the process. The daemon must
	// install its handler before it becomes nudge-able and keep it for its whole
	// lifetime, so a nudge is caught, never fatal — on the fixed path too.
	d := testDaemon(t) // no ResolveWorkspace: the fixed lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{cfg("acme", "/tmp")}) }()
	waitFor(t, func() bool { return len(d.Status().Workspaces) == 1 })

	if err := syscall.Kill(os.Getpid(), syscall.SIGUSR1); err != nil {
		t.Skipf("cannot raise SIGUSR1 on this platform: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	select {
	case <-done:
		t.Fatal("the daemon exited on SIGUSR1 — the handler was not installed")
	default:
	}
	if len(d.Status().Workspaces) != 1 {
		t.Error("the daemon should still be supervising after a nudge")
	}
	cancel()
	<-done
}

func TestInfoCarriesCommandSupport(t *testing.T) {
	d := New("test", t.TempDir())
	d.ResolveWorkspace = func(string, string, string) (supervisor.SpawnConfig, error) {
		return supervisor.SpawnConfig{}, nil
	}
	if err := d.writeInfoIDs(nil); err != nil {
		t.Fatal(err)
	}
	info, err := ReadInfo(d.Dir)
	if err != nil || info == nil {
		t.Fatalf("ReadInfo = %+v, %v", info, err)
	}
	if !info.Commands {
		t.Error("a command-capable daemon must advertise Commands so a nudge is safe to send")
	}
}

func TestDynamicDaemonStopOfANeverStartedWorkspaceIsACleanNoOp(t *testing.T) {
	d := dynDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStop, WorkspaceID: "acme"})
	d.Nudge()
	time.Sleep(60 * time.Millisecond)

	s := d.Status()
	if len(s.Workspaces) != 0 {
		t.Error("stopping a never-started workspace must not create a runner")
	}
	for _, diag := range s.Diagnostics {
		if strings.Contains(diag.Warning, "failed") {
			t.Errorf("a no-op stop must not flash a failure: %q", diag.Warning)
		}
	}
	cancel()
	<-done
}

func TestDynamicDaemonNotifiesOnRemoteStop(t *testing.T) {
	d := dynDaemon(t)
	var mu sync.Mutex
	var bodies []string
	d.Notify = func(_, body string) { mu.Lock(); bodies = append(bodies, body); mu.Unlock() }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStop, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, b := range bodies {
			if strings.Contains(b, "stopped") {
				return true
			}
		}
		return false
	})
	cancel()
	<-done
}

func TestDynamicDaemonDropsAStaleCommandWithoutStarting(t *testing.T) {
	d := dynDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	_, _ = command.Write(d.Dir, command.Command{
		Action: command.ActionStart, WorkspaceID: "acme",
		RequestedAt: time.Now().Add(-2 * command.TTL),
	})
	d.Nudge()
	time.Sleep(80 * time.Millisecond)

	if n := len(d.Status().Workspaces); n != 0 {
		t.Errorf("a command older than the TTL must never start a session, got %d", n)
	}
	cancel()
	<-done
}

func TestDynamicDaemonDrainsOnTheTickWithoutANudge(t *testing.T) {
	d := dynDaemon(t) // CommandTick is 10ms
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	// No Nudge — only the tick can pick this up.
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })
	cancel()
	<-done
}

func TestDynamicDaemonRestartBatchDoesNotDropTheStart(t *testing.T) {
	// The phone "restart my session" gesture is a stop immediately followed by a
	// start, both draining in one batch. The start must not be deduplicated
	// against the runner the stop is tearing down, or the session would stop and
	// never come back.
	d := dynDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })

	now := time.Now().UTC()
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStop, WorkspaceID: "acme", RequestedAt: now})
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme", RequestedAt: now.Add(time.Millisecond)})
	d.Nudge()

	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })
	// And it stays running — the stop's backgrounded teardown must not later
	// knock out the fresh runner.
	time.Sleep(80 * time.Millisecond)
	if s := d.Status(); len(s.Workspaces) != 1 || !s.Workspaces[0].Running {
		t.Fatalf("after a stop+start batch the workspace must be running, got %+v", s.Workspaces)
	}
	cancel()
	<-done
}

func TestPackageNudgeIsSafeWithoutCommandSupport(t *testing.T) {
	// nil, non-positive pid, and a daemon that does not advertise command
	// support must all be no-ops — never signal a process that has no handler.
	Nudge(nil)
	Nudge(&Info{PID: 0, Commands: true})
	Nudge(&Info{PID: os.Getpid(), Commands: false})
	// A command-capable record for our own pid delivers a (harmless) SIGUSR1;
	// the test process has the daemon's handler installed only inside Run, so we
	// do not send to self here — the no-op branches are the contract under test.
}

func TestAttentionCommandNotifiesAndRecords(t *testing.T) {
	d := dynDaemon(t)
	var mu sync.Mutex
	var bodies []string
	d.Notify = func(_, body string) {
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	if _, err := command.Write(d.Dir, command.Command{
		Action: command.ActionAttention, WorkspaceID: "acme",
		Detail: "Claude needs your permission to run rm", Source: "hook",
	}); err != nil {
		t.Fatal(err)
	}
	d.Nudge()

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(bodies) > 0
	})
	mu.Lock()
	got := bodies[0]
	mu.Unlock()
	if got != "Claude needs your permission to run rm" {
		t.Errorf("notification body = %q", got)
	}

	waitFor(t, func() bool {
		evs := d.Events.Read("acme", 1)
		return len(evs) == 1 && evs[0].Kind == "attention"
	})

	// A hook that sent no message still has to say something useful.
	if _, err := command.Write(d.Dir, command.Command{
		Action: command.ActionAttention, WorkspaceID: "acme", Source: "hook",
	}); err != nil {
		t.Fatal(err)
	}
	d.Nudge()
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(bodies) > 1
	})
	mu.Lock()
	second := bodies[1]
	mu.Unlock()
	if second != "a session is waiting for you" {
		t.Errorf("empty detail must fall back, got %q", second)
	}

	cancel()
	<-done

	// Attention never touches a runner: the session is usually one corgi does
	// not supervise.
	if len(d.Status().Workspaces) != 0 {
		t.Errorf("attention must not start a runner, got %+v", d.Status().Workspaces)
	}
}
