package watch

import (
	"strings"
	"testing"
	"time"
)

func fieldLog(t *testing.T, titles ...string) *TaskLog {
	t.Helper()
	log := LoadTasks(t.TempDir())
	for _, title := range titles {
		if _, err := log.Add(title, "", "ws", "cli", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return log
}

func TestLinkSourceAndLabel(t *testing.T) {
	for link, want := range map[string][2]string{
		"https://github.com/acme/api/pull/59":                  {"github", "acme/api#59"},
		"https://github.com/acme/api/issues/7":                 {"github", "acme/api#7"},
		"https://gitlab.com/acme/group/api/-/merge_requests/3": {"gitlab", "acme/group/api!3"},
		"https://git.acme.io/acme/api/-/issues/9":              {"gitlab", "acme/api#9"},
		"https://acme.atlassian.net/browse/ABC-12":             {"jira", "ABC-12"},
		"https://acme.atlassian.net/wiki/spaces/X/pages/1":     {"confluence", "acme.atlassian.net/1"},
		"https://linear.app/acme/issue/HUM-34/some-title":      {"linear", "HUM-34"},
		"https://example.com/doc":                              {"link", "example.com/doc"},
	} {
		if got := [2]string{LinkSource(link), LinkLabel(link)}; got != want {
			t.Errorf("%s = %v, want %v", link, got, want)
		}
	}
}

func TestLinkRefusesWhatIsNotAPlainWebAddress(t *testing.T) {
	log := fieldLog(t, "one")
	for _, bad := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://user:pass@example.com/x", "not a link", "https://" + strings.Repeat("a", 600) + ".com"} {
		if _, err := log.Link("TASK-1", []string{bad}, time.Now()); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	got, err := log.Link("TASK-1", []string{"https://github.com/a/b/pull/1", "https://github.com/a/b/pull/1"}, time.Now())
	if err != nil || len(got.Links) != 1 {
		t.Fatalf("links = %v, err = %v", got.Links, err)
	}
	if got, _ = log.Unlink("TASK-1", "https://github.com/a/b/pull/1", time.Now()); len(got.Links) != 0 {
		t.Fatalf("links after unlink = %v", got.Links)
	}
	if e := LoadTasks(strings.TrimSuffix(log.path, "/watch/tasks.json")).Tasks[0].Event(); len(e.Links) != 0 {
		t.Fatalf("event links = %v", e.Links)
	}
}

func TestLinkStopsAtTheLimit(t *testing.T) {
	log := fieldLog(t, "one")
	links := []string{}
	for i := 0; i <= maxTaskLinks; i++ {
		links = append(links, "https://example.com/"+strings.Repeat("x", i+1))
	}
	if _, err := log.Link("TASK-1", links, time.Now()); err == nil {
		t.Fatal("too many links were accepted")
	}
}

func TestClaimIsHeldUntilReleasedOrForced(t *testing.T) {
	log := fieldLog(t, "one")
	now := time.Now()
	if _, err := log.Claim("TASK-1", "anna", false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Claim("TASK-1", "Anna", false, now); err != nil {
		t.Fatalf("the owner could not claim again: %v", err)
	}
	if _, err := log.Claim("TASK-1", "bot", false, now); err == nil {
		t.Fatal("a second owner took a held task")
	}
	held, _ := log.Find("TASK-1")
	if !held.HeldByAnother("bot") || held.HeldByAnother("ANNA") {
		t.Fatalf("held = %+v", held)
	}
	if got, err := log.Claim("TASK-1", "bot", true, now); err != nil || got.Owner != "bot" {
		t.Fatalf("force: %v %v", got.Owner, err)
	}
	if got, _ := log.Release("TASK-1", now); got.Owner != "" || !got.ClaimedAt.IsZero() {
		t.Fatalf("release left %+v", got)
	}
	if _, err := log.Claim("TASK-1", " ", false, now); err == nil {
		t.Fatal("an empty name claimed")
	}
}

func TestPriorityAndDue(t *testing.T) {
	log := fieldLog(t, "low", "urgent", "none", "soon", "later")
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local)
	for ref, p := range map[string]string{"TASK-1": "low", "TASK-2": "URGENT"} {
		if _, err := log.SetPriority(ref, p, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := log.SetPriority("TASK-1", "asap", now); err == nil {
		t.Fatal("an unknown priority was accepted")
	}
	if _, err := log.SetDue("TASK-4", "2026-11-19", now); err != nil {
		t.Fatal(err)
	}
	if _, err := log.SetDue("TASK-5", "2026-12-01", now); err != nil {
		t.Fatal(err)
	}
	if _, err := log.SetDue("TASK-5", "tomorrow", now); err == nil {
		t.Fatal("a due date that is not a day was accepted")
	}
	order := []string{}
	for _, task := range log.ByPriority() {
		order = append(order, task.Title)
	}
	if got := strings.Join(order, ","); got != "urgent,low,soon,later,none" {
		t.Fatalf("order = %s", got)
	}
	soon, _ := log.Find("TASK-4")
	later, _ := log.Find("TASK-5")
	if !soon.Overdue(now) || later.Overdue(now) {
		t.Fatalf("overdue: soon %v later %v", soon.Overdue(now), later.Overdue(now))
	}
	if cleared, _ := log.SetDue("TASK-4", "none", now); cleared.Due != "" {
		t.Fatalf("due = %q", cleared.Due)
	}
}

func TestBlockedTaskWaitsForItsBlocker(t *testing.T) {
	log := fieldLog(t, "first", "second", "third")
	now := time.Now()
	if _, err := log.SetBlockedBy("TASK-2", []string{"TASK-1"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := log.SetBlockedBy("TASK-1", []string{"TASK-2"}, now); err == nil {
		t.Fatal("a circle was accepted")
	}
	if _, err := log.SetBlockedBy("TASK-3", []string{"TASK-2"}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := log.SetBlockedBy("TASK-1", []string{"TASK-3"}, now); err == nil {
		t.Fatal("a long circle was accepted")
	}
	if _, err := log.SetBlockedBy("TASK-2", []string{"TASK-2"}, now); err == nil {
		t.Fatal("a task waits for itself")
	}
	if _, err := log.SetBlockedBy("TASK-2", []string{"TASK-99"}, now); err == nil {
		t.Fatal("a missing blocker was accepted")
	}
	_, _ = log.AskRun("TASK-2", now)
	if got := log.Wanted(); len(got) != 0 {
		t.Fatalf("a blocked task is wanted: %v", got)
	}
	second, _ := log.Find("TASK-2")
	if open := log.OpenBlockers(second); len(open) != 1 || open[0] != "TASK-1" {
		t.Fatalf("open blockers = %v", open)
	}
	_, _ = log.Move("TASK-1", "Done", now)
	if open := log.OpenBlockers(second); len(open) != 0 {
		t.Fatalf("open blockers after done = %v", open)
	}
	if got := log.Wanted(); len(got) != 1 {
		t.Fatalf("wanted after the blocker finished = %v", got)
	}
	if cleared, _ := log.SetBlockedBy("TASK-2", nil, now); len(cleared.BlockedBy) != 0 {
		t.Fatalf("blockedBy = %v", cleared.BlockedBy)
	}
}
