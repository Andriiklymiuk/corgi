package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
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

func TestASessionSharesTextButNeverAPath(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	s := newCorgiMCPServer("0.0.0-test")
	c, err := client.NewInProcessClient(s)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx := context.Background()
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	call := func(args map[string]any) (map[string]any, bool) {
		t.Helper()
		req := mcp.CallToolRequest{}
		req.Params.Name = "corgi_idea_share"
		req.Params.Arguments = args
		res, err := c.CallTool(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		text := ""
		for _, item := range res.Content {
			if tc, ok := item.(mcp.TextContent); ok {
				text += tc.Text
			} else {
				text += fmt.Sprint(item)
			}
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(text), &out)
		return out, res.IsError
	}
	if _, bad := call(map[string]any{}); !bad {
		t.Fatal("nothing to share is an error")
	}
	out, bad := call(map[string]any{"markdown": "# Pricing\n\nTwo plans.\n", "type": "rfc"})
	if bad || out["created"] != true || out["open"] != true {
		t.Fatalf("text becomes a page: %v", out)
	}
	slug := out["slug"].(string)
	page, _ := idea.Load(dir, slug)
	if page.Meta.Type != idea.TypeRFC || page.Meta.Source != "" || page.Meta.Name != "Pricing" {
		t.Fatalf("an rfc named by its heading, from no file: %+v", page.Meta)
	}
	spec := filepath.Join(t.TempDir(), "x.md")
	_ = os.WriteFile(spec, []byte("# X\n"), 0o600)
	if _, bad := call(map[string]any{"slug": spec}); !bad {
		t.Fatal("a path is not a slug")
	}
	if out, bad := call(map[string]any{"slug": slug, "rotate": true}); bad || out["open"] != true {
		t.Fatalf("rotate: %v", out)
	}
	if out, bad := call(map[string]any{"slug": slug, "off": true}); bad || out["open"] != false {
		t.Fatalf("off: %v", out)
	}
	if _, bad := call(map[string]any{"markdown": "# Y\n", "workspace": "nope"}); !bad {
		t.Fatal("an unknown workspace is refused")
	}
}

func TestPicturesAndVideosOnThePage(t *testing.T) {
	html := renderIdeaHTML("![a](https://cdn.example/v.mp4)\n\n![b](https://cdn.example/p.png)\n\n![c](shots/p%20one.png)\n\n![d](notes.txt)\n\n![e](javascript:alert(1))\n")
	for _, want := range []string{
		`<video src="https://cdn.example/v.mp4" controls playsinline`,
		`<img src="https://cdn.example/p.png" alt="b" loading="lazy" />`,
		`<img data-file="p-one.png" alt="c"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("want %s in %s", want, html)
		}
	}
	if strings.Contains(html, "javascript:") {
		t.Fatalf("an unsafe link never renders: %s", html)
	}
	if v, i := mediaKind("a/b.MOV?x=1"); !v || i {
		t.Fatal("a .MOV is a video")
	}
}

func TestShareSaysWhatHappened(t *testing.T) {
	cases := []struct {
		r      shareResult
		copied bool
		want   []string
	}{
		{shareResult{Name: "A", Created: true, Open: true, Link: "http://x/idea/a?k=1", Hint: "works on this Mac only", Media: []string{"m.mp4"}, Skipped: []string{"big.mov (over 40 MB)"}}, true, []string{"a new page", "copied", "this Mac only", "1 picture(s)", "left out: big.mov"}},
		{shareResult{Name: "B", Updated: true, Open: true, Link: "https://x/idea/b?k=1", Public: true}, false, []string{"same link", "anyone with the link"}},
		{shareResult{Name: "C"}, false, []string{"closed"}},
	}
	for _, c := range cases {
		out := captureStdout(t, func() { c.r.print(c.copied) })
		for _, w := range c.want {
			if !strings.Contains(out, w) {
				t.Fatalf("want %q in %q", w, out)
			}
		}
	}
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, ".git"), 0o700)
	deep := filepath.Join(root, "a", "b")
	_ = os.MkdirAll(deep, 0o700)
	if repoRootOf(deep) != root {
		t.Fatal("the checkout a folder sits in")
	}
	lone := t.TempDir()
	if repoRootOf(lone) != lone {
		t.Fatal("no checkout: the folder itself")
	}
}

func TestAFileWithoutAHeadingIsNamedByItsFile(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	folder := t.TempDir()
	empty := filepath.Join(folder, "empty.md")
	_ = os.WriteFile(empty, []byte("\n\n"), 0o600)
	if _, err := shareIdea(dir, shareRequest{Target: empty}, time.Now()); err == nil {
		t.Fatal("an empty file is no page")
	}
	plain := filepath.Join(folder, "pricing-notes.md")
	_ = os.WriteFile(plain, []byte("just words\n"), 0o600)
	res, err := shareIdea(dir, shareRequest{Target: plain, Type: "hld"}, time.Now())
	if err != nil || res.Name != "pricing-notes" {
		t.Fatalf("named by the file: %+v %v", res, err)
	}
	if page, _ := idea.Load(dir, res.Slug); page.Meta.Type != idea.TypeHLD {
		t.Fatalf("the type asked for: %s", page.Meta.Type)
	}
	if _, err := shareIdea(dir, shareRequest{Target: folder}, time.Now()); err == nil {
		t.Fatal("a folder is no page")
	}
}
