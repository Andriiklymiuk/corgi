package watch

import (
	"testing"
	"time"
)

func TestMarkGreenCreditsTheLatestFinishedRunOnce(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	start := time.Now().Add(-time.Hour)
	link := "https://gitlab.com/acme/core/-/merge_requests/9096"
	l.StartFor(Event{Key: "k1", Workspace: "ws", Ref: "acme/core!9096", Kind: KindCIFailed, URL: link}, start)
	l.Finish("k1", nil, "pushed the fix", "", start.Add(10*time.Minute))

	r, ok := l.MarkGreen("ws", link, time.Now())
	if !ok || r.Key != "k1" {
		t.Fatalf("MarkGreen = %+v, %v; want k1 credited", r, ok)
	}
	if _, again := l.MarkGreen("ws", link, time.Now()); again {
		t.Error("a run is credited once")
	}
	if got := LoadFixLog(l.path[:len(l.path)-len("/watch/fixes.json")]).Records()[0].GreenAt; got.IsZero() {
		t.Error("the credit is saved")
	}
}

func TestMarkGreenIgnoresOldAndFailedRuns(t *testing.T) {
	l := LoadFixLog(t.TempDir())
	link := "https://github.com/acme/api/pull/7"
	old := time.Now().Add(-3 * 24 * time.Hour)
	l.StartFor(Event{Key: "old", Workspace: "ws", Kind: KindCIFailed, URL: link}, old)
	l.Finish("old", nil, "", "", old)
	if _, ok := l.MarkGreen("ws", link, time.Now()); ok {
		t.Error("a run days ago did not make today's pipeline green")
	}
	l.StartFor(Event{Key: "bad", Workspace: "ws", Kind: KindPRComment, URL: link + "#issuecomment-1"}, time.Now())
	l.Finish("bad", nil, "", "exit status 1", time.Now())
	if _, ok := l.MarkGreen("ws", link, time.Now()); ok {
		t.Error("a failed run gets no credit")
	}
}
