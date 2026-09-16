package cmd

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestSynthesizeWatchEventShapesEachKind(t *testing.T) {
	specs := []daemon.WatchSpec{{Workspace: "api", Project: "ACME", Repos: []string{"acme/api"}}}
	issue, err := synthesizeWatchEvent(watch.KindIssueComment, specs, "", "", "")
	if err != nil || issue.Ref != "ACME-1" || issue.Source != "linear" || issue.Body == "" || issue.Key != "test:issue.comment:ACME-1" {
		t.Errorf("issue comment = %+v, %v", issue, err)
	}
	review, err := synthesizeWatchEvent(watch.KindPRReview, specs, "", "", "")
	if err != nil || review.Ref != "acme/api#1" || review.Source != "github" || review.State != "changes_requested" || review.URL == "" {
		t.Errorf("pr review = %+v, %v", review, err)
	}
	asked, err := synthesizeWatchEvent(watch.KindReviewRequested, specs, "acme/web!4", "", "")
	if err != nil || asked.Mine || asked.Source != "gitlab" || asked.Author != "a colleague" {
		t.Errorf("review request = %+v, %v", asked, err)
	}
	ci, err := synthesizeWatchEvent(watch.KindCIFailed, specs, "", "", "")
	if err != nil || ci.Ref != "acme/api" || !strings.HasSuffix(ci.URL, "/actions") || ci.Title == "" {
		t.Errorf("ci = %+v, %v", ci, err)
	}
	if _, err := synthesizeWatchEvent(watch.Kind("routine"), specs, "", "", ""); err == nil {
		t.Error("an unknown kind must be refused")
	}
	if got := watchFirstRepo(daemon.WatchSpec{}, "x/y"); got != "x/y" {
		t.Errorf("no repos → fallback, got %q", got)
	}
	if got := synthesizeIssue(specs[0], "keep me"); got != "keep me" {
		t.Errorf("a given body must stay, got %q", got)
	}
}

func synthesizeIssue(spec daemon.WatchSpec, body string) string {
	e := watch.Event{Kind: watch.KindIssueComment, Body: body}
	synthesizeIssueEvent(&e, spec)
	return e.Body
}

func TestWatchedWorkspaceLineAndStatusPrinters(t *testing.T) {
	spec := daemon.WatchSpec{Workspace: "api", Action: "notify", Interval: 5 * time.Minute, Skipped: []string{"github"}, Quiet: "17:00-08:00"}
	line := watchedWorkspaceLine(spec)
	for _, want := range []string{"api", "notify", "5m0s", "no source with a token", "github skipped"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q lacks %q", line, want)
		}
	}
	out := captureStdout(t, func() {
		printWatchedWorkspaces([]daemon.WatchSpec{spec}, nil, time.Now())
		printWatchPolls(nil)
		printWatchPolls([]watch.Summary{{Key: "linear:api", Polled: "2m ago"}, {Key: "github:acme/api", Polled: "now", Error: "401"}})
	})
	for _, want := range []string{"Watched", "quiet 17:00-08:00", "Last polls", "linear:api", "✗ 401"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output lacks %q:\n%s", want, out)
		}
	}
}
