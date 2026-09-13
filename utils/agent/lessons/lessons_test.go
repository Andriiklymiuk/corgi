package lessons

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLessonsAreOneLineEachAndNeverTwice(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	if err := Add(dir, "acme/api", Lesson{At: at, Source: "review acme/api#7 (dan)", Text: "line 40 is wrong\nmore words"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(dir, "acme/api", Lesson{At: at, Source: "review acme/api#7 (dan)", Text: "line 40 is wrong"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(dir, "acme/api", Lesson{At: at.AddDate(0, 0, 1), Source: "done-when", Text: "go test ./... stayed red 3 times on feat/x"}); err != nil {
		t.Fatal(err)
	}
	if err := Add(dir, "acme/api", Lesson{Text: "   "}); err == nil {
		t.Fatal("blank is not a lesson")
	}
	got := List(dir, "acme/api")
	if len(got) != 2 || got[0].Text != "line 40 is wrong" || got[0].Source != "review acme/api#7 (dan)" || got[0].At.Format("2006-01-02") != "2026-09-13" || got[1].Source != "done-when" {
		t.Fatalf("%+v", got)
	}
	raw, _ := os.ReadFile(Path(dir, "acme/api"))
	if !strings.HasPrefix(string(raw), "# Lessons · acme/api") || !strings.Contains(string(raw), "- 2026-09-13 · review acme/api#7 (dan): line 40 is wrong\n") {
		t.Fatalf("file:\n%s", raw)
	}
	if !strings.HasSuffix(Path(dir, "acme/api"), "/lessons/acme_api.md") {
		t.Fatal(Path(dir, "acme/api"))
	}
	// A line a person typed without the date is kept as text.
	f, _ := os.OpenFile(Path(dir, "acme/api"), os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("- never mock the database in api tests\n")
	f.Close()
	got = List(dir, "acme/api")
	if len(got) != 3 || got[2].Text != "never mock the database in api tests" || got[2].Source != "" {
		t.Fatalf("%+v", got[2])
	}
	if List(dir, "nothing") != nil {
		t.Fatal("no file, no lessons")
	}
}
