package daemon

import (
	"context"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/lessons"
	"andriiklymiuk/corgi/utils/agent/timers"
)

const timerCheckInterval = 15 * time.Second

func (d *Daemon) fireDueTimers(ctx context.Context, now time.Time) {
	due, err := timers.TakeDue(d.Dir, now)
	if err != nil || len(due) == 0 {
		return
	}
	for _, t := range due {
		late := now.Sub(t.At)
		if t.Session != "" {
			utils.Infof("agent: timer %s → %s: %s\n", t.ID, t.Session, t.Text)
			d.sendToSession(ctx, t.Session, t.Text, true)
			continue
		}
		body := t.Text
		if late > 10*time.Minute {
			body += " (the laptop was asleep when it was due)"
		}
		d.notifyAttentionKey(notifyTitlePrefix+"reminder", body, t.Workspace, "", "timer:"+t.ID)
	}
}

const dreamEvery = 7 * 24 * time.Hour

func (d *Daemon) dreamIfDue(now time.Time) {
	if !lessons.DreamDue(d.Dir, now, dreamEvery) {
		return
	}
	reports, err := lessons.DreamAll(d.Dir, now, lessons.DefaultDream)
	if err != nil {
		utils.Infof("agent: dreaming: %v\n", err)
		return
	}
	put := 0
	for _, r := range reports {
		put += len(r.Dropped)
	}
	if put > 0 {
		utils.Infof("agent: dreamed over the lessons: put away %d (lessons/*.dreamed.md)\n", put)
	}
}
