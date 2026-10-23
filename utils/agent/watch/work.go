package watch

import (
	"fmt"
	"math"
	"sort"
	"time"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

const (
	spendDays        = 14
	stalledMerge     = 24 * time.Hour
	stalledChanges   = 48 * time.Hour
	stalledRed       = 24 * time.Hour
	stalledShown     = 8
	leftShown        = 20
	leftDoneShownFor = 24 * time.Hour
)

func BoardWork(records []FixRecord, ticked map[string]LeftTick, pulls map[string]PullStatus, now time.Time) sessions.Work {
	var w sessions.Work
	rows := LeftRows(records, ticked, now.Add(-leftDoneShownFor), now)
	if len(rows) > leftShown {
		rows = rows[:leftShown]
	}
	for _, r := range rows {
		w.Left = append(w.Left, sessions.LeftItem{ID: r.ID, Workspace: r.Workspace, Ref: r.Ref, Title: r.Title, URL: r.URL,
			Text: r.Text, Kind: string(r.Kind), At: r.At, DoneAt: r.DoneAt, DoneBy: r.DoneBy})
	}
	w.Stalled = StalledPulls(pulls, now)
	if spend := RunSpendOf(records, pulls, now); spend.WeekRuns > 0 {
		w.Spend = &spend
	}
	if c := SelfCheckOf(records, ticked, now, 7); c.Runs > 0 {
		w.Check = &sessions.RunCheck{Headline: c.Headline(), Days: c.Days, Runs: c.Runs, Lines: c.Lines}
	}
	return w
}

func StalledPulls(pulls map[string]PullStatus, now time.Time) []sessions.StalledPull {
	var out []sessions.StalledPull
	for ref, p := range pulls {
		if !p.Mine || p.State != "open" || p.Link == "" || p.Since.IsZero() {
			continue
		}
		idle := now.Sub(p.Since)
		s := sessions.StalledPull{Workspace: p.Workspace, Ref: ref, Link: p.Link, Since: p.Since}
		switch {
		case p.Ready() && p.Checks == "passing" && idle >= stalledMerge:
			s.Action, s.Why = "merge", "approved, checks ✓, not merged for "+daysWord(idle)
		case p.Review == "changes" && idle >= stalledChanges:
			s.Action, s.Why = "work", "changes requested "+daysWord(idle)+" ago, nothing since"
			s.Prompt = "The review on " + p.Link + " asked for changes " + daysWord(idle) + " ago and nothing moved since. Read the open threads with `corgi agent watch pr threads " + p.Link + "`, fix each valid one, reply on each thread, run the tests, and push."
		case p.Checks == "failing" && idle >= stalledRed:
			s.Action, s.Why = "work", "checks ✗ for "+daysWord(idle)
			s.Prompt = "The checks on " + p.Link + " have been red for " + daysWord(idle) + ". Read the failing job's log, fix the cause on that branch, run it locally, and push."
		default:
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Since.Before(out[j].Since) })
	if len(out) > stalledShown {
		out = out[:stalledShown]
	}
	return out
}

func daysWord(d time.Duration) string {
	days := int(math.Round(d.Hours() / 24))
	switch {
	case d < 36*time.Hour:
		return "a day"
	default:
		return fmt.Sprintf("%d days", days)
	}
}

func RunSpendOf(records []FixRecord, pulls map[string]PullStatus, now time.Time) sessions.RunSpend {
	var s sessions.RunSpend
	weekAgo := now.Add(-7 * 24 * time.Hour)
	first := now.AddDate(0, 0, -(spendDays - 1))
	dayIndex := map[string]int{}
	for i := 0; i < spendDays; i++ {
		day := first.AddDate(0, 0, i).Format("2006-01-02")
		dayIndex[day] = i
		s.Days = append(s.Days, sessions.DaySpend{Day: day})
	}
	merged := map[string]bool{}
	for _, r := range records {
		if !countedRun(r) || !r.Done() {
			continue
		}
		if i, ok := dayIndex[r.StartedAt.In(now.Location()).Format("2006-01-02")]; ok {
			s.Days[i].USD += r.CostUSD
			s.Days[i].Runs++
		}
		if r.StartedAt.Before(weekAgo) {
			continue
		}
		s.WeekRuns++
		s.WeekUSD += r.CostUSD
		if r.Error != "" && !r.Forgiven {
			s.FailedRuns++
		}
		if r.Said == "nothing" && Kind(r.Kind) != KindRoutine && Kind(r.Kind) != KindReviewRequested {
			s.NothingRuns++
			s.NothingUSD += r.CostUSD
		}
		if len(r.Gaps) > 0 {
			s.SkippedChecks++
		}
		for _, link := range append([]string{r.URL}, r.PRs...) {
			if p, ok := pulls[PullRef(link)]; ok && p.State == "merged" {
				merged[PullRef(link)] = true
			}
		}
	}
	s.MergedPulls = len(merged)
	if s.MergedPulls > 0 {
		s.PerMergedPull = math.Round(s.WeekUSD/float64(s.MergedPulls)*100) / 100
	}
	s.WeekUSD = math.Round(s.WeekUSD*100) / 100
	s.NothingUSD = math.Round(s.NothingUSD*100) / 100
	for i := range s.Days {
		s.Days[i].USD = math.Round(s.Days[i].USD*100) / 100
	}
	return s
}
