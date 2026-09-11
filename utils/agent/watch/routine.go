package watch

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A routine is a run the daemon starts on a clock rather than on an event:
// the morning digest, the PR babysitter, the dependency triage, the release
// notes. Each is one prompt to `claude -p` with the caps, the quiet hours,
// the budget and the log every unattended run has, and its report is one
// row in the inbox.

// KindRoutine is the inbox kind for a routine's report.
const KindRoutine Kind = "routine"

// RoutineKind is one entry of the catalog.
type RoutineKind struct {
	Name    string
	What    string
	Prompt  string
	Default string // the schedule it makes sense on
}

// Catalog is what `corgi agent routine add <kind>` knows how to run.
var Catalog = []RoutineKind{
	{Name: "digest", What: "what happened here since yesterday, in five bullets", Default: "daily 08:30",
		Prompt: "Write the morning digest for this workspace: pull requests opened, merged or gone red since yesterday, tickets that moved, anything stalled more than two days. Use gh/glab and the tracker MCP tools; read, do not change. At most five bullets, each one line, the most important first. Summarise, do not itemise. Start your answer with a one-line headline."},
	{Name: "babysit-pr", What: "keep my open pull requests moving: CI, reviews, at most three rounds", Default: "every 2h",
		Prompt: "For every open pull request of mine in this stack: check CI and the review comments. Fix what is real on the branch and push (at most three rounds per PR), reply to and dismiss what is not with one sentence saying why, and list what needs a person. Never merge, never force-push, never flip a draft to ready. Start your answer with a one-line headline: how many PRs are green, red, waiting."},
	{Name: "deps", What: "triage dependency-bump pull requests: safe, dead, verify", Default: "weekly Mon 09:00",
		Prompt: "Look at every open dependency-bump pull request (renovate, dependabot) in this stack. For each, say SAFE (patch or minor, changelog clean, CI green), DEAD (superseded, or the dependency is unused — say where you looked) or VERIFY (major, security, or a behaviour change — name the change). Read only: change nothing, merge nothing. Start with a one-line headline: counts per class."},
	{Name: "release-notes", What: "draft release notes since the last tag", Default: "weekly Fri 16:00",
		Prompt: "Draft release notes for this stack since the last git tag in each repository: merged pull requests grouped by the tickets they close, in words a user of the product understands, breaking changes first. Open one draft pull request per repository on a branch release-notes/<date> with the notes as the changelog entry; do not tag, do not publish. Start with a one-line headline."},
	{Name: "flaky", What: "find tests that pass and fail without a change", Default: "daily 03:00",
		Prompt: "Run the test suite of every service twice (corgi test, or each service's own command). Report every test whose result differed between the two runs, with the failing output. Open one ticket per flaky test on the tracker with the evidence, unless one already exists — search first. Change no code. Start with a one-line headline: how many flaky tests."},
	{Name: "doc-drift", What: "docs that name what changed on main this week", Default: "weekly Mon 10:00",
		Prompt: "Run `corgi docs check --base $(git describe --tags --abbrev=0 2>/dev/null || echo HEAD~30)` in this workspace. For every doc it lists, read the doc and the change it names, fix the doc if it is wrong now, and open one draft pull request with the doc fixes. Fix stale CLAUDE.md pointers too. Start with a one-line headline: docs touched."},
}

// CatalogKind finds one entry by name.
func CatalogKind(name string) (RoutineKind, bool) {
	for _, k := range Catalog {
		if strings.EqualFold(strings.TrimSpace(name), k.Name) {
			return k, true
		}
	}
	return RoutineKind{}, false
}

// Schedule is when a routine runs: "daily HH:MM", "every Nh" / "every Nm",
// or "weekly Mon HH:MM".
type Schedule struct {
	Every   time.Duration
	At      int // minutes since midnight, for daily and weekly
	Weekday time.Weekday
	Weekly  bool
	Daily   bool
}

var weekdays = map[string]time.Weekday{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// ParseSchedule reads the three shapes; anything else is an error that
// says what the shapes are.
func ParseSchedule(text string) (Schedule, error) {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	usage := fmt.Errorf("a schedule is \"daily HH:MM\", \"every 6h\" (or 30m) or \"weekly Mon HH:MM\", not %q", text)
	if len(f) == 0 {
		return Schedule{}, usage
	}
	switch f[0] {
	case "every":
		if len(f) != 2 {
			return Schedule{}, usage
		}
		d, err := time.ParseDuration(f[1])
		if err != nil || d < 5*time.Minute {
			return Schedule{}, fmt.Errorf("every needs a duration of at least 5m, not %q", f[1])
		}
		return Schedule{Every: d}, nil
	case "daily":
		if len(f) != 2 {
			return Schedule{}, usage
		}
		at, err := parseClock(f[1])
		if err != nil {
			return Schedule{}, err
		}
		return Schedule{Daily: true, At: at}, nil
	case "weekly":
		if len(f) != 3 {
			return Schedule{}, usage
		}
		day := f[1]
		if len(day) > 3 {
			day = day[:3]
		}
		wd, ok := weekdays[day]
		if !ok {
			return Schedule{}, fmt.Errorf("weekly needs a day name, not %q", f[1])
		}
		at, err := parseClock(f[2])
		if err != nil {
			return Schedule{}, err
		}
		return Schedule{Weekly: true, Weekday: wd, At: at}, nil
	}
	return Schedule{}, usage
}

func parseClock(s string) (int, error) {
	hh, mm, ok := strings.Cut(s, ":")
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if !ok || err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("a time is HH:MM, not %q", s)
	}
	return h*60 + m, nil
}

// Due says whether a routine last run at last should run at now.
func (s Schedule) Due(last, now time.Time) bool {
	switch {
	case s.Every > 0:
		return last.IsZero() || now.Sub(last) >= s.Every
	case s.Daily:
		slot := time.Date(now.Year(), now.Month(), now.Day(), s.At/60, s.At%60, 0, 0, now.Location())
		return !now.Before(slot) && last.Before(slot)
	case s.Weekly:
		if now.Weekday() != s.Weekday {
			return false
		}
		slot := time.Date(now.Year(), now.Month(), now.Day(), s.At/60, s.At%60, 0, 0, now.Location())
		return !now.Before(slot) && last.Before(slot)
	}
	return false
}

func (s Schedule) String() string {
	switch {
	case s.Every > 0:
		return "every " + s.Every.String()
	case s.Daily:
		return fmt.Sprintf("daily %02d:%02d", s.At/60, s.At%60)
	case s.Weekly:
		return fmt.Sprintf("weekly %s %02d:%02d", s.Weekday.String()[:3], s.At/60, s.At%60)
	}
	return ""
}
