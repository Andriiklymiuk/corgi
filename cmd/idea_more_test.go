package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
)

func TestLinkHoldersSignThePageOffAndTheLastOneApprovesIt(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	id, err := idea.Create(dir, "", "Checkout", "# Checkout\n", idea.Meta{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	two := 2
	if _, err := setIdea(dir, id.Meta.Slug, ideaChanges{Needed: &two}); err != nil {
		t.Fatal(err)
	}
	open := ideaOpenHandler("secret", "")
	base := "/idea/" + id.Meta.Slug
	k := "?k=" + id.Meta.Key
	var res approveResult
	rec := post(open, base+"/approve"+k, `{"by":"max"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != 200 || res.Approved || len(res.Approvers) != 1 {
		t.Fatalf("one of two: %d %s", rec.Code, rec.Body)
	}
	rec = post(open, base+"/approve"+k, `{"by":"ana"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if !res.JustApproved || res.Idea.Status != idea.Approved {
		t.Fatalf("two of two: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), `"key"`) || strings.Contains(rec.Body.String(), `"source"`) {
		t.Fatalf("a link holder never sees the key field or a path: %s", rec.Body)
	}
	if rec := post(open, base+"/approve"+k, `{}`); rec.Code == 200 {
		t.Fatal("a sign-off needs a name")
	}
	if rec := post(open, base+"/approve"+k, `{"by":"ana","off":true}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"draft"`) {
		t.Fatalf("taken back: %s", rec.Body)
	}
	if rec := post(open, base+"/decide"+k, `{"text":"ship it"}`); rec.Code != 403 {
		t.Fatalf("a link holder resolves threads but does not reword decisions: %d", rec.Code)
	}
	if _, err := setIdea(dir, id.Meta.Slug, ideaChanges{BuildWhenApproved: "maybe"}); err == nil {
		t.Fatal("on or off")
	}
}

func TestTheAgentDrawsAMockupThatIsServedInASandbox(t *testing.T) {
	dir := phoneBoard(t, false)
	rang := quietIdeaNotify(t)
	var asked string
	was := runIdeaPrint
	runIdeaPrint = func(_ context.Context, _, _, system, prompt string) (string, error) {
		asked = prompt
		return "Here it is:\n```html\n<!doctype html><html><body><button>Pay</button><script>fetch('/x')</script></body></html>\n```", nil
	}
	t.Cleanup(func() { runIdeaPrint = was })
	id, _ := idea.Create(dir, "", "Checkout", "# Checkout\n\n## Screens\n\nCart, pay.\n", idea.Meta{}, time.Now())
	open := ideaOpenHandler("secret", "")
	base := "/idea/" + id.Meta.Slug
	k := "?k=" + id.Meta.Key
	if rec := ideaGet(open, base+"/mock"+k); rec.Code != 404 {
		t.Fatalf("nothing drawn yet: %d", rec.Code)
	}
	if rec := post(open, base+"/mock"+k, `{}`); rec.Code != 403 {
		t.Fatalf("drawing costs the owner, not the link holder: %d", rec.Code)
	}
	if err := mockIdea(dir, id.Meta.Slug, "", "- max: make the pay button bigger\n"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "Cart, pay.") || !strings.Contains(asked, "make the pay button bigger") {
		t.Fatalf("the page and the notes go to the designer: %q", asked)
	}
	data, _ := os.ReadFile(filepath.Join(id.Dir, idea.FilesDir, idea.MockName))
	if !strings.HasPrefix(string(data), "<!doctype html>") || strings.Contains(string(data), "```") {
		t.Fatalf("only the HTML is kept: %q", data)
	}
	rec := ideaGet(open, base+"/mock"+k)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("served sandboxed: %d %v", rec.Code, rec.Header())
	}
	got, _ := idea.Load(dir, id.Meta.Slug)
	if got.Meta.Mock != idea.MockName || got.Meta.Mocking || got.Meta.MockAt.IsZero() {
		t.Fatalf("meta: %+v", got.Meta)
	}
	if len(*rang) == 0 || !strings.Contains((*rang)[len(*rang)-1], "mockup") {
		t.Fatalf("rang: %v", *rang)
	}
	if _, err := mockHTML("I could not draw it."); err == nil {
		t.Fatal("no HTML, no mockup")
	}
}

func TestOnlyCommentsOnTheMockupGoIntoTheNextDrawing(t *testing.T) {
	cs := []idea.Comment{
		{By: "max", Text: "bigger pay button", Anchor: &idea.Anchor{Quote: mockHeading, Heading: mockHeading}, Replies: []idea.Reply{{By: idea.Agent, Text: "ok"}, {By: "ana", Text: "and green"}}},
		{By: "max", Text: "Apple Pay first", Anchor: &idea.Anchor{Quote: "Card first"}},
	}
	got := mockNotes(cs)
	if !strings.Contains(got, "bigger pay button") || !strings.Contains(got, "and green") || strings.Contains(got, "Apple Pay") || strings.Contains(got, "ok") {
		t.Fatalf("notes: %q", got)
	}
}

func TestATeamTemplateShapesTheNewPage(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ideaTemplatesDir), 0o700)
	os.WriteFile(filepath.Join(repo, ideaTemplatesDir, "prd.md"), []byte("## Problem\nwho hurts\n## Metrics\nwhat moves\n"), 0o600)
	os.WriteFile(filepath.Join(repo, ideaTemplatesDir, "spec.md"), []byte("## Shadow\n"), 0o600)
	t.Chdir(repo)
	rows := ideaTemplates(ideaTemplateRoots(""))
	if len(rows) != len(idea.Types)+1 || rows[len(rows)-1].Type != "prd" {
		t.Fatalf("built-ins first, a built-in name is never shadowed: %+v", rows)
	}
	var soul, prompt string
	was := runIdeaPrint
	runIdeaPrint = func(_ context.Context, _, _, system, p string) (string, error) {
		soul, prompt = system, p
		return `{"name":"Loyalty","tags":["loyalty"],"summary":"Points.","markdown":"# Loyalty\n\n## Problem\n\nx\n\n## Metrics\n\ny\n","tickets":[{"title":"Add points"}]}`, nil
	}
	t.Cleanup(func() { runIdeaPrint = was })
	id, _, err := newIdea(dir, ideaRequest{Text: "loyalty points", Type: "prd", Model: "sonnet"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if id.Meta.Type != "prd" || !strings.Contains(soul, "## Metrics\nwhat moves") || !strings.Contains(prompt, "Write the prd for: loyalty points") {
		t.Fatalf("type %q soul %q prompt %q", id.Meta.Type, soul, prompt)
	}
	id, _, _ = newIdea(dir, ideaRequest{Text: "x", Type: "nosuch", Model: "sonnet"}, time.Now())
	if id.Meta.Type != idea.TypeIdea {
		t.Fatalf("no template, the default page: %q", id.Meta.Type)
	}
}

func TestTheDigestNamesPagesThatWaitOnAPerson(t *testing.T) {
	dir := phoneBoard(t, false)
	now := time.Now()
	id, _ := idea.Create(dir, "", "Checkout", "# Checkout\n", idea.Meta{Name: "Checkout", Needed: 2}, now)
	_, _ = idea.Update(dir, id.Meta.Slug, func(id *idea.Idea) error {
		c, _ := id.AddComment(idea.KindComment, "max", "why?", nil, nil, nil, now)
		_, _ = id.Reply(c.ID, idea.Agent, "because", false, nil, nil, now)
		_, _ = id.AddComment(idea.KindProposal, idea.Agent, "add a receipt", nil, nil, nil, now)
		_, err := id.Approve("max", false, now)
		return err
	})
	_, _ = idea.Create(dir, "", "Quiet", "# Quiet\n", idea.Meta{}, now)
	got := ideaDigestLines(dir)
	if len(got) != 1 || got[0] != "💡 Checkout: 1 answered, to close, 1 proposal(s) to take or leave, signed off 1 of 2" {
		t.Fatalf("digest: %q", got)
	}
}
