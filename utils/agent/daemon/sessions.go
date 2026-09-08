package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

// Session tracking rides on the daemon that is already always up. The hooks
// `corgi agent track enable` installs write one spool entry per event; the
// daemon folds them into the registry, reaps dead processes, joins sessions
// to editor windows, and publishes sessions.json for whatever draws the
// board. See utils/agent/sessions.

const (
	// reapInterval is how often every session's pid is probed. SessionEnd
	// does not fire for a force-quit window; this is what frees its key.
	reapInterval = 5 * time.Second
	// sweepEvery is how many reap ticks pass between stale sweeps (one
	// minute at the default interval).
	sweepEvery = 12
	// focusBudget bounds the OS-level part of a focus. A key press never
	// waits on it; the outcome comes back on the next state push.
	focusBudget = 1500 * time.Millisecond
)

// SessionsPath is where the daemon publishes the board.
func SessionsPath(dir string) string { return filepath.Join(dir, "sessions.json") }

// handleSessionCommand executes one board-related spool entry. Returns false
// for an action it does not own.
func (d *Daemon) handleSessionCommand(ctx context.Context, c command.Command) bool {
	if d.Sessions == nil {
		return false
	}
	switch c.Action {
	case command.ActionSession:
		if c.Event != nil {
			d.Sessions.Apply(*c.Event)
		}
	case command.ActionFocus:
		d.focusSession(ctx, c.SessionID)
	case command.ActionPin:
		d.Sessions.Pin(c.Index, c.Pinned)
	case command.ActionPage:
		d.Sessions.Page(c.Direction)
	case command.ActionRescan:
		d.rescan()
	case command.ActionResize:
		d.Sessions.Resize(c.Size)
	case command.ActionNew:
		d.newSession(ctx, c.WindowID)
	case command.ActionDismiss:
		if err := d.Sessions.Dismiss(c.SessionID, time.Now()); err != nil {
			utils.Infof("agent: dismiss %s: %v\n", c.SessionID, err)
			d.Sessions.SetNotice(err)
		}
	default:
		return false
	}
	return true
}

// flushSessions publishes the board when it changed. Called at the end of
// every drain and reaper tick, so a burst of tool events is one write.
func (d *Daemon) flushSessions() {
	if d.Sessions == nil {
		return
	}
	if err := d.Sessions.Save(); err != nil {
		utils.Infof("agent: writing sessions.json: %v\n", err)
	}
}

// startSessionTracking restores the previous board, adopts sessions that
// started while the daemon was down, and connects the windows on disk.
func (d *Daemon) startSessionTracking() {
	if d.Sessions == nil {
		return
	}
	d.Sessions.Load()
	d.rescan()
	d.syncWindows()
	d.flushSessions()
}

// reapSessions runs the periodic checks until ctx ends.
func (d *Daemon) reapSessions(ctx context.Context) {
	if d.Sessions == nil {
		return
	}
	interval := d.ReapTick
	if interval == 0 {
		interval = reapInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for tick := 1; ; tick++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		d.Sessions.Reap(d.alive, now)
		if tick%sweepEvery == 0 {
			d.Sessions.Sweep(now)
		}
		d.flushSessions()
	}
}

func (d *Daemon) alive(pid int) bool {
	if d.Alive != nil {
		return d.Alive(pid)
	}
	return proc.Alive(pid)
}

// syncWindows reads the editor windows the corgi VS Code extension left on
// disk and re-runs the join. Runs on every drain, because the extension
// nudges the daemon after writing its record. Cheap: a handful of small
// files, and the join is skipped when none of them changed.
func (d *Daemon) syncWindows() {
	if d.Sessions == nil {
		return
	}
	d.Sessions.SetWindows(sessions.LoadWindows(d.Dir, d.alive))
}

// rescan registers every live Claude process no hook has reported.
func (d *Daemon) rescan() {
	if d.Sessions == nil || d.ListProcesses == nil {
		return
	}
	procs, err := d.ListProcesses()
	if err != nil {
		utils.Infof("agent: listing processes: %v\n", err)
		return
	}
	if n := d.Sessions.Adopt(procs, d.Cwd, time.Now()); n > 0 {
		utils.Infof("agent: adopted %d running Claude session(s) no hook reported\n", n)
	}
}

// focusSession resolves a reference and dispatches the focus off the command
// loop: a slow `open` must not hold up the next event.
func (d *Daemon) focusSession(ctx context.Context, ref string) {
	target, err := d.Sessions.Focus(ref)
	if err != nil {
		utils.Infof("agent: focus %q: %v\n", ref, err)
		return
	}
	d.swaps.Add(1)
	go func() {
		defer d.swaps.Done()
		d.dispatchFocus(ctx, target)
	}()
}

// newSession asks an editor window for a fresh terminal running claude:
// raise the window, then leave the request its extension acts on. The "+"
// key. A failure is a board notice, since no session exists yet to carry it.
func (d *Daemon) newSession(ctx context.Context, windowID string) {
	target, err := d.Sessions.NewSessionTarget(windowID)
	if err != nil {
		utils.Infof("agent: new session: %v\n", err)
		d.Sessions.SetNotice(err)
		return
	}
	d.swaps.Add(1)
	go func() {
		defer d.swaps.Done()
		ctx, cancel := context.WithTimeout(ctx, focusBudget)
		defer cancel()
		raise := d.Raise
		if raise == nil {
			raise = raiseWindow
		}
		err := raise(ctx, target)
		if err == nil {
			err = sessions.WriteReveal(d.Dir, sessions.Reveal{WindowID: target.WindowID, New: true, Folder: target.Folder, Command: newSessionCommand()})
		}
		if err != nil {
			utils.Infof("agent: new session in %s: %v\n", target.WindowID, err)
		}
		d.Sessions.SetNotice(err)
		d.flushSessions()
	}()
}

// dispatchFocus runs the two steps: the OS brings the window forward, then
// the window's extension reveals the tab or panel. The outcome lands on the
// session so the key can flash a failure.
func (d *Daemon) dispatchFocus(ctx context.Context, t sessions.FocusTarget) {
	ctx, cancel := context.WithTimeout(ctx, focusBudget)
	defer cancel()
	raise := d.Raise
	if raise == nil {
		raise = raiseWindow
	}
	err := raise(ctx, t)
	if err == nil && t.WindowID != "" && t.Connected {
		err = sessions.WriteReveal(d.Dir, sessions.Reveal{
			WindowID: t.WindowID, SessionID: t.SessionID, ShellPID: t.ShellPID, Panel: t.Panel,
		})
	}
	if err != nil {
		utils.Infof("agent: focus %s: %v\n", t.SessionID, err)
	}
	d.Sessions.RecordFocus(t.SessionID, err)
	d.flushSessions()
}

// raiseWindow is the OS-level half of a focus. On macOS `open -a <app>
// <folder>` brings forward the existing window that has the folder open —
// no Accessibility permission, no scripting. Anything not recognised is an
// error rather than a guess: raising the wrong window is worse than none.
func raiseWindow(ctx context.Context, t sessions.FocusTarget) error {
	switch t.Kind {
	case sessions.HostVSCodeTerminal, sessions.HostVSCodePanel:
		return raiseEditor(ctx, t)
	case sessions.HostITerm, sessions.HostTerminalApp:
		if runtime.GOOS == "darwin" {
			return raiseTerminal(ctx, t)
		}
	}
	return fmt.Errorf("no window known for a %s session on %s", t.Kind, runtime.GOOS)
}

// raiseEditor brings an editor window forward. The app must be known:
// TERM_PROGRAM=vscode is what Cursor and Windsurf say too, and a guess
// would start the wrong editor and open a new window.
func raiseEditor(ctx context.Context, t sessions.FocusTarget) error {
	if t.App == "" {
		return errors.New("which editor is unknown — install the corgi VS Code extension, or reopen the terminal")
	}
	switch runtime.GOOS {
	case "darwin":
		// With a folder (one a connected window reported open), the
		// window that has it comes forward; without one, the app does.
		args := []string{"-a", t.App}
		if t.Folder != "" {
			args = append(args, t.Folder)
		}
		return run(ctx, "open", args...)
	case "linux":
		if t.Folder == "" {
			// `code` alone opens a new window; there is no "just raise".
			return errors.New("no connected window to raise — install the corgi VS Code extension")
		}
		return run(ctx, editorCLI(t.App), "--reuse-window", t.Folder)
	}
	return fmt.Errorf("no window known for a %s session on %s", t.Kind, runtime.GOOS)
}

// raiseTerminal selects the exact iTerm2 or Terminal.app tab: both expose
// each tab's tty to AppleScript, and the hook recorded the claude process's
// controlling terminal. Without a tty the app comes forward on its own.
func raiseTerminal(ctx context.Context, t sessions.FocusTarget) error {
	app, script := "iTerm", itermScript
	if t.Kind == sessions.HostTerminalApp {
		app, script = "Terminal", terminalAppScript
	}
	if tty := proc.TTYName(t.TTY); tty != "" {
		if err := run(ctx, "osascript", "-e", script(tty)); err == nil {
			return nil
		}
	}
	return run(ctx, "open", "-a", app)
}

// itermScript selects the iTerm2 session on tty and brings its window up.
// The tty is a /dev path corgi resolved itself, never text from a hook.
func itermScript(tty string) string {
	return `tell application "iTerm2"
	repeat with w in windows
		repeat with t in tabs of w
			repeat with s in sessions of t
				if tty of s is "` + tty + `" then
					select s
					select t
					select w
					activate
					return
				end if
			end repeat
		end repeat
	end repeat
end tell`
}

// terminalAppScript does the same for Terminal.app.
func terminalAppScript(tty string) string {
	return `tell application "Terminal"
	repeat with w in windows
		repeat with t in tabs of w
			if tty of t is "` + tty + `" then
				set selected tab of w to t
				set index of w to 1
				activate
				return
			end if
		end repeat
	end repeat
end tell`
}

// editorCLI maps an editor's application name to its command-line launcher.
func editorCLI(app string) string {
	switch {
	case strings.Contains(app, "Insiders"):
		return "code-insiders"
	case strings.Contains(app, "Cursor"):
		return "cursor"
	case strings.Contains(app, "Codium"):
		return "codium"
	case strings.Contains(app, "Windsurf"):
		return "windsurf"
	}
	return "code"
}

func run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, string(out))
	}
	return nil
}

// newSessionCommand is what the "+" terminal runs: this corgi's `agent
// claude`, which picks the folder's workspace account and settings. The
// path is absolute so the terminal's PATH does not matter.
func newSessionCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return "corgi agent claude"
	}
	if strings.ContainsAny(exe, " \t'\"") {
		exe = "'" + strings.ReplaceAll(exe, "'", `'\''`) + "'"
	}
	return exe + " agent claude"
}
