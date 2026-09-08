package cmd

import (
	"andriiklymiuk/corgi/utils/agent/config"
	"context"
	"encoding/json"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestKeyIndex(t *testing.T) {
	for arg, want := range map[string]int{"1": 0, "#6": 5, " 3 ": 2} {
		if got, err := keyIndex(arg); err != nil || got != want {
			t.Errorf("keyIndex(%q) = %d, %v", arg, got, err)
		}
	}
	for _, bad := range []string{"0", "-1", "x", ""} {
		if _, err := keyIndex(bad); err == nil {
			t.Errorf("keyIndex(%q) should fail", bad)
		}
	}
}

func TestFormatSlot(t *testing.T) {
	lines := []string{
		formatSlot(sessions.Slot{Index: 0, Label: "acme-api", Status: sessions.StatusNeedsInput, Detail: "permission: Bash", Profile: "work", Host: sessions.HostVSCodeTerminal, ElapsedS: 75, Pinned: true, FocusError: "boom"}),
		formatSlot(sessions.Slot{Index: 1, Empty: true}),
		formatSlot(sessions.Slot{Index: 5, Pager: true, Overflow: 3}),
		formatSlot(sessions.Slot{Index: 2, Label: "web", Status: sessions.StatusGone}),
	}
	for line, wants := range map[int][]string{
		0: {" 1", "📌", "▲", "acme-api", "NEEDS YOU", "permission: Bash", "work · vscode-terminal", "1m", "⚠ boom"},
		1: {" 2  ·"},
		2: {" 6  +3"},
		3: {"✕", "CLOSED"},
	} {
		for _, w := range wants {
			if !strings.Contains(lines[line], w) {
				t.Errorf("line %d = %q, want %q", line, lines[line], w)
			}
		}
	}
	for s, word := range map[sessions.Status]string{sessions.StatusWorking: "WORKING", sessions.StatusDone: "DONE", sessions.StatusStale: "IDLE", sessions.StatusUnknown: "?"} {
		if statusWord(s) != word {
			t.Errorf("%s -> %s", s, statusWord(s))
		}
	}
	if statusGlyph(sessions.StatusStale) != "◌" || statusGlyph(sessions.StatusUnknown) != "?" || statusGlyph(sessions.StatusDone) != "✓" || statusGlyph(sessions.StatusWorking) != "●" {
		t.Fatal("glyphs")
	}
	if shortDuration(5*time.Second) != "5s" || shortDuration(3*time.Minute) != "3m" || shortDuration(90*time.Minute) != "1h30m" {
		t.Fatal("durations")
	}
}

func TestReadBoardAndPrint(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, _ := agentDir()
	rep, err := readBoard(dir)
	if err != nil || rep.Size != sessions.DefaultSize || rep.Running || rep.Path != daemon.SessionsPath(dir) {
		t.Fatalf("no board yet: %+v %v", rep, err)
	}
	out := captureStdout(t, func() { printBoard(rep, time.Now()) })
	if !strings.Contains(out, "not running") || !strings.Contains(out, "no Claude sessions tracked") {
		t.Fatalf("empty board: %s", out)
	}

	st := sessions.State{Size: 2, Overflow: 1, Slots: []sessions.Slot{
		{Index: 0, SessionID: "a", Label: "acme", Status: sessions.StatusWorking},
		{Index: 1, Pager: true, Overflow: 1},
	}, Sessions: []sessions.Session{{ID: "a"}, {ID: "b"}}, Windows: []sessions.Window{{ID: "w", App: "Cursor", ExtHostPID: 3, Folders: []string{"/f"}, Terminals: []sessions.Terminal{{Name: "zsh", ShellPID: 9}}}}}
	data, _ := json.Marshal(st)
	_ = os.MkdirAll(dir, 0o700)
	if err := os.WriteFile(daemon.SessionsPath(dir), data, 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err = readBoard(dir)
	if err != nil || len(rep.Sessions) != 2 {
		t.Fatalf("board: %+v %v", rep, err)
	}
	out = captureStdout(t, func() { printBoard(rep, time.Now()) })
	if !strings.Contains(out, "2 session(s) on 2 keys, 1 more than fit") || !strings.Contains(out, "+1") {
		t.Fatalf("board print: %s", out)
	}
	out = captureStdout(t, func() { runAgentWindows(nil, nil) })
	if !strings.Contains(out, "Cursor") || !strings.Contains(out, "terminal zsh") {
		t.Fatalf("windows: %s", out)
	}

	_ = os.WriteFile(daemon.SessionsPath(dir), []byte("{"), 0o600)
	if _, err := readBoard(dir); err == nil {
		t.Fatal("a corrupt board is an error")
	}
}

func TestSessionTrackingDoctorChecks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, _ := agentDir()
	checks := checkSessionTracking(dir)
	if len(checks) != 1 || !checks[0].OK || !strings.Contains(checks[0].Detail, "track enable") {
		t.Fatalf("tracking is optional, so off is not a failure: %+v", checks)
	}
	if err := enableTrackingIn(filepath.Join(home, ".claude", "settings.json"), "corgi", true); err != nil {
		t.Fatal(err)
	}
	st := sessions.State{Size: 6, Sessions: []sessions.Session{
		{ID: "a", Host: sessions.Host{Kind: sessions.HostUnknown}},
		{ID: "b", Host: sessions.Host{Kind: sessions.HostVSCodePanel}},
		{ID: "c", Status: sessions.StatusGone, Host: sessions.Host{Kind: sessions.HostUnknown}},
	}}
	data, _ := json.Marshal(st)
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(daemon.SessionsPath(dir), data, 0o600)
	checks = checkSessionTracking(dir)
	if len(checks) != 2 || !checks[0].OK {
		t.Fatalf("hooked: %+v", checks)
	}
	if checks[1].OK || !strings.Contains(checks[1].Detail, "1 with no known window") {
		t.Fatalf("board check: %+v", checks[1])
	}
}

func TestWatchBoardRedrawsOnPublish(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, _ := agentDir()
	_ = os.MkdirAll(dir, 0o700)
	seen := make(chan boardReport, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- watchBoard(ctx, dir, time.Time{}, func(r boardReport) { seen <- r })
	}()
	time.Sleep(100 * time.Millisecond)
	st := sessions.State{UpdatedAt: time.Now(), Size: 6, Sessions: []sessions.Session{{ID: "a"}}}
	data, _ := json.Marshal(st)
	tmp := daemon.SessionsPath(dir) + ".tmp"
	_ = os.WriteFile(tmp, data, 0o600)
	_ = os.Rename(tmp, daemon.SessionsPath(dir))
	select {
	case r := <-seen:
		if len(r.Sessions) != 1 {
			t.Fatalf("redrawn with %d sessions", len(r.Sessions))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no redraw after a publish")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMCPSessionsTool(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	out, err := mcpSessions()
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := out.(map[string]any); !ok || m["hint"] == nil {
		t.Fatalf("empty board explains itself: %+v", out)
	}
	dir, _ := agentDir()
	_ = os.MkdirAll(dir, 0o700)
	data, _ := json.Marshal(sessions.State{Size: 6, NeedsInput: 1, Sessions: []sessions.Session{{ID: "a", Status: sessions.StatusNeedsInput}}})
	_ = os.WriteFile(daemon.SessionsPath(dir), data, 0o600)
	out, err = mcpSessions()
	if err != nil {
		t.Fatal(err)
	}
	if rep, ok := out.(boardReport); !ok || rep.NeedsInput != 1 {
		t.Fatalf("board: %+v", out)
	}
}

func TestAgentBoardShowsAndSetsTheSize(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, _ := agentDir()
	c := &cobra.Command{Use: "board"}
	c.Flags().Int("slots", 0, "")
	out := captureStdout(t, func() { runAgentBoard(c, nil) })
	if !strings.Contains(out, "6 keys") || !strings.Contains(out, "not running") {
		t.Fatalf("default board: %s", out)
	}
	if err := c.Flags().Set("slots", "15"); err != nil {
		t.Fatal(err)
	}
	out = captureStdout(t, func() { runAgentBoard(c, nil) })
	if !strings.Contains(out, "15 keys once the daemon runs") {
		t.Fatalf("set without daemon: %s", out)
	}
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	if user.TrackSlots != 15 {
		t.Fatalf("slots = %d", user.TrackSlots)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "commands")); len(entries) != 0 {
		t.Fatal("no daemon, no spool entry")
	}
}
