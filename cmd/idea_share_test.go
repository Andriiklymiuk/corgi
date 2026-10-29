package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
)

func TestSharingAFileMakesOnePageAndKeepsItsLink(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	spec := filepath.Join(t.TempDir(), "checkout.md")
	if err := os.WriteFile(spec, []byte("# Faster checkout\n\nOne tap pay.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, code := runIdea(t, "share", spec, "--json")
	first := ideaJSONOut(t, out)
	if code != 0 || first["created"] != true || first["open"] != true || first["name"] != "Faster checkout" {
		t.Fatalf("first share: %d %s", code, out)
	}
	if first["public"] != false || !strings.Contains(first["hint"].(string), "corgi agent up") {
		t.Fatalf("no tunnel, so the link is local and says so: %s", out)
	}
	slug := first["slug"].(string)
	page, err := idea.Load(dir, slug)
	if err != nil || page.Meta.Type != idea.TypeSpec || page.Meta.Source != spec || page.Doc != "# Faster checkout\n\nOne tap pay.\n" {
		t.Fatalf("page: %v %+v", err, page)
	}
	key := page.Meta.Key

	out, _ = runIdea(t, "share", spec, "--json")
	again := ideaJSONOut(t, out)
	if again["slug"] != slug || again["created"] != nil || again["updated"] != nil {
		t.Fatalf("an unchanged file is the same page: %s", out)
	}

	_ = os.WriteFile(spec, []byte("# Faster checkout\n\nOne tap pay, and Apple Pay.\n"), 0o600)
	out, code = runIdea(t, "share", spec)
	if code != 0 || !strings.Contains(out, "updated from the file, same link") {
		t.Fatalf("edited file: %d %s", code, out)
	}
	page, _ = idea.Load(dir, slug)
	if page.Meta.Key != key || len(page.Meta.Revisions) != 1 || !strings.Contains(page.Doc, "Apple Pay") {
		t.Fatalf("the page moved on, the key stayed: %+v", page.Meta)
	}
	if len(idea.List(dir)) != 1 {
		t.Fatalf("one page, not two: %d", len(idea.List(dir)))
	}

	if _, code := runIdea(t, "share", filepath.Join(t.TempDir(), "nope.md")); code != 1 {
		t.Fatalf("a missing file fails: %d", code)
	}
}

func TestSharingFromOutsideIsTheOwnersAlone(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	was := ideaAutoAnswerHere
	ideaAutoAnswerHere = false
	t.Cleanup(func() { ideaAutoAnswerHere = was })
	spec := filepath.Join(t.TempDir(), "secret-plan.md")
	_ = os.WriteFile(spec, []byte("# Plan\n\ntext\n"), 0o600)
	res, err := shareIdea(dir, shareRequest{Target: spec, By: "me"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	page, _ := idea.Load(dir, res.Slug)
	open := ideaOpenHandler("secret", "")
	base := "/idea/" + res.Slug

	rec := ideaGet(open, base+"/doc?k="+page.Meta.Key)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), spec) || strings.Contains(rec.Body.String(), `"source"`) {
		t.Fatalf("a link holder never sees where the file sits: %d %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	open(rec, httptest.NewRequest(http.MethodPost, base+"/share?k="+page.Meta.Key, strings.NewReader(`{"rotate":true}`)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a link holder cannot rotate the link: %d %s", rec.Code, rec.Body)
	}

	rec = post(launchIdeaHandler, "/launch/idea", `{"do":"share","slug":"`+spec+`"}`)
	if rec.Code == 200 {
		t.Fatalf("a device cannot turn a path on this Mac into a page: %s", rec.Body)
	}

	rec = post(launchIdeaHandler, "/launch/idea", `{"do":"share","slug":"`+res.Slug+`","off":true}`)
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || got["open"] != false {
		t.Fatalf("the owner's phone closes it: %d %s", rec.Code, rec.Body)
	}
	if rec := ideaGet(open, base+"/doc?k="+page.Meta.Key); rec.Code != 403 {
		t.Fatalf("a closed page answers no key: %d", rec.Code)
	}
}

func TestASharedFileTakesItsVideosAndGetsTheAgentsChangesBack(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	repo := t.TempDir()
	_ = os.Mkdir(filepath.Join(repo, ".git"), 0o700)
	_ = os.MkdirAll(filepath.Join(repo, "docs", "media"), 0o700)
	_ = os.WriteFile(filepath.Join(repo, "docs", "media", "demo.mp4"), []byte("not really a video"), 0o600)
	_ = os.WriteFile(filepath.Join(repo, "docs", "media", "notes.txt"), []byte("not media"), 0o600)
	outside := filepath.Join(t.TempDir(), "secret.png")
	_ = os.WriteFile(outside, []byte("png"), 0o600)
	spec := filepath.Join(repo, "docs", "spec.md")
	text := "# Demo\n\n![the flow](media/demo.mp4)\n\n![x](" + outside + ")\n\n![y](../../" + filepath.Base(filepath.Dir(outside)) + "/secret.png)\n\n[notes](media/notes.txt)\n"
	_ = os.WriteFile(spec, []byte(text), 0o600)

	res, err := shareIdea(dir, shareRequest{Target: spec, By: "me"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Media) != 1 || res.Media[0] != "media/demo.mp4" {
		t.Fatalf("only the video inside the repository goes along: %+v %+v", res.Media, res.Skipped)
	}
	page, _ := idea.Load(dir, res.Slug)
	if page.FilePath("demo.mp4") == "" || page.FilePath("secret.png") != "" || page.FilePath("notes.txt") != "" {
		t.Fatal("the page holds the video and nothing else")
	}
	html := renderIdeaHTML(page.Doc)
	if !strings.Contains(html, `<video data-file="demo.mp4" controls playsinline`) {
		t.Fatalf("the video plays on the page: %s", html)
	}

	_, err = idea.Update(dir, res.Slug, func(id *idea.Idea) error { return id.SetDoc(text+"\nAdded by the agent.\n", time.Now()) })
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(spec); !strings.Contains(string(got), "Added by the agent.") {
		t.Fatalf("the file follows the page: %s", got)
	}

	_ = os.WriteFile(spec, []byte("# Demo\n\nmy own edit\n"), 0o600)
	_, _ = idea.Update(dir, res.Slug, func(id *idea.Idea) error { return id.SetDoc("# Demo\n\nthe agent again\n", time.Now()) })
	if got, _ := os.ReadFile(spec); string(got) != "# Demo\n\nmy own edit\n" {
		t.Fatalf("a file edited on its own is never overwritten: %s", got)
	}
}

func TestOpenTakesAMarkdownFile(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	spec := filepath.Join(t.TempDir(), "plan.md")
	_ = os.WriteFile(spec, []byte("# Plan\n"), 0o600)
	_, _ = runIdea(t, "open", spec, "--print")
	list := idea.List(dir)
	if len(list) != 1 || list[0].Source != spec || list[0].Type != idea.TypeSpec {
		t.Fatalf("the file became a page: %+v", list)
	}
	if _, code := runIdea(t, "open", filepath.Join(t.TempDir(), "nope.md")); code != 2 {
		t.Fatalf("neither a page nor a file: %d", code)
	}
}
