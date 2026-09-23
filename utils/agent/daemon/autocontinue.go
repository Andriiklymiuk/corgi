package daemon

import (
	"context"
	"math/rand"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

const (
	resumeGrace      = 30 * time.Second
	resumeJitter     = 60 * time.Second
	overloadBackoff  = 2 * time.Minute
	overloadBackoffM = 15 * time.Minute
	maxQuotaResumes  = 3
	maxOverloadTries = 5
	quotaStillFull   = 95
)

var jitter = func(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(limit)))
}

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
		d.Sessions.PlanResume(s.ID, now.Add(5*time.Minute))
		return false
	}
	return s.Resumes < maxResumes(s.Limit)
}

func (d *Daemon) resumeSession(ctx context.Context, s sessions.Session) {
	if target, err := d.Sessions.Focus(s.ID); err != nil || target.Kind == sessions.HostVSCodePanel {
		d.Sessions.PlanResume(s.ID, time.Time{})
		if err == nil {
			utils.Infof("agent: %s: limit should be over, but it runs in the Claude Code panel - continue it by hand\n", s.Display)
		}
		return
	}
	if _, ok := d.Sessions.MarkResumed(s.ID); !ok {
		return
	}
	utils.Infof("agent: %s: continuing after %s limit (try %d)\n", s.Display, s.Limit, s.Resumes+1)
	d.sendToSession(ctx, s.ID, "continue", true)
	go d.notifySession(notifyTitlePrefix+label(s), "limit should be over - continued for you", s)
}

func maxResumes(kind sessions.LimitKind) int {
	if kind == sessions.LimitOverload {
		return maxOverloadTries
	}
	return maxQuotaResumes
}

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
		return time.Time{}
	}
	if at.Before(now) {
		at = now
	}
	return at.Add(resumeGrace + jitter(resumeJitter))
}

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
