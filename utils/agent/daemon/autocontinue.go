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

var jitter = func(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(limit)))
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
		if d.resumeIsDue(s, now) {
			d.resumeSession(ctx, s)
		}
	}
}

// resumeIsDue says the session's clock ran out and the numbers agree. A
// session with no plan yet is given one here; one that still reads as
// spent is told to look again in a few minutes.
func (d *Daemon) resumeIsDue(s sessions.Session, now time.Time) bool {
	if s.ResumeAt.IsZero() {
		if at := resumeTime(s, now); !at.IsZero() {
			d.Sessions.PlanResume(s.ID, at)
		}
		return false
	}
	if now.Before(s.ResumeAt) {
		return false
	}
	if s.Limit == sessions.LimitQuota && quotaStillSpent(s.ConfigDir, now) {
		// The clock said yes, the numbers say no: the reset has not
		// landed in the cache yet, or the week is what is spent. Look
		// again in a few minutes rather than type into a wall.
		d.Sessions.PlanResume(s.ID, now.Add(5*time.Minute))
		return false
	}
	return s.Resumes < maxResumes(s.Limit)
}

// resumeSession types "continue" into a session whose limit should be over.
func (d *Daemon) resumeSession(ctx context.Context, s sessions.Session) {
	if target, err := d.Sessions.Focus(s.ID); err != nil || target.Kind == sessions.HostVSCodePanel {
		// Nowhere to type: the panel takes text only from the keyboard.
		// Leave the plan cleared so the key stops promising a continue.
		d.Sessions.PlanResume(s.ID, time.Time{})
		if err == nil {
			utils.Infof("agent: %s: limit should be over, but it runs in the Claude Code panel — continue it by hand\n", s.Display)
		}
		return
	}
	if _, ok := d.Sessions.MarkResumed(s.ID); !ok {
		return
	}
	utils.Infof("agent: %s: continuing after %s limit (try %d)\n", s.Display, s.Limit, s.Resumes+1)
	d.sendToSession(ctx, s.ID, "continue", true)
	go d.notifyAttention(notifyTitlePrefix+label(s), "limit should be over — continued for you", s.Folder)
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
		return overloadResumeTime(s.Resumes, now)
	case sessions.LimitQuota:
		return quotaResumeTime(s.ConfigDir, now)
	}
	return time.Time{}
}

func overloadResumeTime(resumes int, now time.Time) time.Time {
	wait := overloadBackoff << uint(resumes)
	if wait > overloadBackoffM {
		wait = overloadBackoffM
	}
	return now.Add(wait + jitter(resumeJitter))
}

func quotaResumeTime(configDir string, now time.Time) time.Time {
	l, ok := readLimits(configDir)
	if !ok {
		return time.Time{}
	}
	at := earliestSpentReset(l)
	if at.IsZero() {
		// Nothing reads as spent but the session said limit: a
		// session-credit cap, or a cache older than the limit. No
		// reset this side knows means no plan — typing into it only
		// makes the session say no again.
		return time.Time{}
	}
	if at.Before(now) {
		at = now
	}
	return at.Add(resumeGrace + jitter(resumeJitter))
}

// earliestSpentReset is the soonest reset among the windows that read as
// full, zero when none does.
func earliestSpentReset(l usage.Limits) time.Time {
	var at time.Time
	for _, w := range []usage.Window{l.FiveHour, l.SevenDay} {
		if w.Percent < quotaStillFull || w.ResetsAt.IsZero() {
			continue
		}
		if at.IsZero() || w.ResetsAt.Before(at) {
			at = w.ResetsAt
		}
	}
	return at
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

// autoCarry is the other way past a limit: with the workspace's autoCarry
// on, a session that hit its quota is carried to another of its accounts
// that still has budget — once per limit, and never when no account has
// any. Off by default: it opens a terminal that is yours.
func (d *Daemon) autoCarry(ctx context.Context, now time.Time) {
	if d.Carry == nil || d.Policy == nil || d.Sessions == nil {
		return
	}
	if d.carried == nil {
		d.carried = map[string]time.Time{}
	}
	for _, s := range d.Sessions.Sessions() {
		if s.Status != sessions.StatusLimited || s.Limit != sessions.LimitQuota {
			delete(d.carried, s.ID)
			continue
		}
		if at, done := d.carried[s.ID]; done && at.Equal(s.StatusSince) {
			continue
		}
		if !d.Policy(s).AutoCarry {
			continue
		}
		d.carried[s.ID] = s.StatusSince
		profile, err := d.Carry(s)
		switch {
		case err != nil:
			utils.Infof("agent: %s: could not carry: %v\n", s.Display, err)
		case profile == "":
			utils.Infof("agent: %s hit its limit; no other account has budget\n", s.Display)
		default:
			utils.Infof("agent: carried %s to %s after its limit\n", s.Display, profile)
			go d.notifyAttentionAt("corgi agent", "carried "+s.Display+" to "+profile+": the account hit its limit", "", "")
		}
	}
}
