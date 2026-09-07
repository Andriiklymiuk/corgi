package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

func trackingDaemon(t *testing.T) *Daemon {
	t.Helper()
	d := dynDaemon(t)
	d.ReapTick = 10 * time.Millisecond
	d.ListProcesses = func() ([]proc.Process, error) { return nil, nil }
	d.Alive = func(int) bool { return true }
	return d
}

func readBoard(t *testing.T, d *Daemon) sessions.State {
	t.Helper()
	data, err := os.ReadFile(SessionsPath(d.Dir))
	if err != nil {
		return sessions.State{}
	}
	var st sessions.State
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("sessions.json: %v", err)
	}
	return st
}

func TestDaemonFoldsHookEventsIntoThePublishedBoard(t *testing.T) {
	d := trackingDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	ev := sessions.Event{Name: "SessionStart", SessionID: "s1", Cwd: "/tmp/acme-api", ClaudePID: 4242, At: time.Now()}
	if _, err := command.Write(d.Dir, command.Command{Action: command.ActionSession, Event: &ev}); err != nil {
		t.Fatal(err)
	}
	d.Nudge()
	waitFor(t, func() bool { return len(readBoard(t, d).Sessions) == 1 })
	st := readBoard(t, d)
	if st.Slots[0].SessionID != "s1" || st.Slots[0].Status != sessions.StatusDone || st.Slots[0].Label != "acme-api" {
		t.Fatalf("slot 0 = %+v", st.Slots[0])
	}
	info, err := os.Stat(SessionsPath(d.Dir))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("sessions.json is owner-only: %v", err)
	}

	// Pin, then page and rescan are accepted without complaint.
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionPin, Index: 0, Pinned: true})
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionPage, Direction: 1})
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionRescan})
	d.Nudge()
	waitFor(t, func() bool { return readBoard(t, d).Slots[0].Pinned })

	cancel()
	<-done
}

func TestDaemonReaperFreesAKeyWhoseProcessDied(t *testing.T) {
	d := trackingDaemon(t)
	var mu sync.Mutex
	dead := false
	d.Alive = func(pid int) bool { mu.Lock(); defer mu.Unlock(); return !dead }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	ev := sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/acme-api", ClaudePID: 4242, At: time.Now()}
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionSession, Event: &ev})
	d.Nudge()
	waitFor(t, func() bool { return len(readBoard(t, d).Sessions) == 1 })

	mu.Lock()
	dead = true
	mu.Unlock()
	waitFor(t, func() bool { return len(readBoard(t, d).Sessions) == 0 })
	if st := readBoard(t, d); !st.Slots[0].Empty {
		t.Fatalf("the key should be free again: %+v", st.Slots[0])
	}
	cancel()
	<-done
}

func TestDaemonRescanAdoptsRunningClaudeProcesses(t *testing.T) {
	d := trackingDaemon(t)
	d.ListProcesses = func() ([]proc.Process, error) {
		return []proc.Process{{PID: 9001, PPID: 1, Name: "claude"}, {PID: 9002, PPID: 1, Name: "zsh"}}, nil
	}
	d.Cwd = func(pid int) string { return "/tmp/adopted" }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()
	waitFor(t, func() bool { return len(readBoard(t, d).Sessions) == 1 })
	st := readBoard(t, d)
	if st.Sessions[0].ID != sessions.PlaceholderID(9001) || st.Sessions[0].Status != sessions.StatusUnknown || st.Sessions[0].Label != "adopted" {
		t.Fatalf("adopted = %+v", st.Sessions[0])
	}
	cancel()
	<-done

	// The board survives a restart of the daemon: a second daemon on the
	// same directory, told nothing by the process table, still lists it.
	_ = os.Remove(SessionsPath(d.Dir) + ".stale")
	again := New("test", d.Dir)
	again.Start, again.Notify = d.Start, d.Notify
	again.CommandTick, again.ReapTick = d.CommandTick, d.ReapTick
	again.ResolveWorkspace = d.ResolveWorkspace
	again.ListProcesses = func() ([]proc.Process, error) { return nil, nil }
	again.Alive = func(int) bool { return true }
	before, _ := os.Stat(SessionsPath(d.Dir))
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { defer close(done); _ = again.Run(ctx, nil) }()
	waitFor(t, func() bool {
		info, err := os.Stat(SessionsPath(d.Dir))
		return err == nil && info.ModTime().After(before.ModTime()) && len(readBoard(t, again).Sessions) == 1
	})
	if st := readBoard(t, again); st.Sessions[0].ID != sessions.PlaceholderID(9001) {
		t.Fatalf("restored = %+v", st.Sessions[0])
	}
	cancel()
	<-done
}

func TestDaemonFocusRaisesThenLeavesARevealRequest(t *testing.T) {
	d := trackingDaemon(t)
	var mu sync.Mutex
	var raised []sessions.FocusTarget
	d.Raise = func(_ context.Context, t sessions.FocusTarget) error {
		mu.Lock()
		defer mu.Unlock()
		raised = append(raised, t)
		if t.Kind == sessions.HostUnknown {
			return errors.New("nowhere to go")
		}
		return nil
	}
	// A connected window with the terminal the session runs in.
	wdir := sessions.WindowsDir(d.Dir)
	_ = os.MkdirAll(wdir, 0o700)
	win, _ := json.Marshal(sessions.Window{ID: "w1", App: "Cursor", ExtHostPID: 77, Folders: []string{"/tmp/acme-api"},
		Terminals: []sessions.Terminal{{Name: "zsh", ShellPID: 55}}, UpdatedAt: time.Now()})
	_ = os.WriteFile(filepath.Join(wdir, "w1.json"), win, 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = d.Run(ctx, nil) }()

	inWindow := sessions.Event{Name: "Stop", SessionID: "s1", Cwd: "/tmp/acme-api", ClaudePID: 100, Ancestors: []int{100, 55, 77}, Window: "w1", At: time.Now()}
	nowhere := sessions.Event{Name: "Stop", SessionID: "s2", Cwd: "/tmp/other", ClaudePID: 200, At: time.Now()}
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionSession, Event: &inWindow})
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionSession, Event: &nowhere})
	d.Nudge()
	waitFor(t, func() bool { return len(readBoard(t, d).Sessions) == 2 })

	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionFocus, SessionID: "s1"})
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionFocus, SessionID: "other"})
	_, _ = command.Write(d.Dir, command.Command{Action: command.ActionFocus, SessionID: "ghost"})
	d.Nudge()

	revealPath := filepath.Join(sessions.RevealDir(d.Dir), "w1.json")
	waitFor(t, func() bool { _, err := os.Stat(revealPath); return err == nil })
	data, _ := os.ReadFile(revealPath)
	var req sessions.Reveal
	if json.Unmarshal(data, &req) != nil || req.ShellPID != 55 || req.SessionID != "s1" || req.Panel {
		t.Fatalf("reveal = %+v", req)
	}
	waitFor(t, func() bool {
		for _, s := range readBoard(t, d).Sessions {
			if s.ID == "s2" && s.FocusError != "" {
				return true
			}
		}
		return false
	})
	mu.Lock()
	defer mu.Unlock()
	if len(raised) != 2 {
		t.Fatalf("raised = %+v", raised)
	}
	for _, r := range raised {
		if r.SessionID == "s1" && (r.App != "Cursor" || r.Folder != "/tmp/acme-api" || r.ShellPID != 55) {
			t.Fatalf("s1 target = %+v", r)
		}
	}
	cancel()
	<-done
}

func TestRaiseWindowRefusesTheUnknown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := raiseWindow(ctx, sessions.FocusTarget{Kind: sessions.HostUnknown}); err == nil {
		t.Fatal("an unknown host has no window to raise")
	}
	if err := raiseWindow(ctx, sessions.FocusTarget{Kind: sessions.HostVSCodeTerminal, Folder: "/x"}); err == nil {
		t.Fatal("an unknown editor is never guessed")
	}
	if editorCLI("Cursor") != "cursor" || editorCLI("Visual Studio Code - Insiders") != "code-insiders" || editorCLI("Visual Studio Code") != "code" || editorCLI("VSCodium") != "codium" {
		t.Fatal("editorCLI")
	}
	if err := run(ctx, "definitely-not-a-command-corgi"); err == nil {
		t.Fatal("a missing command is an error")
	}
}

func TestDaemonWithoutTrackingStillDrains(t *testing.T) {
	d := dynDaemon(t)
	d.Sessions = nil
	if d.handleSessionCommand(context.Background(), command.Command{Action: command.ActionRescan}) {
		t.Fatal("no registry, nothing handled")
	}
	d.flushSessions()
	d.startSessionTracking()
	d.syncWindows()
	d.rescan()
	d.reapSessions(context.Background())
	if !d.alive(os.Getpid()) {
		t.Fatal("default liveness probe")
	}
}

func TestTerminalScriptsNameTheTTY(t *testing.T) {
	for _, script := range []string{itermScript("/dev/ttys003"), terminalAppScript("/dev/ttys003")} {
		if !strings.Contains(script, `"/dev/ttys003"`) || !strings.Contains(script, "activate") {
			t.Fatalf("script = %s", script)
		}
	}
}
