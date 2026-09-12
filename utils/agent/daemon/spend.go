package daemon

import (
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

// spendMark is how far into a session's transcript the daemon has summed,
// and the total so far. Kept here, not on the session: a daemon that
// restarts sums from the top once and the board's number stays right.
type spendMark struct {
	offset int64
	total  usage.Totals
}

// transcriptOf is a seam: where a session's transcript is.
var transcriptOf = usage.TranscriptPath

// checkSpend adds what each live session wrote since the last sweep to
// its total, and rings once when a session passes its budget.
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
		mark := d.spent[s.ID]
		delta, offset := usage.SumFrom(path, mark.offset)
		if offset == mark.offset && s.Spend != nil {
			continue
		}
		mark.total = mark.total.Plus(delta)
		mark.offset = offset
		d.spent[s.ID] = mark
		sp := sessions.Spend{Tokens: mark.total.Total(), Turns: int(mark.total.Turns), At: now}
		if _, crossed := d.Sessions.SetSpend(s.ID, sp, d.SessionCap); crossed {
			label := s.Display
			if label == "" {
				label = s.Label
			}
			limit := s.Cap
			if limit == 0 {
				limit = d.SessionCap
			}
			go d.notifyAttention("corgi agent · "+label, "over its budget: "+sessions.Tokens(sp.Tokens)+" of "+sessions.Tokens(limit)+" tokens", s.Folder)
		}
	}
	// A session that left the board takes its mark with it.
	for id := range d.spent {
		if !seen[id] {
			delete(d.spent, id)
		}
	}
}
