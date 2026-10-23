package watch

import (
	"regexp"
	"sort"
)

type Gap string

const (
	GapNoDatabase Gap = "no database"
	GapNoTool     Gap = "tool not installed"
	GapNoDevice   Gap = "no device"
	GapNoService  Gap = "no live service"
	GapTestsSkip  Gap = "tests not run"
)

var gapWords = []struct {
	gap Gap
	re  *regexp.Regexp
}{
	{GapNoDatabase, regexp.MustCompile(`(?i)\bno (?:postgres|database|db|mysql|redis)\b|has no (?:postgres|database)|(?:against|on) a (?:local|real) database|database (?:is not|isn't) (?:running|up)|could not (?:reach|connect to) the database`)},
	{GapNoTool, regexp.MustCompile(`(?i)\b(?:isn't|is not|not) installed\b|command not found|no node_modules|dependencies (?:are not|aren't) installed`)},
	{GapNoDevice, regexp.MustCompile(`(?i)\b(?:no|not (?:checked|tested|run) on an?) (?:device|simulator|emulator)\b|need(?:s)? you or a device`)},
	{GapNoService, regexp.MustCompile(`(?i)against a live\b|no live (?:service|account|api)|could not reach (?:staging|the api)`)},
	{GapTestsSkip, regexp.MustCompile(`(?i)\b(?:did not|didn't|could not|couldn't) run (?:the )?(?:full )?(?:tests?|test suites?|specs?|lint)\b|lint did not run|tests? (?:did not|didn't) run`)},
}

func GapsIn(output string) []Gap {
	seen := map[Gap]bool{}
	for _, w := range gapWords {
		if w.re.MatchString(output) {
			seen[w.gap] = true
		}
	}
	out := make([]Gap, 0, len(seen))
	for g := range seen {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (l *FixLog) SetGaps(key string, gaps []Gap) {
	if key == "" || len(gaps) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.Started) - 1; i >= 0; i-- {
		if l.Started[i].Key == key {
			l.Started[i].Gaps = gaps
			_ = l.save()
			return
		}
	}
}
