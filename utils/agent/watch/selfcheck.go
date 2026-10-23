package watch

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type SelfCheck struct {
	Days  int      `json:"days"`
	Runs  int      `json:"runs"`
	Lines []string `json:"lines"`
}

var gapFix = map[Gap]string{
	GapNoDatabase: "isolated runs get a stack of their own now; a workspace whose corgi-compose.yml has no db_services cannot give them one",
	GapNoTool:     "a worktree gets the main checkout's packages when the lockfile matches; a different lockfile needs the install in the service's beforeStart",
	GapNoDevice:   "a device check needs a person - it is on the list left for you",
	GapNoService:  "a check against a live service needs a person or a staging token in the workspace's env",
	GapTestsSkip:  "corgi agent watch enable --done-when \"<test command>\" makes the checks part of every stop",
}

func SelfCheckOf(records []FixRecord, ticked map[string]LeftTick, now time.Time, days int) SelfCheck {
	since := now.AddDate(0, 0, -days)
	out := SelfCheck{Days: days}
	gaps := map[Gap]int{}
	env := map[string]int{}
	failed := 0
	nothingUSD, nothing := 0.0, 0
	for _, r := range records {
		if r.StartedAt.Before(since) {
			continue
		}
		if strings.HasPrefix(r.Error, EnvironmentPrefix) {
			env[strings.TrimPrefix(r.Error, EnvironmentPrefix)]++
			continue
		}
		if !countedRun(r) || !r.Done() {
			continue
		}
		out.Runs++
		for _, g := range r.Gaps {
			gaps[g]++
		}
		if r.Error != "" && !r.Forgiven {
			failed++
		}
		if r.Said == "nothing" && Kind(r.Kind) != KindRoutine && Kind(r.Kind) != KindReviewRequested {
			nothing++
			nothingUSD += r.CostUSD
		}
	}
	for _, g := range sortedGaps(gaps) {
		out.Lines = append(out.Lines, fmt.Sprintf("%d %s skipped a check: %s - %s", gaps[g], runsWord(gaps[g]), g, gapFix[g]))
	}
	if len(env) > 0 {
		var parts []string
		for cause, n := range env {
			parts = append(parts, fmt.Sprintf("%s %d", cause, n))
		}
		sort.Strings(parts)
		out.Lines = append(out.Lines, "runs that never started because of the machine: "+strings.Join(parts, ", ")+" - they were tried again on their own")
	}
	if failed > 0 {
		out.Lines = append(out.Lines, fmt.Sprintf("%d %s failed on the work itself - corgi agent scorecard --days %d", failed, runsWord(failed), days))
	}
	if nothing > 0 {
		out.Lines = append(out.Lines, fmt.Sprintf("$%.2f on %d %s that found nothing to do - corgi agent scorecard names the kind worth turning off", nothingUSD, nothing, runsWord(nothing)))
	}
	if open := OpenLeft(LeftRows(records, ticked, now, now)); len(open) > 0 {
		oldest := open[len(open)-1].At
		out.Lines = append(out.Lines, fmt.Sprintf("%d %s left for you still open, the oldest from %s - corgi agent left", len(open), itemsWord(len(open)), oldest.Format("2 Jan")))
	}
	return out
}

func (c SelfCheck) Headline() string {
	if len(c.Lines) == 0 {
		return fmt.Sprintf("%d unattended runs in %d days, nothing kept going wrong", c.Runs, c.Days)
	}
	return fmt.Sprintf("%d unattended runs in %d days: %d %s worth a look", c.Runs, c.Days, len(c.Lines), map[bool]string{true: "thing", false: "things"}[len(c.Lines) == 1])
}

func sortedGaps(m map[Gap]int) []Gap {
	out := make([]Gap, 0, len(m))
	for g := range m {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

func runsWord(n int) string {
	if n == 1 {
		return "run"
	}
	return "runs"
}

func itemsWord(n int) string {
	if n == 1 {
		return "item"
	}
	return "items"
}
