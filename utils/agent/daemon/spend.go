package daemon

import (
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

type spendMark struct {
	offset int64
	total  usage.Totals
}

var transcriptOf = usage.TranscriptPath

func (d *Daemon) checkSpend(live []sessions.Session, now time.Time) {
	if d.spent == nil {
		d.spent = map[string]spendMark{}
	}
	seen := map[string]bool{}
	for _, s := range live {
		if s.Cwd == "" || sessions.Placeholder(s.ID) {
			continue
		}
		path := transcriptOf(s.ConfigDir, s.Cwd, s.ID)
		if path == "" {
			continue
		}
		seen[s.ID] = true
		d.addSpend(s, path, now)
	}
	for id := range d.spent {
		if !seen[id] {
			delete(d.spent, id)
		}
	}
}

func (d *Daemon) addSpend(s sessions.Session, path string, now time.Time) {
	mark := d.spent[s.ID]
	delta, offset := usage.SumFrom(path, mark.offset)
	if offset == mark.offset && s.Spend != nil {
		return
	}
	mark.total = mark.total.Plus(delta)
	mark.offset = offset
	d.spent[s.ID] = mark
	if n := delta.Total(); n > 0 && d.Ledger != nil {
		d.ringDayCap(s, n, now)
	}
	sp := sessions.Spend{Tokens: mark.total.Total(), Turns: int(mark.total.Turns), At: now}
	if _, crossed := d.Sessions.SetSpend(s.ID, sp, d.SessionCap); crossed {
		limit := s.Cap
		if limit == 0 {
			limit = d.SessionCap
		}
		go d.notifySession(notifyTitlePrefix+firstNonEmpty(s.Display, s.Label), "over its budget: "+sessions.Tokens(sp.Tokens)+" of "+sessions.Tokens(limit)+" tokens", s)
	}
}

func (d *Daemon) ringDayCap(s sessions.Session, n int64, now time.Time) {
	today := d.Ledger.AddTokens(s.Label, n, now)
	if dayCap := d.dayCapFor(s); dayCap > 0 && today >= dayCap && today-n < dayCap {
		go d.notifySession(notifyTitlePrefix+s.Label, "over its day budget: "+sessions.Tokens(today)+" of "+sessions.Tokens(dayCap)+" tokens today", s)
	}
}

func (d *Daemon) dayCapFor(s sessions.Session) int64 {
	if d.Policy == nil {
		return 0
	}
	return d.Policy(s).DayCap
}
