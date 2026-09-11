package daemon

import (
	"context"
	"math/rand"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

// A session that hit a limit is waiting on a clock, not a person. The
// daemon watches the clock so the editor does not have to be open: when a
// spent window resets, or a few minutes after an overload, it types
// "continue" into the session. Bounded — a limit that comes straight back
// is not fought — and only with autoContinue on, because it types into a
// terminal that is yours.

const (
	resumeGrace      = 30 * time.Second
	resumeJitter     = 60 * time.Second
	overloadBackoff  = 2 * time.Minute
	overloadBackoffM = 15 * time.Minute
	maxQuotaResumes  = 3
	maxOverloadTries = 5
	// quotaStillFull is the reading at which a window is not really back.
	quotaStillFull = 95
)

var jitter = func(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(max)))
}

// readLimits is a seam for tests; the daemon reads Claude's cached usage.
var readLimits = usage.ReadLimits

func (d *Daemon) autoContinue(ctx context.Context, now time.Time) {
	if !d.AutoContinue || d.Sessions == nil {
		return
	}
	for _, s := range d.Sessions.Sessions() {
		if s.Status != sessions.StatusLimited {
			continue
		}
		if s.ResumeAt.IsZero() {
			if at := resumeTime(s, now); !at.IsZero() {
				d.Sessions.PlanResume(s.ID, at)
			}
			continue
		}
		if now.Before(s.ResumeAt) {
			continue
		}
		if s.Limit == sessions.LimitQuota && quotaStillSpent(s.ConfigDir, now) {
			// The clock said yes, the numbers say no: the reset has not
			// landed in the cache yet, or the week is what is spent. Look
			// again in a few minutes rather than type into a wall.
			d.Sessions.PlanResume(s.ID, now.Add(5*time.Minute))
			continue
		}
		if s.Resumes >= maxResumes(s.Limit) {
			continue
		}
		if target, err := d.Sessions.Focus(s.ID); err != nil || target.Kind == sessions.HostVSCodePanel {
			// Nowhere to type: the panel takes text only from the keyboard.
			// Leave the plan cleared so the key stops promising a continue.
			d.Sessions.PlanResume(s.ID, time.Time{})
			if err == nil {
				utils.Infof("agent: %s: limit should be over, but it runs in the Claude Code panel — continue it by hand\n", s.Display)
			}
			continue
		}
		if _, ok := d.Sessions.MarkResumed(s.ID); !ok {
			continue
		}
		utils.Infof("agent: %s: continuing after %s limit (try %d)\n", s.Display, s.Limit, s.Resumes+1)
		d.sendToSession(ctx, s.ID, "continue", true)
		label := s.Display
		if label == "" {
			label = s.Label
		}
		go d.notifyAttention("corgi agent · "+label, "limit should be over — continued for you", s.Folder)
	}
}

func maxResumes(kind sessions.LimitKind) int {
	if kind == sessions.LimitOverload {
		return maxOverloadTries
	}
	return maxQuotaResumes
}

// resumeTime is when to try: for an overload a growing wait from now; for
// a quota the earliest reset of a window that is actually spent, from the
// account's cached numbers. No numbers, no plan — guessing at a reset time
// means typing into a session at the wrong moment.
func resumeTime(s sessions.Session, now time.Time) time.Time {
	switch s.Limit {
	case sessions.LimitOverload:
		wait := overloadBackoff << uint(s.Resumes)
		if wait > overloadBackoffM {
			wait = overloadBackoffM
		}
		return now.Add(wait + jitter(resumeJitter))
	case sessions.LimitQuota:
		l, ok := readLimits(s.ConfigDir)
		if !ok {
			return time.Time{}
		}
		var at time.Time
		for _, w := range []usage.Window{l.FiveHour, l.SevenDay} {
			if w.Percent < quotaStillFull || w.ResetsAt.IsZero() {
				continue
			}
			if at.IsZero() || w.ResetsAt.Before(at) {
				at = w.ResetsAt
			}
		}
		if at.IsZero() {
			// Nothing reads as spent but the session said limit: the
			// cache is older than the limit. The five-hour reset is the
			// honest default when it is known.
			at = l.FiveHour.ResetsAt
		}
		if at.IsZero() {
			return time.Time{}
		}
		if at.Before(now) {
			at = now
		}
		return at.Add(resumeGrace + jitter(resumeJitter))
	}
	return time.Time{}
}

// quotaStillSpent says the account's numbers still read as full: a window
// at or above the stop line whose reset is not yet behind us.
func quotaStillSpent(configDir string, now time.Time) bool {
	l, ok := readLimits(configDir)
	if !ok {
		return false
	}
	for _, w := range []usage.Window{l.FiveHour, l.SevenDay} {
		if w.Percent >= quotaStillFull && (w.ResetsAt.IsZero() || w.ResetsAt.After(now)) {
			return true
		}
	}
	return false
}
