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
	"andriiklymiuk/corgi/utils/agent/usage"
)

// Session tracking rides on the daemon that is already always up. The hooks
// `corgi agent track enable` installs write one spool entry per event; the
// daemon folds them into the registry, reaps dead processes, joins sessions
// to editor windows, and publishes sessions.json for whatever draws the
// board. See utils/agent/sessions.

var (
	// reapInterval is how often every session's pid is probed. SessionEnd
	// does not fire for a force-quit window; this is what frees its key.
	reapInterval = 5 * time.Second
	// sweepInterval is how often stale sessions are swept and every
	// account's usage sampled, whatever the tick.
	sweepInterval = time.Minute
)

// focusBudget bounds the OS-level part of a focus. A key press never
// waits on it; the outcome comes back on the next state push.
const focusBudget = 1500 * time.Millisecond

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
		d.newSession(ctx, c.WindowID, c.Command)
	case command.ActionDismiss:
		if err := d.Sessions.Dismiss(c.SessionID, time.Now()); err != nil {
			utils.Infof("agent: dismiss %s: %v\n", c.SessionID, err)
			d.Sessions.SetNotice(err)
		}
	case command.ActionNote:
		if err := d.Sessions.SetNote(c.SessionID, c.Note); err != nil {
			d.Sessions.SetNotice(err)
		}
	case command.ActionSend:
		d.sendToSession(ctx, c.SessionID, c.Text, c.Enter)
	case command.ActionWatch:
		if c.WatchEvent != nil {
			d.handleWatchEvent(ctx, *c.WatchEvent)
		}
	case command.ActionAnswer:
		keys, err := d.Sessions.PendingAnswer(c.SessionID, c.Answer)
		if err != nil {
			utils.Infof("agent: answer %s: %v\n", c.SessionID, err)
			d.Sessions.SetNotice(err)
			return true
		}
		d.sendToSession(ctx, c.SessionID, keys, false)
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
	d.Sessions.OnTransition = d.onSessionTransition
	d.Sessions.Load()
	d.rescan()
	d.syncWindows()
	d.sampleAccounts(time.Now())
	d.flushSessions()
}

// onSessionTransition runs under the registry lock, so it only records and
// hands off: a limit lifting is worth one notification, and every wait that
// ends is a number for `corgi agent usage`.
func (d *Daemon) onSessionTransition(s sessions.Session, from, to sessions.Status, now time.Time) {
	label := s.Display
	if label == "" {
		label = s.Label
	}
	switch from {
	case sessions.StatusNeedsInput, sessions.StatusLimited:
		kind := "wait"
		if from == sessions.StatusLimited {
			kind = "limited"
		}
		if secs := int(now.Sub(s.StatusSince).Seconds()); secs > 0 {
			_ = usage.RecordWait(d.Dir, usage.Wait{At: now.UTC(), Kind: kind, Label: label, Profile: s.Profile, Seconds: secs})
		}
	}
	d.attentionMu.Lock()
	if d.limitWatch == nil {
		d.limitWatch = map[string]bool{}
	}
	lifted := false
	switch {
	case from == sessions.StatusLimited && to == sessions.StatusWorking:
		d.limitWatch[s.ID] = true
	case to == sessions.StatusLimited:
		delete(d.limitWatch, s.ID)
	case d.limitWatch[s.ID] && (to == sessions.StatusDone || to == sessions.StatusNeedsInput):
		delete(d.limitWatch, s.ID)
		lifted = true
	}
	d.attentionMu.Unlock()
	if lifted {
		go d.notifyAttention("corgi agent · "+label, "limit lifted — back to work", s.Folder)
	}
}

// sampleAccounts reads every account's cached /usage numbers, keeps the
// fresh ones for the slope, and puts the picture on the board.
func (d *Daemon) sampleAccounts(now time.Time) {
	if d.Sessions == nil {
		return
	}
	seen := map[string]bool{}
	var dirs []string
	add := func(dir string) {
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	add("")
	if d.AccountDirs != nil {
		for _, dir := range d.AccountDirs() {
			add(dir)
		}
	}
	for _, s := range d.Sessions.Sessions() {
		add(s.ConfigDir)
	}
	var accounts []sessions.Account
	for _, dir := range dirs {
		a := sessions.Account{ConfigDir: dir, Profile: sessions.DefaultProfile(dir)}
		if d.Sessions.ProfileFor != nil {
			if p := d.Sessions.ProfileFor(dir); p != "" {
				a.Profile = p
			}
		}
		if l, ok := usage.ReadLimits(dir); ok {
			a.Limits = &l
			if _, err := usage.RecordSample(d.Dir, a.Profile, l, now); err != nil {
				utils.Infof("agent: usage sample %s: %v\n", a.Profile, err)
			}
			a.Forecast = usage.ForecastFrom(usage.LoadSamples(usage.SamplesPath(d.Dir, a.Profile), now.Add(-24*time.Hour)), l, now)
		}
		accounts = append(accounts, a)
	}
	d.Sessions.SetAccounts(accounts)
}

// sendToSession types text into a session after bringing it forward. An
// integrated terminal takes it through the window's extension; iTerm2 and
// Terminal.app through AppleScript; the Claude Code panel takes nothing
// from here — its input is a web view — so the outcome says so and a key
// falls back to its own keystrokes.
func (d *Daemon) sendToSession(ctx context.Context, ref, text string, enter bool) {
	target, err := d.Sessions.Focus(ref)
	if err != nil {
		utils.Infof("agent: send to %q: %v\n", ref, err)
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
			err = d.deliverText(ctx, target, text, enter)
		}
		if err != nil {
			utils.Infof("agent: send to %s: %v\n", target.SessionID, err)
		}
		d.Sessions.RecordFocus(target.SessionID, err)
		d.flushSessions()
	}()
}

func (d *Daemon) deliverText(ctx context.Context, t sessions.FocusTarget, text string, enter bool) error {
	switch t.Kind {
	case sessions.HostVSCodeTerminal:
		if t.WindowID == "" || !t.Connected {
			return fmt.Errorf("%s: no connected window to type into", t.Label)
		}
		return sessions.WriteReveal(d.Dir, sessions.Reveal{WindowID: t.WindowID, SessionID: t.SessionID, ShellPID: t.ShellPID, Text: text, Enter: enter})
	case sessions.HostVSCodePanel:
		return fmt.Errorf("%s runs in the Claude Code panel, which takes text only from the keyboard", t.Label)
	case sessions.HostITerm, sessions.HostTerminalApp:
		if d.TypeText != nil {
			return d.TypeText(ctx, t, text, enter)
		}
		return typeIntoTerminal(ctx, t, text, enter)
	}
	return fmt.Errorf("%s: nowhere to type", t.Label)
}

// typeIntoTerminal writes text into the emulator tab a session runs in.
// iTerm2 has `write text`; Terminal.app has `do script`, which runs the
// text as a shell command, so its tab gets keystrokes through System Events
// instead (Accessibility permission for corgi).
func typeIntoTerminal(ctx context.Context, t sessions.FocusTarget, text string, enter bool) error {
	tty := proc.TTYName(t.TTY)
	if tty == "" {
		return fmt.Errorf("%s: no terminal tab to type into", t.Label)
	}
	if t.Kind == sessions.HostITerm {
		return run(ctx, "osascript", "-e", itermWriteScript(tty, text, enter))
	}
	script := `tell application "System Events" to keystroke ` + appleScriptString(text)
	if enter {
		script += "\ntell application \"System Events\" to key code 36"
	}
	return run(ctx, "osascript", "-e", script)
}

func itermWriteScript(tty, text string, enter bool) string {
	newline := "no"
	if enter {
		newline = "yes"
	}
	return `tell application "iTerm2"
	repeat with w in windows
		repeat with t in tabs of w
			repeat with s in sessions of t
				if tty of s is "` + tty + `" then
					tell s to write text ` + appleScriptString(text) + ` newline ` + newline + `
					return
				end if
			end repeat
		end repeat
	end repeat
end tell`
}

// appleScriptString quotes text for AppleScript: backslashes and quotes
// escaped, control characters spelled out.
func appleScriptString(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range text {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\r', '\n':
			b.WriteString(`" & return & "`)
		case 0x1b:
			b.WriteString(`" & (ASCII character 27) & "`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// reapSessions runs the periodic checks until ctx ends.
func (d *Daemon) reapSessions(ctx context.Context) {
	if d.Sessions == nil {
		return
	}
	fast := d.ReapTick
	if fast == 0 {
		fast = reapInterval
	}
	ticker := time.NewTicker(d.pollInterval(fast))
	defer ticker.Stop()
	nextSweep := time.Now().Add(sweepInterval)
	for {
		ticker.Reset(d.pollInterval(fast))
		select {
		case <-ctx.Done():
			return
		case <-d.reapSignal:
			continue
		case <-ticker.C:
		}
		now := time.Now()
		if len(d.Sessions.Sessions()) > 0 {
			d.Sessions.Reap(d.alive, now)
		}
		if !now.Before(nextSweep) {
			nextSweep = now.Add(sweepInterval)
			d.Sessions.Sweep(now)
			d.sampleAccounts(now)
			d.autoContinue(ctx, now)
			d.checkDrift(now)
			d.runRoutines(ctx, now)
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
func (d *Daemon) newSession(ctx context.Context, windowID, cmdline string) {
	if strings.TrimSpace(cmdline) == "" {
		cmdline = newSessionCommand()
	}
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
			err = sessions.WriteReveal(d.Dir, sessions.Reveal{WindowID: target.WindowID, New: true, Folder: target.Folder, Command: cmdline})
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
			WindowID: t.WindowID, SessionID: t.SessionID, ShellPID: t.ShellPID, Panel: t.Panel, Title: t.Title,
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
func newSessionCommand() string { return NewSessionCommand() }

// NewSessionCommand is the shell line a fresh terminal runs: this corgi
// binary, `agent claude`, and the given flags. Every argument is quoted, so
// a caller must still keep user text out of it: a prompt travels by id.
func NewSessionCommand(args ...string) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "corgi"
	}
	parts := []string{shellQuote(exe), "agent", "claude"}
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"$`\\!*?[](){}<>|;&#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
