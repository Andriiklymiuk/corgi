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

type selfCheckTally struct {
	runs       int
	gaps       map[Gap]int
	env        map[string]int
	failed     int
	nothing    int
	nothingUSD float64
}

func (t *selfCheckTally) add(r FixRecord) {
	if strings.HasPrefix(r.Error, EnvironmentPrefix) {
		t.env[strings.TrimPrefix(r.Error, EnvironmentPrefix)]++
		return
	}
	if !countedRun(r) || !r.Done() {
		return
	}
	t.runs++
	for _, g := range r.Gaps {
		t.gaps[g]++
	}
	if r.Error != "" && !r.Forgiven {
		t.failed++
	}
	if r.Said == "nothing" && Kind(r.Kind) != KindRoutine && Kind(r.Kind) != KindReviewRequested {
		t.nothing++
		t.nothingUSD += r.CostUSD
	}
}

func (t selfCheckTally) envLine() string {
	if len(t.env) == 0 {
		return ""
	}
	var parts []string
	for cause, n := range t.env {
		parts = append(parts, fmt.Sprintf("%s %d", cause, n))
	}
	sort.Strings(parts)
	return "runs that never started because of the machine: " + strings.Join(parts, ", ") + " - they were tried again on their own"
}

func SelfCheckOf(records []FixRecord, ticked map[string]LeftTick, now time.Time, days int) SelfCheck {
	since := now.AddDate(0, 0, -days)
	t := selfCheckTally{gaps: map[Gap]int{}, env: map[string]int{}}
	for _, r := range records {
		if !r.StartedAt.Before(since) {
			t.add(r)
		}
	}
	out := SelfCheck{Days: days, Runs: t.runs}
	for _, g := range sortedGaps(t.gaps) {
		out.Lines = append(out.Lines, fmt.Sprintf("%d %s skipped a check: %s - %s", t.gaps[g], runsWord(t.gaps[g]), g, gapFix[g]))
	}
	if line := t.envLine(); line != "" {
		out.Lines = append(out.Lines, line)
	}
	if t.failed > 0 {
		out.Lines = append(out.Lines, fmt.Sprintf("%d %s failed on the work itself - corgi agent scorecard --days %d", t.failed, runsWord(t.failed), days))
	}
	if t.nothing > 0 {
		out.Lines = append(out.Lines, fmt.Sprintf("$%.2f on %d %s that found nothing to do - corgi agent scorecard names the kind worth turning off", t.nothingUSD, t.nothing, runsWord(t.nothing)))
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
