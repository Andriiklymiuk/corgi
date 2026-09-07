package daemon

import (
	"context"
	"sync"
	"testing"

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
