package daemon

import (
	"context"
	"path/filepath"
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
