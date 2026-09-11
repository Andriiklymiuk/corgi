package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

// fakeClaude swaps the headless run for an echo and restores it after.
func fakeClaude(t *testing.T) *[]string {
	t.Helper()
	var ran []string
	var mu sync.Mutex
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		mu.Lock()
		ran = append(ran, dir+" "+strings.Join(args, " "))
		mu.Unlock()
		return exec.CommandContext(ctx, "echo", "opened https://github.com/acme/api/pull/412")
	}
	t.Cleanup(func() { claudeCommand = prev })
	return &ran
}

// collectNotes gathers notification bodies until every want has been seen.
// A fix notifies from its own goroutine, so waiting on one string alone
// returns whenever that one happens to win the race.
func collectNotes(t *testing.T, notes <-chan string, wants ...string) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	seen := func() bool {
		for _, want := range wants {
			found := false
			for body := range got {
				if strings.Contains(body, want) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	for {
		select {
		case b := <-notes:
			got[b] = true
			if seen() {
				return got
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("waiting for %q, have %v", wants, got)
		}
	}
}

func TestWatchSinkNotifiesAndFixesOnce(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	ran := fakeClaude(t)
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC", Rules: watch.Rules{Enabled: true}, Action: "fix", SkipPermissions: true}}
	d.startWatches(context.Background())

	e := watch.Event{Key: "linear:ABC-1", Source: "linear", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "Login loops", Mine: true, At: time.Now()}
	d.handleWatchEvent(context.Background(), e)
	d.handleWatchEvent(context.Background(), e)

	got := collectNotes(t, notes, "fixed ABC-1")
	if !got["new issue ABC-1 — Login loops"] || !got["fixed ABC-1 — https://github.com/acme/api/pull/412"] {
		t.Fatalf("notices %v", got)
	}
	if len(*ran) != 1 || !strings.Contains((*ran)[0], "--dangerously-skip-permissions") || !strings.Contains((*ran)[0], "/corgi:stories ABC-1") {
		t.Fatalf("claude runs %v", *ran)
	}
	if data, _ := os.ReadFile(filepath.Join(d.Dir, "watch", "events.jsonl")); strings.Count(string(data), "\n") != 1 {
		t.Fatalf("events log: %q", data)
	}
	if logs, _ := filepath.Glob(filepath.Join(d.Dir, "watch", "runs", "*.log")); len(logs) != 1 {
		t.Fatalf("run logs %v", logs)
	}
	if n := watch.LoadFixLog(d.Dir).StartedSince("acme", time.Now().Add(-time.Minute)); n != 1 {
		t.Fatalf("fix log has %d starts", n)
	}
}

func TestDeferredFixIsNotRunAndNotRetried(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	ran := fakeClaude(t)
	d.Watches = []WatchSpec{{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC", Rules: watch.Rules{Enabled: true}, Action: "fix", MaxFixesPerHour: 1}}
	d.startWatches(context.Background())

	first := watch.Event{Key: "linear:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "one", Mine: true}
	second := watch.Event{Key: "linear:ABC-2", Kind: watch.KindIssueNew, Ref: "ABC-2", Title: "two", Mine: true}
	d.handleWatchEvent(context.Background(), first)
	d.handleWatchEvent(context.Background(), second)
	got := collectNotes(t, notes, "fixed ABC-1", "fix deferred")
	if !got["new issue ABC-2 — two (fix deferred: 1/h cap)"] {
		t.Fatalf("notices %v", got)
	}
	if len(*ran) != 1 {
		t.Fatalf("claude ran %d times: %v", len(*ran), *ran)
	}
	if !d.watchState.IsSeen(first.Key) || d.watchState.IsSeen(second.Key) {
		t.Fatal("the deferred event must leave the seen list; the fixed one stays")
	}
	if d.watchState.Fixes.DeferredCount("acme") != 1 {
		t.Fatalf("deferred %d", d.watchState.Fixes.DeferredCount("acme"))
	}
	// Handed back while still capped: deferred again, once, no run.
	d.handleWatchEvent(context.Background(), second)
	collectNotes(t, notes, "fix deferred")
	if len(*ran) != 1 || d.watchState.Fixes.DeferredCount("acme") != 1 {
		t.Fatalf("retried: runs %v deferred %d", *ran, d.watchState.Fixes.DeferredCount("acme"))
	}
}

func writeLimits(t *testing.T, configDir string, fiveHour, sevenDay int, resets time.Time) {
	t.Helper()
	body := fmt.Sprintf(`{"cachedUsageUtilization":{"fetchedAtMs":%d,"utilization":{"five_hour":{"utilization":%d,"resets_at":%q},"seven_day":{"utilization":%d,"resets_at":%q}}}}`,
		resets.Add(-time.Hour).UnixMilli(), fiveHour, resets.Format(time.RFC3339Nano), sevenDay, resets.Add(48*time.Hour).Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(configDir, ".claude.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFixDeferral(t *testing.T) {
	noon := time.Date(2026, 9, 10, 12, 0, 0, 0, time.Local)
	at := func(h, m int) time.Time { return time.Date(2026, 9, 10, h, m, 0, 0, time.Local) }
	spec := WatchSpec{Workspace: "acme", ConfigDir: t.TempDir(), Quiet: "23:00-07:00"}
	fresh := func() *watch.FixLog { return watch.LoadFixLog(t.TempDir()) }
	started := func(workspace string, n int, ago time.Duration, base ...time.Time) *watch.FixLog {
		l := fresh()
		from := noon
		if len(base) > 0 {
			from = base[0]
		}
		for i := 0; i < n; i++ {
			l.Start(workspace, fmt.Sprintf("k%d", i), from.Add(-ago))
		}
		return l
	}
	limited := func(fiveHour, sevenDay int, resets time.Time) WatchSpec {
		s := spec
		s.ConfigDir = t.TempDir()
		writeLimits(t, s.ConfigDir, fiveHour, sevenDay, resets)
		return s
	}
	oneAnHour := spec
	oneAnHour.MaxFixesPerHour = 1

	cases := []struct {
		name string
		spec WatchSpec
		log  *watch.FixLog
		now  time.Time
		want string
	}{
		{"nothing started", spec, fresh(), noon, ""},
		{"under the hour cap", spec, started("acme", 2, 10*time.Minute), noon, ""},
		{"hour cap", spec, started("acme", 3, 10*time.Minute), noon, "3/h cap"},
		{"hour cap lifts", spec, started("acme", 3, 61*time.Minute), noon, ""},
		{"day cap", spec, started("acme", 10, 5*time.Hour), noon, "10/day cap"},
		{"day cap lifts", spec, started("acme", 10, 25*time.Hour), noon, ""},
		{"another workspace's fixes do not count", spec, started("web", 10, 10*time.Minute), noon, ""},
		{"own cap", oneAnHour, started("acme", 1, time.Minute), noon, "1/h cap"},
		{"quiet before midnight", spec, fresh(), at(23, 30), "quiet hours"},
		{"quiet after midnight", spec, fresh(), at(3, 0), "quiet hours"},
		{"quiet starts", spec, fresh(), at(23, 0), "quiet hours"},
		{"quiet ends", spec, fresh(), at(7, 0), ""},
		{"a cap says so before quiet does", spec, started("acme", 3, time.Minute, at(23, 30)), at(23, 30), "3/h cap"},
		{"limit 97", limited(97, 10, noon.Add(2*time.Hour)), fresh(), noon, "limit 97%"},
		{"week limit", limited(10, 96, noon.Add(2*time.Hour)), fresh(), noon, "limit 96%"},
		{"limit 94 is fine", limited(94, 10, noon.Add(2*time.Hour)), fresh(), noon, ""},
		{"a window that already reset", limited(97, 10, noon.Add(-time.Minute)), fresh(), noon, ""},
		{"no cache, no refusal", spec, fresh(), noon, ""},
	}
	for _, tc := range cases {
		if got := fixDeferral(tc.spec, tc.log, tc.now); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestQuietHours(t *testing.T) {
	for _, bad := range []string{"23:00", "25:00-07:00", "23:00-23:00", "late-early"} {
		if _, err := ParseQuiet(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	if q, err := ParseQuiet(""); err != nil || q.Contains(time.Now()) {
		t.Fatal("empty is no window")
	}
	q, err := ParseQuiet(" 12:00-13:00 ")
	if err != nil {
		t.Fatal(err)
	}
	at := func(h, m int) time.Time { return time.Date(2026, 9, 10, h, m, 0, 0, time.Local) }
	if !q.Contains(at(12, 30)) || q.Contains(at(13, 0)) || q.Contains(at(11, 59)) {
		t.Fatal("same-day window")
	}
}

func TestFixBudget(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.Local)
	l := watch.LoadFixLog(t.TempDir())
	l.Start("acme", "a", now.Add(-30*time.Minute))
	l.Start("acme", "b", now.Add(-3*time.Hour))
	l.Start("acme", "c", now.Add(-20*time.Hour)) // yesterday, still within the day cap
	l.Defer(watch.Event{Key: "d", Workspace: "acme"})
	b := BudgetFor(WatchSpec{Workspace: "acme"}, l, now)
	if b.Hour != 1 || b.Day != 3 || b.Today != 2 || b.PerHour != 3 || b.PerDay != 10 || b.Deferred != 1 || !b.Last.Equal(now.Add(-30*time.Minute)) {
		t.Fatalf("%+v", b)
	}
	if b.String() != "1/3 this hour · 3/10 today" {
		t.Fatal(b.String())
	}
}

func TestStartWatchesSkipsDeadSources(t *testing.T) {
	d := testDaemon(t)
	github, linear, gitlab := &countingSource{name: "github"}, &countingSource{name: "linear"}, &countingSource{name: "gitlab"}
	// Interval 0: the rounds are run by hand below, so nothing outlives the test.
	d.Watches = []WatchSpec{
		{Workspace: "web", Rules: watch.Rules{Enabled: true}, Sources: []watch.Source{github, linear}},
		{Workspace: "docs", Rules: watch.Rules{Enabled: true}, Sources: []watch.Source{gitlab}},
	}
	d.startWatches(context.Background())
	if got := d.watchers["web"].Sources; len(got) != 1 || got[0].Name() != "linear" {
		t.Fatalf("web polls %v", got)
	}
	if got := d.watchers["docs"].Sources; len(got) != 0 {
		t.Fatalf("docs polls %v", got)
	}
	d.watchers["web"].Once(context.Background(), time.Now())
	d.watchers["docs"].Once(context.Background(), time.Now())
	if github.polls != 0 || gitlab.polls != 0 || linear.polls != 1 {
		t.Fatalf("polls github %d gitlab %d linear %d", github.polls, gitlab.polls, linear.polls)
	}
}

type countingSource struct {
	name  string
	polls int
}

func (c *countingSource) Name() string { return c.name }
func (c *countingSource) Poll(context.Context, watch.Cursor) ([]watch.Event, watch.Cursor, error) {
	c.polls++
	return nil, watch.Cursor{"at": "x"}, nil
}

func TestWatchRoutesByProjectAndRepo(t *testing.T) {
	d := testDaemon(t)
	d.Notify = func(_, _ string) {}
	d.Watches = []WatchSpec{
		{Workspace: "web", Dir: t.TempDir(), Repos: []string{"acme/web"}, Rules: watch.Rules{Enabled: true, PRs: true}},
		{Workspace: "api", Dir: t.TempDir(), Project: "API", Repos: []string{"acme/api"}, Rules: watch.Rules{Enabled: true, PRs: true}},
	}
	d.startWatches(context.Background())
	d.handleWatchEvent(context.Background(), watch.Event{Key: "github:acme/api#3:c1", Kind: watch.KindPRComment, Ref: "acme/api#3", Mine: true})
	d.handleWatchEvent(context.Background(), watch.Event{Key: "linear:API-4", Kind: watch.KindIssueNew, Ref: "API-4", Mine: true})
	data, _ := os.ReadFile(filepath.Join(d.Dir, "watch", "events.jsonl"))
	if !strings.Contains(string(data), `"workspace":"api"`) || strings.Contains(string(data), `"workspace":"web"`) {
		t.Fatalf("routed wrong: %s", data)
	}
	if d.WatchIdentity("github") != "" {
		t.Fatal("no identity before a poll")
	}
}

func TestFixPromptPerKind(t *testing.T) {
	cases := []struct {
		name string
		e    watch.Event
		must []string
		not  []string
	}{
		{"new issue ships it", watch.Event{Kind: watch.KindIssueNew, Ref: "ABC-1"},
			[]string{"/corgi:stories ABC-1", "draft PRs"}, []string{"/corgi:review"}},
		{"issue comment answers or changes", watch.Event{Kind: watch.KindIssueComment, Ref: "ABC-2", Author: "Max", Body: "also X"},
			[]string{"Max", `"also X"`, "answer it as a comment on ABC-2", "do NOT open a PR", "existing branch for ABC-2", "ticket key in the branch names", "/corgi:stories ABC-2"}, nil},
		{"pr comment addresses feedback", watch.Event{Kind: watch.KindPRComment, URL: "https://github.com/a/b/pull/1"},
			[]string{"/corgi:review https://github.com/a/b/pull/1", "do not start a fresh review", "resolve the threads", "push the fixes"}, []string{"/corgi:stories"}},
		{"pr review addresses feedback", watch.Event{Kind: watch.KindPRReview, URL: "https://gitlab.com/a/b/-/merge_requests/2"},
			[]string{"/corgi:review https://gitlab.com/a/b/-/merge_requests/2", "my own PR"}, nil},
		{"unknown kind has no fix", watch.Event{Kind: "issue.closed", Ref: "ABC-3"}, nil, []string{"/corgi"}},
	}
	for _, tc := range cases {
		p := fixPrompt(tc.e)
		for _, m := range tc.must {
			if !strings.Contains(p, m) {
				t.Errorf("%s: missing %q in %q", tc.name, m, p)
			}
		}
		for _, n := range tc.not {
			if strings.Contains(p, n) {
				t.Errorf("%s: has %q in %q", tc.name, n, p)
			}
		}
	}
	if b := watchBody(watch.Event{Kind: watch.KindPRReview, Ref: "a/b#1", State: "approved"}); b != "someone reviewed a/b#1: approved" {
		t.Fatal(b)
	}
	e := watch.Event{Kind: watch.KindIssueNew, Ref: "ABC-1", URL: "https://x/browse/ABC-1"}
	args := fixArgs(WatchSpec{Workspace: "api"}, e)
	if args[0] != "-p" || !strings.HasPrefix(args[1], fixPrompt(e)) {
		t.Fatalf("the kind's own prompt leads: %q", args)
	}
	if strings.Join(args[2:], " ") != "--output-format json --permission-mode acceptEdits" {
		t.Fatalf("flags %q", args[2:])
	}
	// Nobody but the run has read this diff, and whoever finds the PR later
	// needs to know where it came from.
	for _, want := range []string{"review your own diff", "corgi watch · api · issue.new ABC-1", "https://x/browse/ABC-1"} {
		if !strings.Contains(args[1], want) {
			t.Errorf("an unattended prompt must carry %q:\n%s", want, args[1])
		}
	}
	// A run someone started by hand is already being read; it gets none of it.
	if strings.Contains(fixPrompt(e), "review your own diff") {
		t.Error("the suffix belongs to unattended runs, not to the prompt the phone hands a session")
	}
}

func TestProbeEvent(t *testing.T) {
	d := &Daemon{Dir: t.TempDir(), Watches: []WatchSpec{
		{Workspace: "acme", Project: "ABC", ConfigDir: t.TempDir(), Rules: watch.Rules{Enabled: true, Comments: true}, Action: "fix", SkipPermissions: true},
		{Workspace: "notes", Project: "DOC", ConfigDir: t.TempDir(), Rules: watch.Rules{Enabled: true}, Action: "notify"},
	}}
	e := watch.Event{Key: "test:issue.comment:ABC-3", Kind: watch.KindIssueComment, Ref: "ABC-3", Body: "why?", Mine: true}
	p, ok := d.ProbeEvent(e, time.Now())
	if !ok || p.Workspace != "acme" || !p.Matched || p.Seen || p.Deferred != "" || p.Busy || p.Action != "fix" {
		t.Fatalf("%+v %v", p, ok)
	}
	if !strings.Contains(p.Prompt, "/corgi:stories ABC-3") || p.Args[len(p.Args)-1] != "--dangerously-skip-permissions" || p.Budget.PerHour != 3 {
		t.Fatalf("%+v", p)
	}
	d.watchState.MarkSeen(e.Key)
	if p, _ := d.ProbeEvent(e, time.Now()); !p.Seen {
		t.Fatal("a seen key must say so")
	}
	if p, ok := d.ProbeEvent(watch.Event{Kind: watch.KindIssueNew, Ref: "DOC-1", Mine: true}, time.Now()); !ok || p.Workspace != "notes" || p.Action != "notify" || p.Prompt != "" {
		t.Fatalf("notify workspace: %+v", p)
	}
	if p, ok := d.ProbeEvent(watch.Event{Kind: watch.KindIssueNew, Ref: "ABC-9"}, time.Now()); !ok || p.Matched {
		t.Fatalf("owned but not mine: %+v %v", p, ok)
	}
	if _, ok := d.ProbeEvent(watch.Event{Kind: watch.KindPRReview, Ref: "x/y#1", Mine: true}, time.Now()); ok {
		t.Fatal("nobody watches PRs")
	}
	if watch.LoadFixLog(d.Dir).StartedSince("acme", time.Time{}) != 0 {
		t.Fatal("a probe records nothing")
	}
}

func TestQuietHoursHoldTheNotificationAndReleaseItLater(t *testing.T) {
	d := testDaemon(t)
	notes := make(chan string, 8)
	d.Notify = func(_, body string) { notes <- body }
	fakeClaude(t)
	// A window that is open now would defeat the test, so cover the whole day.
	spec := WatchSpec{Workspace: "acme", Dir: t.TempDir(), ConfigDir: t.TempDir(), Project: "ABC",
		Rules: watch.Rules{Enabled: true}, Action: "notify", Quiet: "00:00-23:59"}
	d.Watches = []WatchSpec{spec}
	d.startWatches(context.Background())

	d.handleWatchEvent(context.Background(), watch.Event{Key: "linear:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "one", Mine: true})
	d.handleWatchEvent(context.Background(), watch.Event{Key: "linear:ABC-2", Kind: watch.KindIssueNew, Ref: "ABC-2", Title: "two", Mine: true})

	select {
	case body := <-notes:
		t.Fatalf("quiet hours must not notify, got %q", body)
	case <-time.After(300 * time.Millisecond):
	}
	// The events are still recorded — the inbox shows them, nothing is lost.
	if data, _ := os.ReadFile(filepath.Join(d.Dir, "watch", "events.jsonl")); strings.Count(string(data), "\n") != 2 {
		t.Fatalf("both events should be logged: %q", data)
	}

	// The window opens: one notification for the lot, not one each.
	open := spec
	open.Quiet = ""
	d.releaseHeld(open, time.Now())
	got := collectNotes(t, notes, "while you were away")
	var summary string
	for body := range got {
		if strings.Contains(body, "while you were away") {
			summary = body
		}
	}
	if !strings.Contains(summary, "2 while you were away") ||
		!strings.Contains(summary, "ABC-1") || !strings.Contains(summary, "ABC-2") {
		t.Fatalf("summary = %q", summary)
	}

	// Released once: a second round says nothing.
	d.releaseHeld(open, time.Now())
	select {
	case body := <-notes:
		t.Fatalf("held notes are delivered once, got %q", body)
	case <-time.After(200 * time.Millisecond):
	}

	// Still quiet? Nothing is released.
	d.watchState.Hold("acme", "later", time.Now())
	d.releaseHeld(spec, time.Now())
	select {
	case body := <-notes:
		t.Fatalf("still quiet, got %q", body)
	case <-time.After(200 * time.Millisecond):
	}
}
