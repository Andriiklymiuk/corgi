package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
)

func TestALinkIsReadAsAPageAndAnythingElseIsNot(t *testing.T) {
	r, ok := parseIdeaLink("https://blue-fox.trycloudflare.com/idea/checkout?k=abc")
	if !ok || r.Slug != "checkout" || r.Key != "abc" || r.url("comment") != "https://blue-fox.trycloudflare.com/idea/checkout/comment?k=abc" {
		t.Fatalf("link: %+v %s", r, r.url("comment"))
	}
	for _, s := range []string{"checkout", "docs/spec.md", "https://example.com/other/checkout", "https://example.com/idea/../etc", "ftp://h/idea/x"} {
		if _, ok := parseIdeaLink(s); ok {
			t.Fatalf("%q is not a page link", s)
		}
	}
}

func TestAnAgentElsewhereReadsCommentsAndFollowsThroughTheLink(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	was := ideaAutoAnswerHere
	ideaAutoAnswerHere = false
	t.Cleanup(func() { ideaAutoAnswerHere = was })
	id, err := idea.Create(dir, "", "Checkout HLD", "# Checkout HLD\n\n## Data\n\nOrders live in Postgres.\n", idea.Meta{Type: idea.TypeHLD}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(ideaOpenHandler("secret", ""))
	defer srv.Close()
	link := srv.URL + "/idea/" + id.Meta.Slug + "?k=" + id.Meta.Key

	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	shell, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(shell), `/idea/`+id.Meta.Slug+`/agent?k=`+id.Meta.Key) {
		t.Fatal("the shell tells a fetching agent where the text is")
	}

	r, ok := parseIdeaLink(link)
	if !ok {
		t.Fatal(link)
	}
	text, err := r.do(http.MethodGet, "agent", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# Checkout HLD", "Orders live in Postgres.", "POST " + srv.URL + "/idea/" + id.Meta.Slug + "/comment?k=", "version 1"} {
		if !strings.Contains(string(text), want) {
			t.Fatalf("agent text lacks %q:\n%s", want, text)
		}
	}
	if _, err := (ideaRemote{Base: r.Base, Slug: r.Slug, Key: "wrong"}).do(http.MethodGet, "agent", nil); err == nil {
		t.Fatal("the key is still the lock")
	}

	before, err := r.doc()
	if err != nil {
		t.Fatal(err)
	}
	sketch := filepath.Join(t.TempDir(), "erd.png")
	os.WriteFile(sketch, []byte("\x89PNG fake"), 0o600)
	if _, err := remoteIdeaComment(r, remoteComment{Text: "Why not one table per tenant?", By: "maria-agent", Quote: "Postgres", Files: []string{sketch}}); err != nil {
		t.Fatal(err)
	}
	got, _ := idea.Load(dir, id.Meta.Slug)
	c := got.Comments.Comments[len(got.Comments.Comments)-1]
	if c.By != "maria-agent" || c.Anchor == nil || c.Anchor.Heading != "Data" || len(c.Files) != 1 {
		t.Fatalf("the comment landed on the words, with its file: %+v", c)
	}
	mid, err := r.doc()
	if err != nil {
		t.Fatal(err)
	}
	if ev := followChanges(before, mid); len(ev) != 1 || ev[0].line() != "comment 1 by maria-agent on “Postgres”: Why not one table per tenant?" {
		t.Fatalf("a new comment: %+v", ev)
	}

	_, _ = idea.Update(dir, id.Meta.Slug, func(id *idea.Idea) error {
		_, _ = id.Reply(c.ID, idea.Agent, "One schema, tenant_id on every row: simpler migrations.", true, nil, nil, time.Now())
		id.ChangeBy, id.ChangeNote = idea.Agent, "answered comment(s) 1"
		return id.SetDoc("# Checkout HLD\n\n## Data\n\nOrders live in Postgres, tenant_id on every row.\n", time.Now())
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch := make(chan struct{}, 1)
	go r.waitForChange(ctx, ch)
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal("the stream never said it changed")
	}
	after, err := r.doc()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, e := range followChanges(mid, after) {
		lines = append(lines, e.line())
	}
	all := strings.Join(lines, "\n")
	for _, want := range []string{"version 2 by agent: answered comment(s) 1", "reply under 1 by agent: One schema", "comment 1 is fixed"} {
		if !strings.Contains(all, want) {
			t.Fatalf("follow lacks %q:\n%s", want, all)
		}
	}
	if len(followChanges(after, after)) != 0 {
		t.Fatal("nothing moved, nothing said")
	}
}
