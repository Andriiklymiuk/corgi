package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

const (
	selfCheckDay  = time.Monday
	selfCheckHour = 9
	selfCheckDays = 7
)

func selfCheckWeek(now time.Time) string {
	y, w := now.ISOWeek()
	return strconv.Itoa(y) + "-W" + strconv.Itoa(w)
}

func (d *Daemon) weeklySelfCheck(now time.Time) {
	if d.watchState == nil || len(d.Watches) == 0 || now.Weekday() != selfCheckDay || now.Hour() < selfCheckHour {
		return
	}
	marker := filepath.Join(d.Dir, "watch", "selfcheck-week")
	week := selfCheckWeek(now)
	if data, err := os.ReadFile(marker); err == nil && strings.TrimSpace(string(data)) == week {
		return
	}
	_ = os.MkdirAll(filepath.Dir(marker), 0o700)
	_ = os.WriteFile(marker, []byte(week), 0o600)
	c := watch.SelfCheckOf(d.fixLog().Records(), watch.LoadLeftTicks(d.Dir).All(), now, selfCheckDays)
	if len(c.Lines) == 0 {
		return
	}
	body := c.Headline() + "\n- " + strings.Join(c.Lines, "\n- ")
	go d.notifyAttentionAt(notifyTitlePrefix+"self-check", body, "", "")
}
