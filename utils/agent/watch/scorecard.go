package watch

import (
	"sort"
	"strings"
	"time"
)

// ScoreRow is what one kind of unattended run in one workspace came to.
type ScoreRow struct {
	Workspace string  `json:"workspace"`
	Kind      string  `json:"kind"`
	Runs      int     `json:"runs"`
	Pushed    int     `json:"pushed"`
	Green     int     `json:"green"`
	Replied   int     `json:"replied"`
	Idle      int     `json:"idle"`
	Blocked   int     `json:"blocked"`
	Failed    int     `json:"failed"`
	Running   int     `json:"running,omitempty"`
	Tokens    int64   `json:"tokens,omitempty"`
	USD       float64 `json:"usd,omitempty"`
}

// idleHintMin is how many runs a kind needs before an all-idle streak is worth a word
const idleHintMin = 4

// Scorecard counts runs since a time by workspace and kind. Riders of a
// batched run, runs never started and runs the daemon cut short are not runs.
// A run that said nothing about its outcome and left no link is in none of
// the columns.
func Scorecard(records []FixRecord, workspace string, since time.Time) []ScoreRow {
	rows := map[string]*ScoreRow{}
	for _, r := range records {
		if r.StartedAt.Before(since) || (workspace != "" && r.Workspace != workspace) || !countedRun(r) {
			continue
		}
		key := r.Workspace + "\x00" + r.Kind
		row := rows[key]
		if row == nil {
			row = &ScoreRow{Workspace: r.Workspace, Kind: r.Kind}
			rows[key] = row
		}
		row.Runs++
		row.Tokens += r.Tokens
		row.USD += r.CostUSD
		switch {
		case !r.Done():
			row.Running++
		case r.Error != "" && !r.Forgiven:
			row.Failed++
		case !r.GreenAt.IsZero():
			row.Green++
			row.Pushed++
		case r.Said == "pushed" || (r.Said == "" && len(r.PRs) > 0):
			row.Pushed++
		case r.Said == "replied":
			row.Replied++
		case r.Said == "nothing":
			row.Idle++
		case r.Said == "blocked":
			row.Blocked++
		}
	}
	out := make([]ScoreRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Workspace != out[j].Workspace {
			return out[i].Workspace < out[j].Workspace
		}
		return out[i].Runs > out[j].Runs
	})
	return out
}

func countedRun(r FixRecord) bool {
	if strings.HasPrefix(r.Note, "in one run with ") {
		return false
	}
	return !strings.HasPrefix(r.Error, "not started") && !strings.HasPrefix(r.Error, "interrupted") && !strings.HasPrefix(r.Error, "could not create worktrees")
}

// IdleKinds are the rows whose runs keep ending with nothing to show. Reviews
// and routines post or report rather than push, so they are never named.
func IdleKinds(rows []ScoreRow) []ScoreRow {
	var out []ScoreRow
	for _, r := range rows {
		switch Kind(r.Kind) {
		case KindReviewRequested, KindRoutine:
			continue
		}
		done := r.Runs - r.Running
		if done >= idleHintMin && r.Idle*2 > done {
			out = append(out, r)
		}
	}
	return out
}
