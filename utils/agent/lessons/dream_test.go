package lessons

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestDreamMergesRetiresAndArchives(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	add := func(daysAgo int, text string) {
		if err := Add(dir, "api", Lesson{At: now.AddDate(0, 0, -daysAgo), Source: "review", Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	add(300, "Run the migrations before the integration tests")
	add(40, "Always run database migrations before running integration tests")
	add(2, "Always run the database migrations before running the integration tests")
	add(10, "Mock the payment client in unit tests")
	f, _ := os.OpenFile(Path(dir, "api"), os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("\nA note I wrote by hand.\n")
	f.Close()

	dry, err := Dream(dir, "api", now, DreamOptions{MaxAge: 180 * 24 * time.Hour, Keep: 60, Similar: 0.8, DryRun: true})
	if err != nil || dry.Before != 4 || dry.After != 2 || len(dry.Dropped) != 2 {
		t.Fatalf("dry run: %+v %v", dry, err)
	}
	if len(List(dir, "api")) != 4 {
		t.Fatal("a dry run changes nothing")
	}
	rep, err := Dream(dir, "api", now, DefaultDream)
	if err != nil || rep.After != 2 {
		t.Fatalf("dream: %+v %v", rep, err)
	}
	left := List(dir, "api")
	if len(left) != 2 || left[0].Text != "Always run the database migrations before running the integration tests" {
		t.Fatalf("left: %+v", left)
	}
	body, _ := os.ReadFile(Path(dir, "api"))
	if !strings.Contains(string(body), "A note I wrote by hand.") || !strings.HasPrefix(string(body), "# Lessons · api") {
		t.Fatal("hand-written lines and the header stay")
	}
	arch, _ := os.ReadFile(ArchivePath(dir, "api"))
	if !strings.Contains(string(arch), "older than 180 days") || !strings.Contains(string(arch), "same as a newer lesson") {
		t.Fatalf("archive: %s", arch)
	}
	if Workspaces(dir)[0] != "api" || len(Workspaces(dir)) != 1 {
		t.Fatalf("workspaces: %v", Workspaces(dir))
	}
}

func TestDreamKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < 5; i++ {
		_ = Add(dir, "w", Lesson{At: now.AddDate(0, 0, -i), Source: "s", Text: []string{"alpha beta gamma", "delta epsilon zeta", "eta theta iota", "kappa lambda mu", "nu xi omicron"}[i]})
	}
	rep, _ := Dream(dir, "w", now, DreamOptions{Keep: 3, Similar: 0.8})
	if rep.After != 3 || List(dir, "w")[0].Text != "alpha beta gamma" {
		t.Fatalf("%+v %+v", rep, List(dir, "w"))
	}
}

func TestDreamDue(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if DreamDue(dir, now, 7*24*time.Hour) {
		t.Fatal("nothing to dream about")
	}
	_ = Add(dir, "w", Lesson{Source: "s", Text: "x y z"})
	if !DreamDue(dir, now, 7*24*time.Hour) {
		t.Fatal("first pass is due")
	}
	_, _ = DreamAll(dir, now, DefaultDream)
	if DreamDue(dir, now.Add(24*time.Hour), 7*24*time.Hour) {
		t.Fatal("a week between passes")
	}
	if !DreamDue(dir, now.Add(8*24*time.Hour), 7*24*time.Hour) {
		t.Fatal("due after a week")
	}
}
