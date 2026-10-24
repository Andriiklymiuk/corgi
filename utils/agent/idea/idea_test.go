package idea

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSlugifyMakesADirectoryNameOutOfAnyTitle(t *testing.T) {
	cases := map[string]string{
		"Coffee shop scheduling app": "coffee-shop-scheduling-app",
		"  BrewShift!! (v2) ":        "brewshift-v2",
		"Ünïcode café":               "n-code-caf",
		"":                           "idea",
		"a-very-long-title-that-goes-on-and-on-and-on-and-on-forever-and-ever": "a-very-long-title-that-goes-on-and-on-and-on-and",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
		if !ValidSlug(Slugify(in)) {
			t.Errorf("Slugify(%q) is not a valid slug", in)
		}
	}
}

func TestAPageIsCreatedCommentedAnsweredAndResolved(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	id, err := Create(dir, "", "Coffee shop scheduling", "# BrewShift\n\n## Infra\n\nWe use sqlite.\n", Meta{Name: "BrewShift", Tags: []string{"scheduling"}, Auto: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if id.Meta.Slug != "brewshift" || id.Meta.Key == "" || id.Meta.Status != Draft {
		t.Fatalf("meta: %+v", id.Meta)
	}
	if _, err := os.Stat(filepath.Join(Root(dir), ChangedName)); err != nil {
		t.Fatalf("the change marker is written: %v", err)
	}
	// A second idea with the same name gets a number.
	twin, err := Create(dir, "", "Coffee shop scheduling", "x", Meta{Name: "BrewShift"}, now)
	if err != nil || twin.Meta.Slug != "brewshift-2" {
		t.Fatalf("twin: %+v %v", twin, err)
	}

	loaded, err := Load(dir, "brewshift")
	if err != nil {
		t.Fatal(err)
	}
	c, err := loaded.AddComment(KindComment, "andrii", "use postgres", &Anchor{Quote: "We use sqlite.", Heading: "Infra"}, nil, []string{"https://postgres.org", "javascript:alert(1)"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != 1 || c.Status != StatusOpen || len(c.Links) != 1 {
		t.Fatalf("comment: %+v", c)
	}
	if got := loaded.ForAgent(); len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("the agent sees the open comment: %+v", got)
	}
	if err := loaded.Save(dir); err != nil {
		t.Fatal(err)
	}

	// The agent's reply with a change marks it fixed; a person's reply reopens.
	again, _ := Load(dir, "brewshift")
	if _, err := again.Reply(1, Agent, "Switched to Postgres.", true, nil, nil, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := again.Find(1); got.Status != StatusFixed {
		t.Fatalf("after the agent's change: %s", got.Status)
	}
	if len(again.ForAgent()) != 0 {
		t.Fatal("nothing waits on the agent now")
	}
	if _, err := again.Reply(1, "andrii", "and pgbouncer?", false, nil, nil, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := again.Find(1); got.Status != StatusOpen || len(again.ForAgent()) != 1 {
		t.Fatalf("a person's follow-up reopens: %s", got.Status)
	}
	if _, err := again.Reply(1, Agent, "Yes, in front of it.", false, nil, nil, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := again.Find(1); got.Status != StatusAnswered {
		t.Fatalf("a reply without a change answers: %s", got.Status)
	}
	if _, err := again.SetStatus(1, StatusResolved, now); err != nil {
		t.Fatal(err)
	}
	if _, err := again.SetStatus(1, "weird", now); err == nil {
		t.Fatal("an unknown status is refused")
	}
	if again.Open() != 0 || again.Counts()[StatusResolved] != 1 {
		t.Fatalf("counts: %v", again.Counts())
	}

	// A proposal by the agent waits on a person, not on the agent.
	p, _ := again.AddComment(KindProposal, Agent, "Add a waitlist.", nil, nil, nil, now)
	if len(again.ForAgent()) != 0 {
		t.Fatal("an unanswered proposal is the agent's own")
	}
	if _, err := again.Reply(p.ID, "andrii", "yes please", false, nil, nil, now); err != nil {
		t.Fatal(err)
	}
	if got := again.ForAgent(); len(got) != 1 || got[0].ID != p.ID {
		t.Fatalf("an accepted proposal waits on the agent: %+v", got)
	}
	if err := again.Save(dir); err != nil {
		t.Fatal(err)
	}
	list := List(dir)
	if len(list) != 2 || list[0].Slug != "brewshift" {
		t.Fatalf("list, newest change first: %+v", list)
	}
}

func TestUpdateHoldsTheLockAndFilesStayInTheirFolder(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if _, err := Create(dir, "demo", "Demo", "# Demo\n", Meta{}, now); err != nil {
		t.Fatal(err)
	}
	id, err := Update(dir, "demo", func(id *Idea) error {
		_, err := id.AddComment(KindComment, "", "hello", nil, nil, nil, now)
		return err
	})
	if err != nil || len(id.Comments.Comments) != 1 || id.Comments.Comments[0].By != "someone" {
		t.Fatalf("update: %+v %v", id, err)
	}
	if _, err := Update(dir, "nope", func(*Idea) error { return nil }); err == nil {
		t.Fatal("an unknown idea is refused")
	}
	if _, err := Update(dir, "../etc", func(*Idea) error { return nil }); err == nil {
		t.Fatal("a path is not a slug")
	}
	f, err := id.AddFile("../../mock up.PNG", []byte("png"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "mock-up.PNG" || filepath.Dir(f.Path) != FilesDir {
		t.Fatalf("file: %+v", f)
	}
	if id.FilePath(filepath.Base(f.Path)) == "" || id.FilePath("../meta.json") != "" || id.FilePath("nothere") != "" {
		t.Fatal("only this idea's files are served")
	}
	if _, err := id.AddFile("x", nil, ""); err == nil {
		t.Fatal("an empty file is refused")
	}
}

func TestTranscriptAndHeadingTellTheAgentWhereACommentSits(t *testing.T) {
	doc := "# Title\n\n## Infra\n\nPostgres it is.\n\n## Screens\n\nA calendar.\n"
	if got := HeadingFor(doc, "A calendar."); got != "Screens" {
		t.Fatalf("heading: %q", got)
	}
	if got := HeadingFor(doc, "nowhere"); got != "" {
		t.Fatalf("missing quote: %q", got)
	}
	cs := []Comment{{ID: 3, Kind: KindComment, By: "andrii", Status: StatusOpen, Text: "weekly too", Anchor: &Anchor{Quote: "A calendar.", Heading: "Screens"},
		Replies: []Reply{{By: Agent, Text: "Added a week view."}}, Links: []string{"https://x"}}}
	tr := Transcript(cs)
	for _, want := range []string{"### Comment 3", "Under: Screens", `Highlighted: "A calendar."`, "weekly too", "agent replied: Added a week view.", "Link: https://x"} {
		if !contains(tr, want) {
			t.Errorf("transcript lacks %q:\n%s", want, tr)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestTheSmallHelpersAndTheLimits(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	id, err := Create(dir, "", "Limits", "# L\n", Meta{Type: "poem", Tickets: []Ticket{{Title: "a"}, {Title: "b"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if id.Meta.Type != TypeIdea || !ValidType(TypeHLD) || ValidType("poem") {
		t.Fatalf("types: %+v", id.Meta)
	}
	if tk, ok := id.Meta.FindTicket(2); !ok || tk.Title != "b" {
		t.Fatalf("tickets are numbered: %+v", id.Meta.Tickets)
	}
	if _, ok := id.Meta.FindTicket(9); ok {
		t.Fatal("no ticket 9")
	}
	if err := id.SetDoc(string(make([]byte, MaxDoc+1)), now); err == nil {
		t.Fatal("a page has a size")
	}
	if err := id.SetDoc("# New\n", now); err != nil || id.Doc != "# New\n" {
		t.Fatalf("set: %v", err)
	}
	if _, err := Create(dir, "Bad Slug", "x", "y", Meta{}, now); err == nil {
		t.Fatal("a slug is lowercase")
	}
	if _, err := Create(dir, "", "  ", "y", Meta{}, now); err == nil {
		t.Fatal("a title is needed")
	}
	if _, err := Create(dir, "big", "x", string(make([]byte, MaxDoc+1)), Meta{}, now); err == nil {
		t.Fatal("a page has a size at birth too")
	}
	if NewKey() == NewKey() || len(NewKey()) != 18 {
		t.Fatal("keys differ")
	}
	if clip("héllo", 2) != "hé" || firstNonEmpty(" ", "b") != "b" || firstNonEmpty("a", "b") != "a" {
		t.Fatal("clip and firstNonEmpty")
	}
	if _, err := id.AddComment(KindComment, "x", string(make([]byte, MaxComment+1)), nil, nil, nil, now); err == nil {
		t.Fatal("a comment has a size")
	}
	c, _ := id.AddComment(KindComment, "x", "hi", &Anchor{Quote: "  "}, nil, nil, now)
	if c.Anchor != nil {
		t.Fatal("an empty quote is no anchor")
	}
	if _, err := id.Reply(c.ID, "y", "", false, nil, nil, now); err == nil {
		t.Fatal("a reply needs words")
	}
	if _, err := id.Reply(c.ID, "y", string(make([]byte, MaxComment+1)), false, nil, nil, now); err == nil {
		t.Fatal("a reply has a size")
	}
	if _, err := id.Reply(77, "y", "x", false, nil, nil, now); err == nil || !errors.Is(err, ErrNoComment) {
		t.Fatalf("no comment 77: %v", err)
	}
	if _, err := id.SetStatus(77, StatusOpen, now); err == nil || !errors.Is(err, ErrNoComment) {
		t.Fatalf("no comment 77: %v", err)
	}
	// Files: a count and a size, together.
	for i := 0; i < MaxFiles; i++ {
		if _, err := id.AddFile("f.txt", []byte("x"), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := id.AddFile("one-more.txt", []byte("x"), ""); err == nil || !strings.Contains(err.Error(), "enough files") {
		t.Fatalf("the count holds: %v", err)
	}
	if _, err := id.AddFile("huge.bin", make([]byte, MaxFile+1), ""); err == nil {
		t.Fatal("one file has a size")
	}
	n, bytes := id.filesHeld()
	if n != MaxFiles || bytes != int64(MaxFiles) {
		t.Fatalf("held: %d %d", n, bytes)
	}
	// A broken meta.json is an error, not a page.
	if err := os.WriteFile(filepath.Join(id.Dir, MetaName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir, "limits"); err == nil {
		t.Fatal("broken meta")
	}
	if _, err := Load(dir, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := Load(dir, "../x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not a slug: %v", err)
	}
	if got := List(dir); len(got) != 0 {
		t.Fatalf("a broken page is left out of the list: %+v", got)
	}
}
