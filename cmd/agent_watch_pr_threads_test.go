package cmd

import (
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestOpenThreadsKeepsTheLatestSummaryPerReviewer(t *testing.T) {
	note := func(who, body string) []watch.ThreadNote {
		return []watch.ThreadNote{{ID: "1", Author: who, Body: body}}
	}
	threads := []watch.ReviewThread{
		{Kind: "review", Notes: note("rev", "round 1")},
		{Kind: "review", Notes: note("ana", "nit")},
		{Kind: "review", Notes: note("rev", "round 2")},
		{Kind: "thread", ID: "PRRT_1", Path: "a.ts", Line: 4, Notes: note("rev", "the retry drops the token")},
		{Kind: "thread", ID: "PRRT_2", Resolved: true, Notes: note("rev", "ok")},
	}
	open := openThreads(threads)
	if len(open) != 3 || open[0].Notes[0].Body != "nit" || open[1].Notes[0].Body != "round 2" || open[2].ID != "PRRT_1" {
		t.Fatalf("open: %+v", open)
	}
	text := threadsText(prThreads{URL: "https://github.com/acme/api/pull/5", Threads: open})
	if !strings.Contains(text, "a.ts:4  [open]  thread PRRT_1  reply-to 1") || !strings.Contains(text, "the retry drops the token") {
		t.Fatalf("a reply needs the thread id and the root comment id: %s", text)
	}
}
