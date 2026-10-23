package daemon

import (
	"os"
	"path/filepath"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func (d *Daemon) sampleWork(now time.Time) {
	if d.Sessions == nil {
		return
	}
	w := watch.BoardWork(d.fixLog().Records(), watch.LoadLeftTicks(d.Dir).All(), watch.LoadPullLog(d.Dir).All(), now)
	if d.Sessions.SetWork(w) {
		d.flushSessions()
	}
}

func (d *Daemon) backfillRunNotes() {
	if d.watchState == nil || d.watchState.Fixes == nil {
		return
	}
	logOf := func(key string) string {
		data, err := os.ReadFile(filepath.Join(d.Dir, "watch", "runs", safeName(key)+".log"))
		if err != nil {
			return ""
		}
		return string(data)
	}
	if n := d.watchState.Fixes.Backfill(logOf, watch.LoadPullLog(d.Dir).All(), time.Now()); n > 0 {
		utils.Infof("agent: read what %d earlier runs left for you and which checks they skipped\n", n)
	}
}
