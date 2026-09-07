package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/supervisor"
)

// linkingStarter is a blocking starter that, for a device-only launch, also
// reports one on-demand session link when told to — the shape of a server
// somebody opened a conversation through from the Claude app.
func linkingStarter(withSession bool) supervisor.Starter {
	var n int
	var mu sync.Mutex
	return func(ctx context.Context, cfg supervisor.SpawnConfig) (supervisor.Process, error) {
		mu.Lock()
		n++
		p := &blockingProcess{pid: 2000 + n, stopped: make(chan struct{})}
		mu.Unlock()
		go func() { <-ctx.Done(); p.Stop() }()
		if withSession && cfg.DeviceOnly && cfg.OnSessionLink != nil {
			cfg.OnSessionLink("session_ondemand")
		}
		return p, nil
	}
}

func deviceDaemon(t *testing.T, withSession bool) (*Daemon, supervisor.SpawnConfig) {
	t.Helper()
	d := dynDaemon(t)
	d.Start = linkingStarter(withSession)
	auto := cfg("acme", "/tmp")
	auto.Origin = supervisor.OriginAutostart
	auto.DeviceOnly = true
	return d, auto
}

func TestRemoteStartGivesADeviceOnlyServerASession(t *testing.T) {
	d, auto := deviceDaemon(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{auto}) }()
	waitFor(t, func() bool {
		s := d.Status()
		return len(s.Workspaces) == 1 && s.Workspaces[0].Running && s.Workspaces[0].DeviceOnly
	})
	devicePID := d.Status().Workspaces[0].PID

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	d.Nudge()

	waitFor(t, func() bool {
		s := d.Status()
		return len(s.Workspaces) == 1 && s.Workspaces[0].Running && !s.Workspaces[0].DeviceOnly
	})
	w := d.Status().Workspaces[0]
	if w.Origin != supervisor.OriginRemote {
		t.Errorf("origin = %q, want the remote start that asked for a session", w.Origin)
	}
	if w.PID == devicePID {
		t.Error("the device-only process must be replaced, not kept")
	}
	cancel()
	<-done
}

func TestRemoteStartLeavesADeviceWithSessionsAlone(t *testing.T) {
	d, auto := deviceDaemon(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{auto}) }()
	waitFor(t, func() bool {
		s := d.Status()
		return len(s.Workspaces) == 1 && s.Workspaces[0].Running && s.Workspaces[0].SessionsThisRun == 1
	})
	pid := d.Status().Workspaces[0].PID

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool { return len(d.Status().Workspaces) == 1 })
	// Give a wrong replacement time to show up before asserting it did not.
	for i := 0; i < 20; i++ {
		d.Nudge()
	}
	w := d.Status().Workspaces[0]
	if w.PID != pid || !w.DeviceOnly {
		t.Errorf("a device serving a session must not be swapped out from under it: pid %d→%d, deviceOnly %v", pid, w.PID, w.DeviceOnly)
	}
	cancel()
	<-done
}

func TestRemoteStopPutsAnAutostartWorkspaceBackOnlineAsADevice(t *testing.T) {
	d, auto := deviceDaemon(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{auto}) }()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool {
		s := d.Status()
		return len(s.Workspaces) == 1 && s.Workspaces[0].Running && s.Workspaces[0].Origin == supervisor.OriginRemote
	})
	sessionPID := d.Status().Workspaces[0].PID

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStop, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool {
		s := d.Status()
		return len(s.Workspaces) == 1 && s.Workspaces[0].Running && s.Workspaces[0].DeviceOnly && s.Workspaces[0].PID != sessionPID
	})
	if w := d.Status().Workspaces[0]; w.Origin != supervisor.OriginAutostart {
		t.Errorf("after Stop the workspace is back on its startup settings, got origin %q", w.Origin)
	}
	cancel()
	<-done
}

// slowStopProcess is a device-only server whose teardown waits for the test
// to let it go, so a daemon shutdown can be timed to land mid-swap.
type slowStopProcess struct {
	pid     int
	release chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (p *slowStopProcess) Pid() int            { return p.pid }
func (p *slowStopProcess) Wait() (int, string) { <-p.done; return 0, "" }
func (p *slowStopProcess) Stop() {
	p.once.Do(func() { <-p.release; close(p.done) })
}

func TestShutdownDuringASwapLaunchesNothing(t *testing.T) {
	d := dynDaemon(t)
	release := make(chan struct{})
	var starts atomic.Int32
	d.Start = func(ctx context.Context, cfg supervisor.SpawnConfig) (supervisor.Process, error) {
		if starts.Add(1) == 1 {
			return &slowStopProcess{pid: 3001, release: release, done: make(chan struct{})}, nil
		}
		p := &blockingProcess{pid: 3002, stopped: make(chan struct{})}
		go func() { <-ctx.Done(); p.Stop() }()
		return p, nil
	}
	auto := cfg("acme", "/tmp")
	auto.Origin = supervisor.OriginAutostart
	auto.DeviceOnly = true

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{auto}) }()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStart, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool { return d.isReplacing("acme") })

	// The daemon is asked to stop while the old process is still going down.
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run must return once the swap it was waiting on finishes")
	}
	if n := starts.Load(); n != 1 {
		t.Errorf("a swap that completes after shutdown must launch nothing, started %d processes", n)
	}
}

func TestAnUnsupportedFlagIsForgottenForTheWorkspace(t *testing.T) {
	d := dynDaemon(t)
	d.Start = func(ctx context.Context, cfg supervisor.SpawnConfig) (supervisor.Process, error) {
		if cfg.DeviceOnly {
			return &exitingProcess{pid: 41, output: "error: unknown option '--no-create-session-in-dir'"}, nil
		}
		p := &blockingProcess{pid: 42, stopped: make(chan struct{})}
		go func() { <-ctx.Done(); p.Stop() }()
		return p, nil
	}
	auto := cfg("acme", "/tmp")
	auto.Origin = supervisor.OriginAutostart
	auto.DeviceOnly = true

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{auto}) }()
	waitFor(t, func() bool {
		s := d.Status()
		return len(s.Workspaces) == 1 && s.Workspaces[0].Running && s.Workspaces[0].PID == 42
	})
	if got, ok := d.autostartConfig("acme"); !ok || got.DeviceOnly {
		t.Errorf("after the CLI rejected the flag the startup settings must drop it, got deviceOnly=%v", got.DeviceOnly)
	}
	cancel()
	<-done
}

// exitingProcess ends at once with the given output, like a CLI rejecting
// its argv.
type exitingProcess struct {
	pid    int
	output string
}

func (p *exitingProcess) Pid() int            { return p.pid }
func (p *exitingProcess) Wait() (int, string) { return 1, p.output }
func (p *exitingProcess) Stop()               {}

func TestRemoteStopOfAnIdleDeviceIsANoOp(t *testing.T) {
	d, auto := deviceDaemon(t, false)
	var notes []string
	d.Notify = func(_, body string) { notes = append(notes, body) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, []supervisor.SpawnConfig{auto}) }()
	waitFor(t, func() bool { s := d.Status(); return len(s.Workspaces) == 1 && s.Workspaces[0].Running })
	pid := d.Status().Workspaces[0].PID

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionStop, WorkspaceID: "acme"})
	d.Nudge()
	waitFor(t, func() bool { return len(d.Status().Workspaces) == 1 })
	for i := 0; i < 20; i++ {
		d.Nudge()
	}
	if w := d.Status().Workspaces[0]; !w.Running || w.PID != pid {
		t.Errorf("stopping a device with nothing on it would only put the same thing back; running=%v pid %d→%d", w.Running, pid, w.PID)
	}
	for _, n := range notes {
		if n == "session stopped remotely" {
			t.Error("nothing was stopped, so nothing should be announced as stopped")
		}
	}
	cancel()
	<-done
}
