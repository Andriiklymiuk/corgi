package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadCodexWindowTakesTheNewestRolloutsFullestWindow(t *testing.T) {
	home := t.TempDir()
	day := filepath.Join(home, "sessions", "2026", "09", "22")
	older := filepath.Join(home, "sessions", "2026", "09", "21")
	for _, d := range []string{day, older} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(older, "rollout-a.jsonl"), []byte(`{"timestamp":"2026-09-21T10:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":99,"resets_at":1790000000}}}}`+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(day, "rollout-b.jsonl"), []byte(
		`{"timestamp":"2026-09-22T10:00:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":40,"resets_at":1790000000},"secondary":{"used_percent":98.5,"resets_at":1790600000},"rate_limit_reached_type":null}}}`+"\n"+
			`{"timestamp":"2026-09-22T10:00:01Z","type":"response_item","payload":{"type":"message"}}`+"\n"), 0o600)

	w, ok := ReadCodexWindow(home)
	if !ok || w.Percent != 98 || !w.ResetsAt.Equal(time.Unix(1790600000, 0)) || w.Reached {
		t.Fatalf("window = %+v %v", w, ok)
	}
	if !w.Spent(time.Unix(1790500000, 0)) {
		t.Fatal("98% before the reset is spent")
	}
	if w.Spent(time.Unix(1790600001, 0)) {
		t.Fatal("after the reset it is free again")
	}
	if _, ok := ReadCodexWindow(t.TempDir()); ok {
		t.Fatal("no sessions, no reading")
	}
	if (CodexWindow{Reached: true}).Spent(time.Now()) != true {
		t.Fatal("a reached limit with no reset time is spent")
	}
}

func TestSummaryOfText(t *testing.T) {
	s := SummaryOfText("**Fixed the retry.**\nOpened https://gitlab.com/acme/api/-/merge_requests/5 for it.")
	if s.Line != "Fixed the retry." || s.PR != "https://gitlab.com/acme/api/-/merge_requests/5" {
		t.Fatalf("summary = %+v", s)
	}
}
