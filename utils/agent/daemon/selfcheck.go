package daemon

import (
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

const selfCheckDays = 7

// selfCheck is what kept going wrong in the unattended runs, for the board
// and corgi agent selfcheck. It never rings on its own: a weekly summary
// of skipped checks and idle spend is a thing to look up, not to be told.
func (d *Daemon) selfCheck(now time.Time) watch.SelfCheck {
	if d.watchState == nil {
		return watch.SelfCheck{}
	}
	return watch.SelfCheckOf(d.fixLog().Records(), watch.LoadLeftTicks(d.Dir).All(), now, selfCheckDays)
}
