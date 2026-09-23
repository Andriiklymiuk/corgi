package daemon

import (
	"andriiklymiuk/corgi/utils/agent/bots"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/harness"
	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

var (
	reapInterval  = 5 * time.Second
	sweepInterval = time.Minute
)

const focusBudget = 5 * time.Second

const liftEpisode = 10 * time.Minute

func SessionsPath(dir string) string { return filepath.Join(dir, "sessions.json") }

func (d *Daemon) handleSessionCommand(ctx context.Context, c command.Command) bool {
	if d.Sessions == nil {
		return false
	}
	switch c.Action {
	case command.ActionSession:
		if c.Event != nil {
			d.Ledger.Note(c.Event.Name, c.Event.SessionID, c.Event.Agent != "", c.Event.At)
			d.Sessions.Apply(*c.Event)
			if c.Event.Bot != "" && c.Event.SessionID != "" && !sessions.Placeholder(c.Event.SessionID) {
				if err := bots.RecordSession(bots.Path(d.Dir), c.Event.Bot, c.Event.SessionID, c.Event.At); err != nil {
					utils.Infof("agent: bot %s: %v\n", c.Event.Bot, err)
				}
			}
		}
	case command.ActionFocus:
		d.focusSession(ctx, c.SessionID)
	case command.ActionPin:
		d.Sessions.Pin(c.Index, c.Pinned)
	case command.ActionPage:
		d.Sessions.Page(c.Direction)
	case command.ActionRescan:
		d.rescan()
	case command.ActionRefresh:
		d.refresh()
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
	case command.ActionCap:
		if strings.TrimSpace(c.SessionID) == "" {
			d.SessionCap = c.Tokens
		} else if err := d.Sessions.SetCap(c.SessionID, c.Tokens); err != nil {
			d.Sessions.SetNotice(err)
		}
	case command.ActionSend:
		d.sendToSession(ctx, c.SessionID, c.Text, c.Enter)
	case command.ActionWatch:
		if c.WatchEvent != nil {
			if c.Source == "webhook" && d.watchState != nil {
				d.watchState.MarkHooked(c.WatchEvent.Source, time.Now())
			}
			if c.Retry && d.watchState != nil {
				d.watchState.Unsee(c.WatchEvent.Key)
				d.watchState.Fixes.DropDeferred(c.WatchEvent.Key)
			}
			d.handleWatchEvent(ctx, *c.WatchEvent)
		}
	case command.ActionPlan:
		d.advancePlans(ctx)
	case command.ActionAnswer:
		keys, err := d.Sessions.PendingAnswer(c.SessionID, c.Answer)
		if err != nil {
			utils.Infof("agent: answer %s: %v\n", c.SessionID, err)
			d.Sessions.SetNotice(err)
			return true
		}
		d.sendToSession(ctx, c.SessionID, keys, false)
	case command.ActionRead:
		_ = d.Sessions.ReadBy(c.SessionID, time.Now())
	case command.ActionContinue:
		d.continueHeadless(ctx, c.SessionID, c.Text)
	case command.ActionInterrupt:
		keys, err := d.Sessions.InterruptKeys(c.SessionID)
		if err != nil {
			utils.Infof("agent: interrupt %s: %v\n", c.SessionID, err)
			d.Sessions.SetNotice(err)
			return true
		}
		id := c.SessionID
		d.sendKeys(ctx, id, keys, false, func() { d.Sessions.Interrupted(id, time.Now()) })
	default:
		return false
	}
	return true
}

func (d *Daemon) flushSessions() {
	if d.Sessions == nil {
		return
	}
	if err := d.Sessions.Save(); err != nil {
		utils.Infof("agent: writing sessions.json: %v\n", err)
	}
}

func (d *Daemon) startSessionTracking() {
	if d.Sessions == nil {
		return
	}
	d.Sessions.OnTransition = d.onSessionTransition
	if d.Ledger == nil {
		d.Ledger = usage.OpenLedger(d.Dir)
	}
	d.Sessions.Load()
	d.rescan()
	d.syncWindows()
	d.sampleAccounts(time.Now())
	d.flushSessions()
}

func (d *Daemon) configDirs() []string {
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
	if d.Sessions != nil {
		for _, s := range d.Sessions.Sessions() {
			add(s.ConfigDir)
		}
	}
	return dirs
}

func (d *Daemon) backfillLedger(ctx context.Context) {
	if d.Ledger == nil || !d.Ledger.NeedsBackfill() {
		return
	}
	opened := time.Now()
	dirs := d.configDirs()
	d.swaps.Add(1)
	go func() {
		defer d.swaps.Done()
		n := d.Ledger.Backfill(ctx, dirs, opened)
		if err := d.Ledger.Flush(); err != nil {
			utils.Infof("agent: days ledger: %v\n", err)
			return
		}
		if n > 0 {
			utils.Infof("agent: counted %d transcripts into the day ledger\n", n)
		}
	}()
}

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
	if to == sessions.StatusDone && from == sessions.StatusWorking {
		d.swaps.Add(1)
		go func() {
			defer d.swaps.Done()
			d.compactIfFull(s)
			d.onMainMoved(s)
		}()
		d.gateDone(s)
	}
	if to == sessions.StatusNeedsInput && s.Pending != nil && d.allowsByPolicy(s) {
		go d.autoAllow(s)
		return
	}
	if to == sessions.StatusNeedsInput && s.Pending != nil && d.Push != nil && MutedUntil(d.Dir).IsZero() {
		body := "permission: " + s.Pending.Tool
		if s.Pending.Subject != "" {
			body += " " + s.Pending.Subject
		}
		data := map[string]string{"session": s.ID, "tool": s.Pending.Tool, "workspace": s.Label, "needs": "1"}
		if s.Pending.Risky() {
			data["risky"] = "1"
		}
		if s.Pending.Risk != "" {
			data["risk"] = s.Pending.Risk
		}
		go d.Push(push.Message{Title: notifyTitlePrefix + label, Body: body, Category: "permission", Data: data, Thread: s.ID})
	}
	d.attentionMu.Lock()
	if d.limitWatch == nil {
		d.limitWatch = map[string]bool{}
	}
	rested := ""
	resumed := false
	switch {
	case from == sessions.StatusLimited && to == sessions.StatusWorking:
		d.limitWatch[s.ID] = true
		resumed = !d.liftTold[s.ID] && s.ResumedBy != "person"
		delete(d.liftTold, s.ID)
		d.stopLiftClock(s.ID)
	case to == sessions.StatusLimited:
		delete(d.limitWatch, s.ID)
		delete(d.liftTold, s.ID)
		d.scheduleLiftClock(s, label, now)
	case d.limitWatch[s.ID] && (to == sessions.StatusDone || to == sessions.StatusNeedsInput):
		delete(d.limitWatch, s.ID)
		rested = "done"
		if to == sessions.StatusNeedsInput {
			rested = "needs you"
		}
	}
	d.attentionMu.Unlock()
	if resumed {
		id := s.ID
		grace := d.LiftGrace
		if grace == 0 {
			grace = 20 * time.Second
		}
		time.AfterFunc(grace, func() {
			d.attentionMu.Lock()
			still := d.limitWatch[id]
			recent := !d.liftRang[id].IsZero() && time.Since(d.liftRang[id]) < liftEpisode
			if still && !recent {
				if d.liftRang == nil {
					d.liftRang = map[string]time.Time{}
				}
				d.liftRang[id] = time.Now()
			}
			d.attentionMu.Unlock()
			if !still || recent {
				return
			}
			d.notifySession(notifyTitlePrefix+label, d.liftWord(s), s)
		})
	}
	if rested != "" {
		go d.notifySession(notifyTitlePrefix+label, "the turn resumed after the limit"+d.accountWord(s)+" is "+rested, s)
	}
}

func (d *Daemon) accountWord(s sessions.Session) string {
	parts := []string{}
	if s.Agent != "" {
		parts = append(parts, s.Agent)
	}
	if s.Profile != "" && (s.Profile != "default" || len(d.configDirs()) > 1) {
		parts = append(parts, s.Profile)
	}
	if len(parts) == 0 {
		return ""
	}
	return " on " + strings.Join(parts, " · ")
}

func (d *Daemon) sampleAccounts(now time.Time) {
	if d.Sessions == nil {
		return
	}
	dirs := d.configDirs()
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

func (d *Daemon) sendToSession(ctx context.Context, ref, text string, enter bool) {
	d.sendKeys(ctx, ref, text, enter, nil)
}

func (d *Daemon) sendKeys(ctx context.Context, ref, text string, enter bool, delivered func()) {
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
		} else if delivered != nil {
			delivered()
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
	case sessions.HostTmux:
		if d.TypeText != nil {
			return d.TypeText(ctx, t, text, enter)
		}
		return typeIntoTmux(ctx, t, text, enter)
	}
	return fmt.Errorf("%s: nowhere to type", t.Label)
}

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
		d.Sessions.SetMuted(MutedUntil(d.Dir))
		if !now.Before(nextSweep) {
			nextSweep = now.Add(sweepInterval)
			d.Sessions.Sweep(now)
			if err := d.Ledger.Flush(); err != nil {
				utils.Infof("agent: days ledger: %v\n", err)
			}
			d.sampleAccounts(now)
			d.autoContinue(ctx, now)
			d.autoCarry(ctx, now)
			d.checkDrift(now)
			d.runRoutines(ctx, now)
			d.pruneWorktrees(now)
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

func (d *Daemon) syncWindows() {
	if d.Sessions == nil {
		return
	}
	d.Sessions.SetWindows(sessions.LoadWindows(d.Dir, d.alive))
}

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

func raiseWindow(ctx context.Context, t sessions.FocusTarget) error {
	switch t.Kind {
	case sessions.HostVSCodeTerminal, sessions.HostVSCodePanel:
		return raiseEditor(ctx, t)
	case sessions.HostITerm, sessions.HostTerminalApp:
		if runtime.GOOS == "darwin" {
			return raiseTerminal(ctx, t)
		}
	case sessions.HostTmux:
		return raiseTmuxPane(ctx, t)
	}
	return fmt.Errorf("no window known for a %s session on %s", t.Kind, runtime.GOOS)
}

func raiseEditor(ctx context.Context, t sessions.FocusTarget) error {
	if t.App == "" {
		return errors.New("which editor is unknown — install the corgi VS Code extension, or reopen the terminal")
	}
	switch runtime.GOOS {
	case "darwin":
		args := []string{"-a", t.App}
		if t.Folder != "" {
			args = append(args, t.Folder)
		}
		return run(ctx, "open", args...)
	case "linux":
		if t.Folder == "" {
			return errors.New("no connected window to raise — install the corgi VS Code extension")
		}
		return run(ctx, editorCLI(t.App), "--reuse-window", t.Folder)
	}
	return fmt.Errorf("no window known for a %s session on %s", t.Kind, runtime.GOOS)
}

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

func newSessionCommand() string { return NewSessionCommand() }

func NewSessionCommand(args ...string) string { return NewSessionCommandFor("", args...) }

// NewSessionCommandFor opens the named harness ("" leaves it to the
// workspace: corgi agent claude picks its first installed agent).
func NewSessionCommandFor(agent string, args ...string) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "corgi"
	}
	sub := "claude"
	if a := strings.ToLower(strings.TrimSpace(agent)); a != "" && harness.Known(a) {
		sub = a
	}
	parts := []string{shellQuote(exe), "agent", sub}
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

var resetClock = func(detail string, now time.Time) (time.Time, bool) {
	m := resetClockText.FindStringSubmatch(detail)
	if m == nil {
		return time.Time{}, false
	}
	loc := now.Location()
	if m[2] != "" {
		if l, err := time.LoadLocation(m[2]); err == nil {
			loc = l
		}
	}
	clock := strings.ToUpper(strings.ReplaceAll(m[1], " ", ""))
	var at time.Time
	var err error
	for _, layout := range []string{"3:04PM", "3PM", "15:04"} {
		if at, err = time.ParseInLocation(layout, clock, loc); err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}, false
	}
	local := now.In(loc)
	at = time.Date(local.Year(), local.Month(), local.Day(), at.Hour(), at.Minute(), 0, 0, loc)
	if !at.After(now) {
		at = at.Add(24 * time.Hour)
	}
	return at, true
}

var resetClockText = regexp.MustCompile(`(?i)resets?\s+(?:at\s+)?(\d{1,2}(?::\d{2})?\s*(?:am|pm)?)(?:\s*\(([A-Za-z_]+/[A-Za-z_]+)\))?`)

func (d *Daemon) scheduleLiftClock(s sessions.Session, label string, now time.Time) {
	d.stopLiftClock(s.ID)
	at, ok := resetClock(s.Detail, now)
	if !ok || at.Sub(now) > 24*time.Hour {
		return
	}
	if d.liftDue == nil {
		d.liftDue = map[string]*time.Timer{}
	}
	if d.liftTold == nil {
		d.liftTold = map[string]bool{}
	}
	id := s.ID
	d.liftDue[id] = time.AfterFunc(at.Sub(now), func() {
		d.attentionMu.Lock()
		delete(d.liftDue, id)
		if d.limitWatch[id] || d.liftTold[id] {
			d.attentionMu.Unlock()
			return
		}
		d.liftTold[id] = true
		if d.liftRang == nil {
			d.liftRang = map[string]time.Time{}
		}
		d.liftRang[id] = time.Now()
		d.attentionMu.Unlock()
		d.notifySession(notifyTitlePrefix+label, d.liftWord(s), s)
	})
}

func (d *Daemon) liftWord(s sessions.Session) string {
	line := "limit lifted" + d.accountWord(s) + " — back to work"
	if s.Model != "" {
		line += " on " + sessions.ModelLabel(s.Model)
	}
	return line
}

func (d *Daemon) stopLiftClock(id string) {
	if t, ok := d.liftDue[id]; ok {
		t.Stop()
		delete(d.liftDue, id)
	}
}
