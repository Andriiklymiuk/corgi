package watch

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseAge reads "7d" or anything time.ParseDuration does.
func ParseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%q is not a number of days", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration: use 7d or 48h", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("%q is negative", s)
	}
	return d, nil
}

// PrunableFixes is the isolated runs whose worktrees may go: finished at
// least olderThan ago, on a branch no unfinished run is still on. One
// record per workspace and branch, the newest.
func PrunableFixes(records []FixRecord, olderThan time.Duration, now time.Time) []FixRecord {
	busy := map[string]bool{}
	for _, r := range records {
		if r.Branch != "" && !r.Done() {
			busy[r.Workspace+"\x00"+r.Branch] = true
		}
	}
	seen := map[string]bool{}
	var out []FixRecord
	for _, r := range records {
		key := r.Workspace + "\x00" + r.Branch
		if r.Branch == "" || !r.Done() || busy[key] || seen[key] {
			continue
		}
		if r.FinishedAt.Add(olderThan).After(now) {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}
