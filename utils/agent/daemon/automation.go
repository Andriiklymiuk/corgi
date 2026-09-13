package daemon

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/events"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// Two things the daemon does on its own once a workspace says so, read
// from the config every time they are asked — a switch flipped on the
// phone takes on the next round, no restart:
//
//   handOver   a review comment, an asked-for review or a red build on a
//              branch a session already owns is typed into that session as
//              the next message, and the row says "handed to api·auth".
//   autoMerge  a pull request of mine that the forge calls ready — checks
//              green, approved — is merged, and the inbox says so.
//   autoAllow  a permission prompt for a tool that only reads is answered
//              by the daemon, and the session's row counts it.
//   doneWhen   a session that stops with changes on its branch has the
//              workspace's own checks run; a red one is typed back as the
//              next message, so "done" means the tests say so.

// Policy is the part of a workspace's watch config that concerns a live
// session rather than the tracker.
type Policy struct {
	AutoAllow string
	DoneWhen  []string
}

// automation is the two switches for a workspace, as the config says now.
func (d *Daemon) automation(spec WatchSpec) (handOver, autoMerge bool) {
	dir := spec.AgentDir
	if dir == "" {
		dir = d.Dir
	}
	user, err := config.LoadUser(filepath.Join(dir, "config.yml"))
	if err != nil || user == nil {
		return false, false
	}
	repo, _ := config.LoadRepo(spec.Dir)
	wc := config.Resolve(spec.Workspace, repo, user).Watch
	if wc == nil {
		return false, false
	}
	return wc.HandOver, wc.AutoMerge
}

// sessionOnPull is the live session whose pull request this is.
func (d *Daemon) sessionOnPull(link string) (sessions.Session, bool) {
	if link == "" || d.Sessions == nil {
		return sessions.Session{}, false
	}
	for _, s := range d.Sessions.Sessions() {
		if s.Status == sessions.StatusGone || s.Status == sessions.StatusStale || s.PR == "" {
			continue
		}
		if strings.HasPrefix(s.PR, link) {
			return s, true
		}
	}
	return sessions.Session{}, false
}

// handOverEvent types a PR-kind event into the session on that pull
// request, when the workspace asked for it. Reports whether it did.
func (d *Daemon) handOverEvent(ctx context.Context, spec WatchSpec, e watch.Event) bool {
	on, _ := d.automation(spec)
	if !on {
		return false
	}
	line := watch.HandoverLine(e)
	if line == "" {
		return false
	}
	s, ok := d.sessionOnPull(watch.PullLinkOf(e))
	if !ok {
		return false
	}
	hands := watch.LoadHands(d.Dir)
	if _, done := hands.Get(e.Key); done {
		return true
	}
	label := s.Display
	if label == "" {
		label = s.Label
	}
	_ = hands.Set(e.Key, watch.Hand{At: time.Now(), To: s.ID, Label: label, By: "daemon"})
	d.sendToSession(ctx, s.ID, line, true)
	if d.Events != nil {
		d.Events.Append(spec.Workspace, events.Event{At: time.Now().UTC(), Kind: "handover", Reason: string(e.Kind) + " " + e.Ref + " → " + label, URL: e.URL})
	}
	utils.Infof("agent: handed %s to %s\n", e.Ref, label)
	return true
}

// pullChanged runs what a change in a pull request's standing asks for:
// a merge when it is ready and the workspace merges on its own; a word to
// the session on it when the checks went red.
func (d *Daemon) pullChanged(ctx context.Context, spec WatchSpec, ref, link string, was, now watch.PullStatus, known bool) {
	handOver, autoMerge := d.automation(spec)
	if handOver && link != "" && now.Checks == "failing" && (!known || was.Checks != "failing") {
		if s, ok := d.sessionOnPull(link); ok {
			hands := watch.LoadHands(d.Dir)
			key := "checks:" + ref + ":" + now.At.Format(time.RFC3339)
			label := s.Display
			if label == "" {
				label = s.Label
			}
			_ = hands.Set(key, watch.Hand{At: time.Now(), To: s.ID, Label: label, By: "daemon"})
			d.sendToSession(ctx, s.ID, "The checks went red on "+link+". Read the failing job, fix it on this branch, run the tests, and push.", true)
			utils.Infof("agent: red checks on %s handed to %s\n", ref, label)
		}
	}
	if autoMerge && link != "" && now.Ready() && d.MergePull != nil {
		if err := d.MergePull(ctx, spec.Workspace, link); err != nil {
			utils.Infof("agent: auto-merge %s: %v\n", ref, err)
			go d.notifyAttentionAt("corgi agent · "+spec.Workspace, "could not merge "+link+": "+err.Error(), spec.Workspace, link)
			return
		}
		_ = watch.LoadPullLog(d.Dir).Set(ref, watch.PullStatus{State: "merged", Checks: now.Checks, Review: now.Review, At: time.Now()})
		if d.Events != nil {
			d.Events.Append(spec.Workspace, events.Event{At: time.Now().UTC(), Kind: "merged", Reason: "merged " + ref + " — checks ✓, approved", URL: link})
		}
		go d.notifyAttentionAt("corgi agent · "+spec.Workspace, "merged "+link+" — checks ✓ · approved", spec.Workspace, link)
	}
}

// allowsByPolicy says whether the prompt a session just raised is one the
// workspace's policy answers: the tool only reads, the policy is reads,
// and the session sits in iTerm2 — the one host that takes keys without
// its window coming forward, so nothing on the desk moves. A Bash command
// is never a read here, whatever it says; elsewhere the prompt rings as
// it always did.
func (d *Daemon) allowsByPolicy(s sessions.Session) bool {
	if d.Policy == nil || s.Pending == nil || s.Pending.Risk != config.AutoAllowReads || s.Pending.Tool == "Bash" {
		return false
	}
	if s.Host.Kind != sessions.HostITerm {
		return false
	}
	return d.Policy(s).AutoAllow == config.AutoAllowReads
}

// autoAllow presses Enter into the session for the prompt it raised, a
// beat after the prompt was drawn, and counts it on the row. The prompt
// may have been answered at the keyboard meanwhile; then there is nothing
// pending and nothing is typed.
func (d *Daemon) autoAllow(s sessions.Session) {
	d.swaps.Add(1)
	defer d.swaps.Done()
	time.Sleep(autoAllowDelay)
	keys, err := d.Sessions.PendingAnswer(s.ID, "allow")
	if err != nil {
		return
	}
	target, err := d.Sessions.Focus(s.ID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), focusBudget)
	defer cancel()
	if err := d.deliverText(ctx, target, keys, false); err != nil {
		utils.Infof("agent: allow %s for %s: %v\n", s.Pending.Tool, s.Label, err)
		return
	}
	label := s.Display
	if label == "" {
		label = s.Label
	}
	utils.Infof("agent: allowed %s %s for %s — the workspace's reads policy\n", s.Pending.Tool, s.Pending.Subject, label)
	d.Sessions.AutoAllowed(s.ID)
	d.flushSessions()
}

// autoAllowDelay is how long after the prompt appears the Enter lands:
// enough for Claude Code to draw it, short enough that nobody notices.
var autoAllowDelay = 400 * time.Millisecond

// gateDone runs the workspace's done-when commands for a session that
// just stopped with work on its branch. All green: the row says so. One
// red: its tail is typed into the session as the next message and the
// session is working again — up to gateTries times in a row, after which
// the daemon rings a person instead of arguing with a model.
func (d *Daemon) gateDone(s sessions.Session) {
	if d.Policy == nil || s.Cwd == "" || d.Sessions == nil {
		return
	}
	cmds := d.Policy(s).DoneWhen
	if len(cmds) == 0 || !hasWork(s) || s.Detail == "interrupted" {
		return
	}
	d.gateMu.Lock()
	if d.gating == nil {
		d.gating = map[string]bool{}
	}
	if d.gating[s.ID] {
		d.gateMu.Unlock()
		return
	}
	d.gating[s.ID] = true
	d.gateMu.Unlock()
	d.runs.Add(1)
	go func() {
		defer d.runs.Done()
		defer func() {
			d.gateMu.Lock()
			delete(d.gating, s.ID)
			d.gateMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), gateBudget)
		defer cancel()
		label := s.Display
		if label == "" {
			label = s.Label
		}
		for _, cmd := range cmds {
			out, err := d.shell(ctx, s.Cwd, cmd)
			if err == nil {
				continue
			}
			fails := d.Sessions.SetGate(s.ID, false, cmd, time.Now())
			d.flushSessions()
			utils.Infof("agent: %s stopped, but %q failed (%d in a row)\n", label, cmd, fails)
			if fails > gateTries {
				go d.notifyAttentionAt("corgi agent · "+label, "not done: "+cmd+" still red after "+strconv.Itoa(fails)+" tries", s.Label, "")
				return
			}
			d.sendToSession(ctx, s.ID, gateMessage(cmd, string(out), err), true)
			return
		}
		d.Sessions.SetGate(s.ID, true, "", time.Now())
		d.flushSessions()
	}()
}

// hasWork says the session has something on its branch worth checking:
// the minute sweep saw changed files, or it ran tests itself.
func hasWork(s sessions.Session) bool {
	return (s.Changes != nil && s.Changes.Files > 0) || s.Tests != nil
}

func (d *Daemon) shell(ctx context.Context, dir, cmd string) ([]byte, error) {
	if d.Shell != nil {
		return d.Shell(ctx, dir, cmd)
	}
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Dir = dir
	return c.CombinedOutput()
}

// gateMessage is what the session reads next: the command, the last lines
// it printed, and what to do — the same words a reviewer would use.
func gateMessage(cmd, out string, err error) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > gateTail {
		lines = lines[len(lines)-gateTail:]
	}
	tail := strings.TrimSpace(strings.Join(lines, "\n"))
	if tail == "" {
		tail = err.Error()
	}
	return "Not done yet: `" + cmd + "` failed after you stopped.\n\n" + tail + "\n\nFix it, run `" + cmd + "` again, and stop when it is green."
}

const (
	gateBudget = 10 * time.Minute
	gateTries  = 3
	gateTail   = 12
)
