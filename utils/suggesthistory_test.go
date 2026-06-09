package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Add composite index on orders(creator_id, created_at)": "add-composite-index-on-orders-creator-id-created-at",
		"Cache /search results 60s":                             "cache-search-results-60s",
		"  Trim  --  Edges  ":                                   "trim-edges",
		"Already-slugged":                                       "already-slugged",
		"":                                                      "",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadSuggestHistory_MissingIsEmpty(t *testing.T) {
	root := t.TempDir() // no .corgi/ inside
	h, err := LoadSuggestHistory(root)
	if err != nil {
		t.Fatalf("expected no error for missing history, got: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil history")
	}
	if h.Version != 1 {
		t.Errorf("expected Version=1, got %d", h.Version)
	}
	if len(h.Entries) != 0 {
		t.Errorf("expected zero entries, got %d", len(h.Entries))
	}
}

func TestLoadSuggestHistory_ParsesFixtureInOrder(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "corgi_services"), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := `{
  "version": 1,
  "entries": [
    {"slug":"orders-composite-index","title":"Add composite index on orders(creator_id, created_at)","lens":"eng","status":"filed","ticket":"ABC-321","ts":"2026-06-08T09:23:11Z"},
    {"slug":"weekly-activity-digest","title":"Email-digest of weekly activity","lens":"product","status":"dismissed","ticket":"","ts":"2026-05-30T09:23:05Z"},
    {"slug":"search-result-cache","title":"Cache /search results 60s","lens":"eng","status":"skipped","ticket":"","ts":"2026-06-08T09:23:11Z"}
  ]
}`
	if err := os.WriteFile(SuggestHistoryPath(root), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := LoadSuggestHistory(root)
	if err != nil {
		t.Fatalf("LoadSuggestHistory failed: %v", err)
	}
	if len(h.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(h.Entries))
	}
	if h.Entries[0].Slug != "orders-composite-index" || h.Entries[0].Status != "filed" || h.Entries[0].Ticket != "ABC-321" {
		t.Errorf("entry[0] mismatch: %+v", h.Entries[0])
	}
	if h.Entries[1].Slug != "weekly-activity-digest" || h.Entries[1].Lens != "product" {
		t.Errorf("entry[1] mismatch: %+v", h.Entries[1])
	}
	if h.Entries[2].Status != "skipped" {
		t.Errorf("entry[2] mismatch: %+v", h.Entries[2])
	}
	wantTs := time.Date(2026, 6, 8, 9, 23, 11, 0, time.UTC)
	if !h.Entries[0].Ts.Equal(wantTs) {
		t.Errorf("entry[0].Ts = %v, want %v", h.Entries[0].Ts, wantTs)
	}
}

func TestAppendSuggestEntry_CreatesDirAndRoundTrips(t *testing.T) {
	root := t.TempDir() // no corgi_services/ yet
	e := SuggestEntry{
		Slug: "orders-composite-index", Title: "Add composite index", Lens: "eng",
		Status: "filed", Ticket: "ABC-321", Ts: time.Date(2026, 6, 8, 9, 23, 11, 0, time.UTC),
	}
	if err := AppendSuggestEntry(root, e); err != nil {
		t.Fatalf("AppendSuggestEntry failed: %v", err)
	}

	// corgi_services/ created with 0o755, file 0o644 (match SaveUserConfig).
	di, err := os.Stat(filepath.Join(root, "corgi_services"))
	if err != nil {
		t.Fatalf("expected corgi_services/ created: %v", err)
	}
	if di.Mode().Perm() != 0o755 {
		t.Errorf("corgi_services/ mode = %o, want 0755", di.Mode().Perm())
	}
	fi, err := os.Stat(SuggestHistoryPath(root))
	if err != nil {
		t.Fatalf("expected history file: %v", err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("history file mode = %o, want 0644", fi.Mode().Perm())
	}

	// Per-developer state must be gitignored (docs/skill promise it stays
	// out of commits), via corgi_services/.gitignore.
	gi, err := os.ReadFile(filepath.Join(root, "corgi_services", ".gitignore"))
	if err != nil {
		t.Fatalf("expected corgi_services/.gitignore: %v", err)
	}
	if !strings.Contains(string(gi), suggestHistoryFileName) {
		t.Errorf("%s not gitignored; .gitignore = %q", suggestHistoryFileName, gi)
	}

	h, err := LoadSuggestHistory(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Entries) != 1 || h.Entries[0].Slug != "orders-composite-index" || h.Entries[0].Ticket != "ABC-321" {
		t.Fatalf("appended entry not read back: %+v", h.Entries)
	}

	// A second append preserves the first (append-only).
	if err := AppendSuggestEntry(root, SuggestEntry{Slug: "second", Status: "skipped", Ts: e.Ts}); err != nil {
		t.Fatal(err)
	}
	h, _ = LoadSuggestHistory(root)
	if len(h.Entries) != 2 {
		t.Fatalf("expected 2 entries after second append, got %d", len(h.Entries))
	}
}

func TestShouldSkip(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	cooldown := 30 * 24 * time.Hour

	h := &SuggestHistory{Version: 1, Entries: []SuggestEntry{
		{Slug: "filed-one", Status: "filed", Ts: now.Add(-100 * 24 * time.Hour)}, // old but filed → always blocks
		{Slug: "dismissed-recent", Status: "dismissed", Ts: now.Add(-5 * 24 * time.Hour)},
		{Slug: "dismissed-old", Status: "dismissed", Ts: now.Add(-40 * 24 * time.Hour)},
		{Slug: "proposed-recent", Status: "proposed", Ts: now.Add(-3 * 24 * time.Hour)},
		{Slug: "skipped-recent", Status: "skipped", Ts: now.Add(-1 * 24 * time.Hour)}, // audit only, never blocks
	}}

	tests := []struct {
		slug       string
		wantSkip   bool
		wantReason string
	}{
		{"filed-one", true, "filed"},
		{"dismissed-recent", true, "dismissed"},
		{"dismissed-old", false, ""},
		{"proposed-recent", true, "proposed"},
		{"skipped-recent", false, ""},
		{"unknown", false, ""},
	}
	for _, tc := range tests {
		gotSkip, gotReason := ShouldSkip(h, tc.slug, now, cooldown)
		if gotSkip != tc.wantSkip || gotReason != tc.wantReason {
			t.Errorf("ShouldSkip(%q) = (%v,%q), want (%v,%q)", tc.slug, gotSkip, gotReason, tc.wantSkip, tc.wantReason)
		}
	}
}

func TestRateLimited(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	h := &SuggestHistory{Version: 1, Entries: []SuggestEntry{
		{Slug: "a", Status: "filed", Ts: now.Add(-2 * 24 * time.Hour)},  // within week
		{Slug: "b", Status: "filed", Ts: now.Add(-10 * 24 * time.Hour)}, // outside week
		{Slug: "c", Status: "dismissed", Ts: now.Add(-1 * 24 * time.Hour)},
	}}

	// default (<=0) → treated as 1; one filed in the week → limited.
	if limited := RateLimited(h, now, 0); !limited {
		t.Error("maxPerWeek<=0 should default to 1 and block with one filed in the week")
	}
	// max 2 → one filed in the week is under the cap.
	if limited := RateLimited(h, now, 2); limited {
		t.Error("maxPerWeek=2 with one filed in the week should NOT be limited")
	}

	// two filed in the week, cap 2 → limited.
	h2 := &SuggestHistory{Version: 1, Entries: []SuggestEntry{
		{Slug: "a", Status: "filed", Ts: now.Add(-1 * 24 * time.Hour)},
		{Slug: "b", Status: "filed", Ts: now.Add(-3 * 24 * time.Hour)},
	}}
	if limited := RateLimited(h2, now, 2); !limited {
		t.Error("two filed in the week with cap 2 should be limited")
	}

	// hard ceiling: cap 5 is clamped to 3. Three filed in the week → limited.
	h3 := &SuggestHistory{Version: 1, Entries: []SuggestEntry{
		{Slug: "a", Status: "filed", Ts: now.Add(-1 * 24 * time.Hour)},
		{Slug: "b", Status: "filed", Ts: now.Add(-2 * 24 * time.Hour)},
		{Slug: "c", Status: "filed", Ts: now.Add(-3 * 24 * time.Hour)},
	}}
	if limited := RateLimited(h3, now, 5); !limited {
		t.Error("cap 5 must clamp to 3; three filed in the week should be limited")
	}
	// two filed in the week, ceiling-clamped cap 3 → not limited.
	if limited := RateLimited(h2, now, 5); limited {
		t.Error("two filed with clamped cap 3 should NOT be limited")
	}
}

func TestSuggestHistoryPath(t *testing.T) {
	root := "/abs/ws"
	want := filepath.Join("/abs/ws", "corgi_services", "suggest-history.json")
	if got := SuggestHistoryPath(root); got != want {
		t.Errorf("SuggestHistoryPath = %q, want %q", got, want)
	}
}
