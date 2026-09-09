package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordWaitAppendsAndLoadWaitsReadsBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	first := Wait{At: now.Add(-time.Hour), Kind: "wait", Label: "acme", Profile: "work", Seconds: 30}
	second := Wait{At: now, Kind: "limited", Label: "web", Seconds: 90}
	if err := RecordWait(dir, first); err != nil {
		t.Fatal(err)
	}
	if err := RecordWait(dir, second); err != nil {
		t.Fatal(err)
	}
	if WaitsPath(dir) != filepath.Join(dir, "waits.jsonl") {
		t.Fatalf("path %s", WaitsPath(dir))
	}
	info, err := os.Stat(WaitsPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("waits log mode = %04o, want owner-only", perm)
	}
	got := LoadWaits(dir, time.Time{})
	if len(got) != 2 || got[0].Label != "acme" || got[0].Profile != "work" || got[1].Seconds != 90 {
		t.Fatalf("got %+v", got)
	}
	if got := LoadWaits(dir, now.Add(-time.Minute)); len(got) != 1 || got[0].Kind != "limited" {
		t.Fatalf("since filters out older waits: %+v", got)
	}
}

func TestRecordWaitFailsWhenTheDirIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecordWait(blocker, Wait{Kind: "wait"}); err == nil {
		t.Fatal("a file where the dir should be must fail")
	}
}

func TestLoadWaitsSkipsBadRowsAndSortsOldestFirst(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	row := func(at time.Time, label string) string {
		b, _ := json.Marshal(Wait{At: at, Kind: "wait", Label: label, Seconds: 1})
		return string(b)
	}
	write(t, WaitsPath(dir), []string{
		row(now, "newer"),
		"{not json",
		row(now.Add(-time.Hour), "older"),
	})
	got := LoadWaits(dir, time.Time{})
	if len(got) != 2 || got[0].Label != "older" || got[1].Label != "newer" {
		t.Fatalf("got %+v", got)
	}
	if got := LoadWaits(filepath.Join(dir, "nope"), time.Time{}); got != nil {
		t.Fatalf("missing log is no waits, got %+v", got)
	}
}

func TestLoadWaitsKeepsOnlyTheNewestRows(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	var b strings.Builder
	for i := 0; i < waitsKeep+3; i++ {
		data, _ := json.Marshal(Wait{At: base.Add(time.Duration(i) * time.Second), Kind: "wait", Seconds: i})
		b.Write(data)
		b.WriteByte('\n')
	}
	write(t, WaitsPath(dir), []string{strings.TrimRight(b.String(), "\n")})
	got := LoadWaits(dir, time.Time{})
	if len(got) != waitsKeep || got[0].Seconds != 3 {
		t.Fatalf("got %d rows, first seconds %d", len(got), got[0].Seconds)
	}
}

func TestSummarizeFoldsOneKind(t *testing.T) {
	waits := []Wait{
		{Kind: "wait", Label: "a", Seconds: 10},
		{Kind: "limited", Label: "x", Seconds: 500},
		{Kind: "wait", Label: "b", Seconds: 40},
		{Kind: "wait", Label: "c", Seconds: 20},
	}
	for _, tc := range []struct {
		kind string
		want WaitSummary
	}{
		{"wait", WaitSummary{Count: 3, Median: 20, Longest: 40, LongestLabel: "b", Total: 70}},
		{"limited", WaitSummary{Count: 1, Median: 500, Longest: 500, LongestLabel: "x", Total: 500}},
		{"other", WaitSummary{}},
	} {
		if got := Summarize(waits, tc.kind); got != tc.want {
			t.Errorf("%s: got %+v want %+v", tc.kind, got, tc.want)
		}
	}
	if got := Summarize(nil, "wait"); got != (WaitSummary{}) {
		t.Errorf("no waits: %+v", got)
	}
}
