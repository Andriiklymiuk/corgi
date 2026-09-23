package watch

import (
	"path/filepath"
	"testing"
	"time"
)

func TestARowLeavesTheInboxWhenItsWorkIsOver(t *testing.T) {
	dir := t.TempDir()
	pulls := &PullLog{path: filepath.Join(dir, "pulls.json"), Pulls: map[string]PullStatus{}}
	fixes := &FixLog{path: filepath.Join(dir, "fixes.json")}
	at := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	ask := Event{Key: "slack:c1:1790000200", Kind: KindReviewRequested, Workspace: "w", Ref: "slack-1790000200", State: "#code-review", At: at,
		Links: []string{"https://github.com/acme/api/pull/7", "https://github.com/acme/web/pull/3"}}

	if why := InboxDone(ask, ask.State, pulls, fixes); why != "" {
		t.Fatalf("nothing known yet, it waits: %q", why)
	}
	_ = pulls.Set("acme/api#7", PullStatus{State: "merged"})
	if why := InboxDone(ask, ask.State, pulls, fixes); why != "" {
		t.Fatalf("one of two merged, it still waits: %q", why)
	}
	_ = pulls.Set("acme/web#3", PullStatus{State: "closed"})
	if why := InboxDone(ask, ask.State, pulls, fixes); why != "merged" {
		t.Fatalf("both done: %q", why)
	}

	comment := Event{Key: "github:acme/api#9:c5", Kind: KindPRComment, Workspace: "w", Ref: "acme/api#9", At: at}
	fixes.Started = append(fixes.Started, FixRecord{Key: "other", Workspace: "w", Ref: "acme/api#9", StartedAt: at.Add(-time.Hour), FinishedAt: at})
	if InboxDone(comment, "", pulls, fixes) != "" {
		t.Fatal("a run that started before the comment did not read it")
	}
	fixes.Started = append(fixes.Started, FixRecord{Key: "later", Workspace: "w", Ref: "acme/api#9", StartedAt: at.Add(time.Minute), FinishedAt: at.Add(5 * time.Minute), Failure: "limit"})
	if InboxDone(comment, "", pulls, fixes) != "" {
		t.Fatal("a failed run handled nothing")
	}
	fixes.Started = append(fixes.Started, FixRecord{Key: "ok", Workspace: "w", Ref: "acme/api#9", StartedAt: at.Add(time.Minute), FinishedAt: at.Add(9 * time.Minute)})
	if InboxDone(comment, "", pulls, fixes) != "handled" {
		t.Fatal("a later run on the same pull request handled it")
	}

	ticket := Event{Key: "linear:ABC-1:c1", Kind: KindIssueComment, Workspace: "w", Ref: "ABC-1", State: "In Progress", At: at}
	fixes.Started = append(fixes.Started, FixRecord{Key: "x", Workspace: "w", Ref: "ABC-1", StartedAt: at.Add(time.Minute), FinishedAt: at.Add(2 * time.Minute)})
	if InboxDone(ticket, ticket.State, pulls, fixes) != "" {
		t.Fatal("a ticket comment leaves only through its own run")
	}
	if InboxDone(Event{Key: "k", Kind: KindIssueNew, Ref: "ABC-2", State: "Todo"}, "Done", pulls, fixes) == "" {
		t.Fatal("a finished ticket is settled as before")
	}
}
