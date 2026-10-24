package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/pflag"
)

// runIdea runs `corgi idea …` the way a person would. The exit is a panic
// the runner catches, so a command stops where the real one would, and
// what it printed to stdout comes back with the exit code.
type ideaExit struct{ code int }

func runIdea(t *testing.T, args ...string) (out string, code int) {
	t.Helper()
	prev := osExit
	osExit = func(c int) { panic(ideaExit{c}) }
	t.Cleanup(func() { osExit = prev })
	rootCmd.SetArgs(append([]string{"idea"}, args...))
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	t.Cleanup(resetCommandFlags)
	resetCommandFlags()
	// A repeatable flag reset to its default "[]" would carry "[]" as a value.
	for _, name := range []string{"link", "file"} {
		if f := ideaCommentCmd.Flags().Lookup(name); f != nil {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				_ = sv.Replace(nil)
			}
			f.Changed = false
		}
	}
	activeLogFilter = logStreamFilter{}
	utils.JSONOutput = false
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() { var b strings.Builder; _, _ = io.Copy(&b, r); done <- b.String() }()
	logged := captureConsole(t, func() {
		defer func() {
			if rec := recover(); rec != nil {
				if e, ok := rec.(ideaExit); ok {
					code = e.code
					return
				}
				panic(rec)
			}
		}()
		if err := rootCmd.Execute(); err != nil {
			code = 1
		}
	})
	os.Stdout = stdout
	_ = w.Close()
	out = <-done + logged
	utils.JSONOutput = false
	return out, code
}

func ideaJSONOut(t *testing.T, out string) map[string]any {
	t.Helper()
	start := strings.Index(out, "{")
	if start < 0 {
		t.Fatalf("no JSON in %q", out)
	}
	var m map[string]any
	if err := json.NewDecoder(strings.NewReader(out[start:])).Decode(&m); err != nil {
		t.Fatalf("bad JSON %v in %q", err, out)
	}
	return m
}

func TestTheIdeaCommandsEndToEnd(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	rang := quietIdeaNotify(t)
	utils.JSONOutput = false
	t.Cleanup(func() { utils.JSONOutput = false })

	// A listening socket where the dashboard would be, so links exist.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.WriteFile(filepath.Join(dir, mcpAddrName), []byte(ln.Addr().String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, code := runIdea(t, "new", "a scheduling app for a coffee shop", "--type", "spec", "--by", "andrii")
	if code != 0 || !strings.Contains(out, "brewshift - BrewShift (spec)") || !strings.Contains(out, "TASK-1") || !strings.Contains(out, "open: http://127.0.0.1") {
		t.Fatalf("new: %d %s", code, out)
	}
	if _, code := runIdea(t, "new", "x", "--type", "poem"); code != 2 {
		t.Fatalf("an unknown type is refused: %d", code)
	}
	if _, code := runIdea(t, "new", "x", "--kind", "abacus"); code != 2 {
		t.Fatalf("an unknown kind is refused: %d", code)
	}
	if _, code := runIdea(t, "new", "x", "--workspace", "nope"); code != 2 {
		t.Fatalf("an unknown workspace is refused: %d", code)
	}
	if _, code := runIdea(t, "new", "x", "--from", filepath.Join(dir, "missing.md")); code != 2 {
		t.Fatalf("a missing file is refused: %d", code)
	}
	empty := filepath.Join(dir, "empty.md")
	_ = os.WriteFile(empty, []byte("  \n"), 0o600)
	if _, code := runIdea(t, "new", "--from", empty); code != 2 {
		t.Fatalf("an empty file is refused: %d", code)
	}
	own := filepath.Join(dir, "own.md")
	_ = os.WriteFile(own, []byte("# Rota\n\nSQLite on a single VPS.\n"), 0o600)
	out, code = runIdea(t, "new", "--from", own, "--type", "hld", "--no-tasks", "--json")
	if code != 0 {
		t.Fatalf("from: %d %s", code, out)
	}
	if got := ideaJSONOut(t, out); got["idea"].(map[string]any)["type"] != "hld" || got["tasks"] != nil {
		t.Fatalf("from json: %v", got)
	}
	// No words: the help, wherever cobra's output points in this process, and no page.
	if _, code := runIdea(t, "new"); code != 0 {
		t.Fatalf("no words shows the help: %d", code)
	}
	if got := idea.List(dir); len(got) != 2 {
		t.Fatalf("no page was written for no words: %d", len(got))
	}

	out, code = runIdea(t, "list")
	if code != 0 || !strings.Contains(out, "brewshift") || !strings.Contains(out, "brewshift-2") {
		t.Fatalf("list: %d %s", code, out)
	}
	out, _ = runIdea(t, "list", "--json")
	if got := ideaJSONOut(t, out); len(got["ideas"].([]any)) != 2 {
		t.Fatalf("list json: %v", got)
	}
	out, code = runIdea(t, "show", "brewshift")
	if code != 0 || !strings.Contains(out, "# BrewShift") || !strings.Contains(out, "### Comment 1 (proposal") {
		t.Fatalf("show: %d %s", code, out)
	}
	out, _ = runIdea(t, "show", "brewshift", "--json")
	if got := ideaJSONOut(t, out); got["html"] == nil || got["file"] == nil {
		t.Fatalf("show json: %v", got)
	}
	if _, code := runIdea(t, "show", "nope"); code != 2 {
		t.Fatalf("show nope: %d", code)
	}
	out, code = runIdea(t, "path", "brewshift")
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(out), "idea.md") {
		t.Fatalf("path: %d %s", code, out)
	}
	out, _ = runIdea(t, "path", "brewshift", "--json")
	if got := ideaJSONOut(t, out); got["dir"] == nil {
		t.Fatalf("path json: %v", got)
	}
	out, code = runIdea(t, "open", "brewshift", "--print")
	if code != 0 || !strings.Contains(out, "/idea/brewshift?k=") {
		t.Fatalf("open --print: %d %s", code, out)
	}
	out, _ = runIdea(t, "open", "brewshift", "--json")
	if got := ideaJSONOut(t, out); got["link"] == nil {
		t.Fatalf("open json: %v", got)
	}

	// Comments from the terminal: on words, with a file and a link, a reply, the agent's own.
	sketch := filepath.Join(dir, "sketch.png")
	_ = os.WriteFile(sketch, []byte("png"), 0o600)
	out, code = runIdea(t, "comment", "brewshift", "use postgres", "--quote", "SQLite on a single VPS", "--by", "maria", "--link", "https://fly.io", "--file", sketch)
	if code != 0 || !strings.Contains(out, "comment 3 on brewshift (open)") || strings.Contains(out, "waiting on the agent") {
		t.Fatalf("comment: %d %s", code, out)
	}
	if len(*rang) == 0 || !strings.Contains((*rang)[len(*rang)-1], "comment by maria") {
		t.Fatalf("it rang: %v", *rang)
	}
	out, code = runIdea(t, "comment", "brewshift", "not on the page", "--quote", "zebra", "--json")
	if code != 0 {
		t.Fatalf("comment with a lost quote: %d %s", code, out)
	}
	if got := ideaJSONOut(t, out); got["comment"].(map[string]any)["anchor"].(map[string]any)["quote"] != "zebra" {
		t.Fatalf("the words are kept even when not found: %v", got)
	}
	out, code = runIdea(t, "comment", "brewshift", "Moved Infra to Postgres.", "--reply-to", "3", "--as-agent", "--changed")
	if code != 0 || !strings.Contains(out, "reply") {
		t.Fatalf("agent reply: %d %s", code, out)
	}
	page, _ := idea.Load(dir, "brewshift")
	if c, _ := page.Find(3); c.Status != idea.StatusFixed || c.Replies[0].By != idea.Agent || !c.Replies[0].Changed {
		t.Fatalf("the agent's reply fixes: %+v", c)
	}
	if _, code := runIdea(t, "comment", "brewshift", "x", "--by", "agent"); code != 2 {
		t.Fatalf("--by agent without --as-agent: %d", code)
	}
	if _, code := runIdea(t, "comment", "brewshift", "x", "--changed"); code != 2 {
		t.Fatalf("--changed alone: %d", code)
	}
	if _, code := runIdea(t, "comment", "brewshift", "x", "--file", filepath.Join(dir, "none.png")); code != 1 {
		t.Fatalf("a missing file: %d", code)
	}
	out, code = runIdea(t, "resolve", "brewshift", "3")
	if code != 0 || !strings.Contains(out, "resolved") {
		t.Fatalf("resolve: %d %s", code, out)
	}
	out, code = runIdea(t, "reopen", "brewshift", "#3", "--json")
	if code != 0 || ideaJSONOut(t, out)["comment"].(map[string]any)["status"] != "open" {
		t.Fatalf("reopen: %d %s", code, out)
	}
	if _, code := runIdea(t, "resolve", "brewshift", "three"); code != 2 {
		t.Fatalf("not a number: %d", code)
	}
	if _, code := runIdea(t, "resolve", "brewshift", "99"); code != 1 {
		t.Fatalf("no such comment: %d", code)
	}

	// The agent's pass, by hand.
	out, code = runIdea(t, "answer", "brewshift")
	if code != 0 || !strings.Contains(out, "answered;") {
		t.Fatalf("answer: %d %s", code, out)
	}
	out, code = runIdea(t, "answer", "brewshift", "--json")
	if code != 0 || ideaJSONOut(t, out)["answered"] != false {
		t.Fatalf("nothing waits now: %d %s", code, out)
	}
	if out, code := runIdea(t, "answer", "brewshift"); code != 0 || !strings.Contains(out, "nothing waits") {
		t.Fatalf("answer idle: %d %s", code, out)
	}
	if _, code := runIdea(t, "answer", "brewshift", "--kind", "abacus"); code != 2 {
		t.Fatalf("unknown kind: %d", code)
	}

	// Tickets, the build, and sending.
	out, code = runIdea(t, "tickets", "brewshift")
	if code != 0 || !strings.Contains(out, "1. Scaffold the app [TASK-1 Todo]") {
		t.Fatalf("tickets: %d %s", code, out)
	}
	out, _ = runIdea(t, "tickets", "brewshift-2", "--json")
	if got := ideaJSONOut(t, out); len(got["tickets"].([]any)) != 2 {
		t.Fatalf("tickets json: %v", got)
	}
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "api", AbsPath: t.TempDir(), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}
	if _, code := runIdea(t, "build", "brewshift"); code != 1 {
		t.Fatalf("build without a workspace: %d", code)
	}
	out, code = runIdea(t, "build", "brewshift", "--workspace", "api", "--no-run")
	if code != 0 || !strings.Contains(out, "P-1: 2 task(s)") || !strings.Contains(out, "plan run P-1") {
		t.Fatalf("build --no-run: %d %s", code, out)
	}
	out, code = runIdea(t, "build", "brewshift", "--workspace", "api", "--json")
	if code != 0 || ideaJSONOut(t, out)["why"] == nil {
		t.Fatalf("build says why it is not running: %d %s", code, out)
	}
	if _, code := runIdea(t, "build", "brewshift", "--kind", "abacus"); code != 2 {
		t.Fatalf("unknown kind: %d", code)
	}
	n := 0
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_ = json.NewEncoder(w).Encode(map[string]any{"id": itoa(n), "key": "SHOP-" + itoa(n)})
	}))
	defer jira.Close()
	if err := watch.SaveSecrets(dir, watch.Secrets{JiraURL: jira.URL, JiraEmail: "me@x", JiraToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	if _, code := runIdea(t, "send", "brewshift", "--to", "jira"); code != 1 {
		t.Fatalf("send without a project: %d", code)
	}
	out, code = runIdea(t, "send", "brewshift", "--to", "jira", "--project", "SHOP")
	if code != 0 || !strings.Contains(out, "epic    SHOP-1") || !strings.Contains(out, "ticket  SHOP-3") {
		t.Fatalf("send: %d %s", code, out)
	}
	out, code = runIdea(t, "send", "brewshift-2", "--to", "jira", "--project", "SHOP", "--json")
	if code != 0 || len(ideaJSONOut(t, out)["sent"].([]any)) != 3 {
		t.Fatalf("send json: %d %s", code, out)
	}

	// Sharing and ringing.
	out, code = runIdea(t, "share", "brewshift")
	if code != 0 || !strings.Contains(out, "?k=") {
		t.Fatalf("share: %d %s", code, out)
	}
	before, _ := idea.Load(dir, "brewshift")
	out, _ = runIdea(t, "share", "brewshift", "--rotate", "--json")
	after, _ := idea.Load(dir, "brewshift")
	if after.Meta.Key == before.Meta.Key || ideaJSONOut(t, out)["open"] != true {
		t.Fatalf("rotate: %s", out)
	}
	out, code = runIdea(t, "share", "brewshift", "--off")
	if code != 0 || !strings.Contains(out, "closed") {
		t.Fatalf("share --off: %d %s", code, out)
	}
	if _, code := runIdea(t, "open", "brewshift"); code != 2 {
		t.Fatalf("a closed page does not open: %d", code)
	}
	out, _ = runIdea(t, "share", "brewshift")
	if !strings.Contains(out, "?k=") {
		t.Fatalf("share opens it again: %s", out)
	}
	out, code = runIdea(t, "notify")
	if code != 0 || !strings.Contains(out, "idea notifications: on") {
		t.Fatalf("notify: %d %s", code, out)
	}
	if out, code := runIdea(t, "notify", "off"); code != 0 || !strings.Contains(out, "off") || ideaNotifyOn(dir) {
		t.Fatalf("notify off: %d %s", code, out)
	}
	out, _ = runIdea(t, "notify", "--json")
	if ideaJSONOut(t, out)["ideaNotify"] != "off" {
		t.Fatalf("notify json: %s", out)
	}
	if _, code := runIdea(t, "notify", "loud"); code != 2 {
		t.Fatalf("on or off: %d", code)
	}
	if out, code := runIdea(t, "notify", "on"); code != 0 || !ideaNotifyOn(dir) {
		t.Fatalf("notify on: %d %s", code, out)
	}
	out, code = runIdea(t, "rm", "brewshift-2")
	if code != 0 || !strings.Contains(out, "removed") {
		t.Fatalf("rm: %d %s", code, out)
	}
	if _, code := runIdea(t, "rm", "brewshift-2"); code != 2 {
		t.Fatalf("rm twice: %d", code)
	}
	if out, code := runIdea(t, "list", "--json"); code != 0 || len(ideaJSONOut(t, out)["ideas"].([]any)) != 1 {
		t.Fatalf("one left: %s", out)
	}
}

func TestTheIdeaMCPToolsDoTheSameFromASession(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	quietIdeaNotify(t)
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "api", AbsPath: t.TempDir(), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}
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
	call := func(name string, args map[string]any) (map[string]any, string) {
		t.Helper()
		req := mcp.CallToolRequest{}
		req.Params.Name = name
		req.Params.Arguments = args
		res, err := c.CallTool(ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		text := ""
		for _, item := range res.Content {
			switch tc := item.(type) {
			case mcp.TextContent:
				text += tc.Text
			case *mcp.TextContent:
				text += tc.Text
			default:
				text += fmt.Sprintf("%v", item)
			}
		}
		if res.IsError {
			if text == "" {
				text = fmt.Sprintf("error result with no text: %+v", res)
			}
			return nil, text
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("%s: not JSON: %s", name, text)
		}
		return out, ""
	}

	out, _ := call("corgi_idea_list", nil)
	if len(out["ideas"].([]any)) != 0 {
		t.Fatalf("nothing yet: %v", out)
	}
	if _, msg := call("corgi_idea_new", map[string]any{"text": " "}); !strings.Contains(msg, "say what") {
		t.Fatalf("empty: %s", msg)
	}
	out, _ = call("corgi_idea_new", map[string]any{"text": "coffee shop shifts", "type": "spec", "workspace": "api", "tasks": true})
	if out["idea"].(map[string]any)["slug"] != "brewshift" || len(out["tasks"].([]any)) != 2 {
		t.Fatalf("new: %v", out)
	}
	if _, msg := call("corgi_idea_new", map[string]any{"text": "x", "workspace": "nope"}); msg == "" {
		t.Fatal("unknown workspace")
	}
	out, _ = call("corgi_idea_read", map[string]any{"slug": "brewshift"})
	if out["markdown"] == nil || len(out["waiting"].([]any)) != 0 || len(out["tickets"].([]any)) != 2 {
		t.Fatalf("read: %v", out)
	}
	if _, msg := call("corgi_idea_read", map[string]any{"slug": "nope"}); msg == "" {
		t.Fatal("read nope")
	}
	out, _ = call("corgi_idea_comment", map[string]any{"slug": "brewshift", "text": "use postgres", "quote": "SQLite on a single VPS", "by": "maria"})
	if out["forAgent"].(float64) != 1 || out["comment"].(map[string]any)["anchor"] == nil {
		t.Fatalf("comment: %v", out)
	}
	if _, msg := call("corgi_idea_comment", map[string]any{"slug": "brewshift", "text": "x", "quote": "zebra"}); !strings.Contains(msg, "verbatim") {
		t.Fatalf("a quote must be on the page: %s", msg)
	}
	out, _ = call("corgi_idea_edit", map[string]any{"slug": "brewshift", "markdown": strings.Replace(fakePage, "SQLite", "Postgres", 1), "tickets": []any{map[string]any{"title": "Scaffold the app"}, map[string]any{"body": "no title"}, map[string]any{"title": "Postgres on Fly", "after": []any{float64(1)}}}})
	if tk := out["tickets"].([]any); len(tk) != 2 || tk[1].(map[string]any)["title"] != "Postgres on Fly" || tk[0].(map[string]any)["ref"] != "TASK-1" {
		t.Fatalf("edit: %v", out)
	}
	if _, msg := call("corgi_idea_edit", map[string]any{"slug": "brewshift"}); !strings.Contains(msg, "markdown") {
		t.Fatalf("edit nothing: %s", msg)
	}
	out, _ = call("corgi_idea_comment", map[string]any{"slug": "brewshift", "text": "Done: Infra says Postgres now.", "replyTo": float64(3), "by": "agent", "changed": true})
	if out["forAgent"].(float64) != 0 {
		t.Fatalf("the agent's reply: %v", out)
	}
	page, _ := idea.Load(dir, "brewshift")
	if c, _ := page.Find(3); c.Status != idea.StatusFixed || !strings.Contains(page.Doc, "Postgres") {
		t.Fatalf("fixed: %+v", c)
	}
	out, _ = call("corgi_idea_resolve", map[string]any{"slug": "brewshift", "id": float64(3)})
	if out["comment"].(map[string]any)["status"] != "resolved" {
		t.Fatalf("resolve: %v", out)
	}
	out, _ = call("corgi_idea_resolve", map[string]any{"slug": "brewshift", "id": float64(3), "status": "open"})
	if out["comment"].(map[string]any)["status"] != "open" {
		t.Fatalf("reopen: %v", out)
	}
	if _, msg := call("corgi_idea_resolve", map[string]any{"slug": "brewshift", "id": float64(3), "status": "fixed"}); msg == "" {
		t.Fatal("only resolved or open")
	}
	out, _ = call("corgi_idea_build", map[string]any{"slug": "brewshift", "workspace": "api", "run": false, "slots": float64(2)})
	if out["plan"] == nil || out["running"] != false || len(out["tasks"].([]any)) != 2 {
		t.Fatalf("build: %v", out)
	}
	if _, msg := call("corgi_idea_send", map[string]any{"slug": "brewshift", "to": "linear"}); !strings.Contains(msg, "linear") {
		t.Fatalf("send without credentials: %s", msg)
	}
	n := 0
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_ = json.NewEncoder(w).Encode(map[string]any{"id": itoa(n), "key": "SHOP-" + itoa(n)})
	}))
	defer jira.Close()
	_ = watch.SaveSecrets(dir, watch.Secrets{JiraURL: jira.URL, JiraEmail: "me@x", JiraToken: "tok"})
	out, _ = call("corgi_idea_send", map[string]any{"slug": "brewshift", "to": "jira", "project": "SHOP"})
	if len(out["sent"].([]any)) != 3 {
		t.Fatalf("send: %v", out)
	}
	out, _ = call("corgi_idea_list", nil)
	if rows := out["ideas"].([]any); len(rows) != 1 || rows[0].(map[string]any)["status"] != "building" {
		t.Fatalf("list after: %v", out)
	}
}

// lockedRecorder is a response recorder a test may read while a handler
// still writes to it.
type lockedRecorder struct {
	mu sync.Mutex
	*httptest.ResponseRecorder
}

func (l *lockedRecorder) Header() http.Header {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ResponseRecorder.Header()
}

func (l *lockedRecorder) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ResponseRecorder.Write(b)
}

func (l *lockedRecorder) WriteHeader(code int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ResponseRecorder.WriteHeader(code)
}

func (l *lockedRecorder) Flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ResponseRecorder.Flush()
}

func (l *lockedRecorder) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Body.String()
}

func TestTheStreamSaysChangeWhenThePageMoves(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	quietIdeaNotify(t)
	id, _, err := newIdea(dir, ideaRequest{Text: "shifts", Model: "sonnet"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	was := ideaStreamTick
	ideaStreamTick = 20 * time.Millisecond
	t.Cleanup(func() { ideaStreamTick = was })
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/idea/brewshift/stream?k="+id.Meta.Key, nil).WithContext(ctx)
	rec := &lockedRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() { ideaOpenHandler("", "")(rec, req); close(done) }()
	time.Sleep(60 * time.Millisecond)
	_, _ = idea.Update(dir, id.Meta.Slug, func(id *idea.Idea) error {
		_, err := id.AddComment(idea.KindComment, "x", "hello", nil, nil, nil, time.Now())
		return err
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(rec.text(), "event: change") {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	body := rec.text()
	if !strings.HasPrefix(body, "event: hello") || !strings.Contains(body, "event: change") || rec.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("stream: %q", body)
	}
	if rec := post(ideaOpenHandler("", ""), "/idea/brewshift/stream", ""); rec.Code != 405 {
		t.Fatalf("POST to the stream: %d", rec.Code)
	}
}

func TestTheRestOfTheOpenRouteAndTheLauncher(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	quietIdeaNotify(t)
	was := ideaAutoAnswerHere
	ideaAutoAnswerHere = false
	t.Cleanup(func() { ideaAutoAnswerHere = was })
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "api", AbsPath: t.TempDir(), Status: workspace.StatusOK})
	_ = workspace.Save(agentRegistryPath(dir), reg)
	id, _, err := newIdea(dir, ideaRequest{Text: "shifts", Model: "sonnet"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	open := ideaOpenHandler("secret", "")
	k := "?k=" + id.Meta.Key
	base := "/idea/brewshift"
	owner := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		open(rec, req)
		return rec
	}
	if rec := post(open, base+"/reply"+k, `{"id":1,"text":"yes please","by":"maria"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"forAgent":1`) {
		t.Fatalf("reply accepts a proposal: %d %s", rec.Code, rec.Body)
	}
	if rec := post(open, base+"/reply"+k, `{"id":99,"text":"x"}`); rec.Code != 404 {
		t.Fatalf("reply to nothing: %d", rec.Code)
	}
	if rec := post(open, base+"/status"+k, `{"id":1,"status":"weird"}`); rec.Code != 400 {
		t.Fatalf("bad status: %d", rec.Code)
	}
	if rec := post(open, base+"/edit"+k, `{"markdown":"# x"}`); rec.Code != 403 {
		t.Fatalf("a key may not edit: %d", rec.Code)
	}
	if rec := post(open, base+"/auto"+k, `{"auto":false}`); rec.Code != 403 {
		t.Fatalf("a key may not set auto: %d", rec.Code)
	}
	if rec := post(open, base+"/send"+k, `{"to":"jira"}`); rec.Code != 403 {
		t.Fatalf("a key may not send: %d", rec.Code)
	}
	if rec := post(open, base+"/file"+k, `{"name":"x","data":"!!"}`); rec.Code != 400 {
		t.Fatalf("bad base64: %d", rec.Code)
	}
	if rec := post(open, base+"/comment"+k, `not json`); rec.Code != 400 {
		t.Fatalf("bad body: %d", rec.Code)
	}
	if rec := ideaGet(open, base+"/comment"+k); rec.Code != 405 {
		t.Fatalf("GET a comment: %d", rec.Code)
	}
	if rec := post(open, base+"/doc"+k, ""); rec.Code != 405 {
		t.Fatalf("POST the doc: %d", rec.Code)
	}
	if rec := ideaGet(open, base+"/dance"+k); rec.Code != 404 {
		t.Fatalf("no such part: %d", rec.Code)
	}
	if rec := ideaGet(open, base+"/file/"+k); rec.Code != 404 {
		t.Fatalf("no file name: %d", rec.Code)
	}
	if rec := ideaGet(open, "/idea/"); rec.Code != 200 {
		t.Fatalf("the index shell: %d", rec.Code)
	}
	rec := owner(http.MethodPost, base+"/file", `{"name":"notes.txt","type":"text/plain","data":"aGk="}`)
	if rec.Code != 200 {
		t.Fatalf("file: %d %s", rec.Code, rec.Body)
	}
	var up struct {
		File idea.Attachment `json:"file"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	rec = owner(http.MethodGet, base+"/file/"+filepath.Base(up.File.Path), "")
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("a text file downloads: %d %s", rec.Code, rec.Header())
	}
	if rec := owner(http.MethodPut, base+"/file/x", ""); rec.Code != 405 {
		t.Fatalf("PUT a file: %d", rec.Code)
	}
	rec = owner(http.MethodPost, base+"/edit", `{"markdown":"# Edited\n\n## Infra\n\nPostgres.\n"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Edited") {
		t.Fatalf("the owner edits: %d %s", rec.Code, rec.Body)
	}
	rec = owner(http.MethodPost, base+"/auto", `{"auto":false}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"auto":false`) {
		t.Fatalf("auto off: %d %s", rec.Code, rec.Body)
	}
	rec = owner(http.MethodPost, base+"/auto", `{"auto":true}`)
	if rec.Code != 200 {
		t.Fatalf("auto on: %d", rec.Code)
	}
	rec = owner(http.MethodPost, base+"/build", `{"workspace":"api","run":false}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"running":false`) {
		t.Fatalf("the owner builds: %d %s", rec.Code, rec.Body)
	}
	if rec := owner(http.MethodPost, base+"/build", `{"workspace":"nope"}`); rec.Code != 400 {
		t.Fatalf("build in an unknown workspace: %d %s", rec.Code, rec.Body)
	}
	n := 0
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_ = json.NewEncoder(w).Encode(map[string]any{"id": itoa(n), "key": "SHOP-" + itoa(n)})
	}))
	defer jira.Close()
	_ = watch.SaveSecrets(dir, watch.Secrets{JiraURL: jira.URL, JiraEmail: "me@x", JiraToken: "tok"})
	rec = owner(http.MethodPost, base+"/send", `{"to":"jira","workspace":"SHOP"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "SHOP-1") {
		t.Fatalf("the owner sends: %d %s", rec.Code, rec.Body)
	}
	if rec := owner(http.MethodPost, base+"/send", `{"to":"mars"}`); rec.Code != 400 {
		t.Fatalf("send to mars: %d", rec.Code)
	}

	// The launcher routes, for the phone.
	rec = ideaGet(launchIdeaHandler, "/launch/idea?slug=brewshift&file="+filepath.Base(up.File.Path))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"data":"aGk="`) {
		t.Fatalf("a file inside JSON: %d %s", rec.Code, rec.Body)
	}
	if rec := ideaGet(launchIdeaHandler, "/launch/idea?slug=brewshift&file=nope"); rec.Code != 404 {
		t.Fatalf("no such file: %d", rec.Code)
	}
	if rec := ideaGet(launchIdeaHandler, "/launch/idea?slug=brewshift&html=1"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"html"`) {
		t.Fatalf("html on request: %d", rec.Code)
	}
	if rec := post(launchIdeaHandler, "/launch/idea", `nope`); rec.Code != 400 {
		t.Fatalf("bad body: %d", rec.Code)
	}
	if rec := post(launchIdeaHandler, "/launch/idea", `{"do":"new","text":""}`); rec.Code != 400 {
		t.Fatalf("new without words: %d", rec.Code)
	}
	if rec := post(launchIdeaHandler, "/launch/idea", `{"do":"new","text":"x","workspace":"nope"}`); rec.Code != 400 {
		t.Fatalf("new in an unknown workspace: %d", rec.Code)
	}
	rec = post(launchIdeaHandler, "/launch/idea", `{"do":"new","markdown":"# Own\n\nWords.\n","type":"rfc"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"type":"rfc"`) {
		t.Fatalf("new from markdown: %d %s", rec.Code, rec.Body)
	}
	req := httptest.NewRequest(http.MethodPut, "/launch/idea", nil)
	rec = httptest.NewRecorder()
	launchIdeaHandler(rec, req)
	if rec.Code != 405 {
		t.Fatalf("PUT: %d", rec.Code)
	}
	if rec := post(launchIdeasHandler, "/launch/ideas", ""); rec.Code != 405 {
		t.Fatalf("POST the list: %d", rec.Code)
	}
	if ideaHTTPStatus(errors.New("x")) != 400 || ideaHTTPStatus(errAlreadyAnswering) != 409 || ideaHTTPStatus(idea.ErrNotFound) != 404 {
		t.Fatal("status codes")
	}
	if mimeFor("a.md") != "text/plain; charset=utf-8" || mimeFor("a.png") != "image/png" || mimeFor("a.weird-ext") != "application/octet-stream" {
		t.Fatalf("mime: %s %s", mimeFor("a.png"), mimeFor("a.weird-ext"))
	}
	t.Setenv("CORGI_USER", "")
	t.Setenv("USER", "")
	t.Setenv("USERNAME", "")
	if osUserName() != "someone" {
		t.Fatal("no user name")
	}
	t.Setenv("CORGI_USER", "andrii")
	if osUserName() != "andrii" {
		t.Fatal("CORGI_USER wins")
	}
	if ideaDashboardUp(dir) {
		t.Fatal("no dashboard here")
	}
	// Links prefer the public URL for others, the local one for this machine.
	recordPublicURL("https://corgi.example.com")
	m := idea.Meta{Slug: "brewshift", Key: "abc"}
	if got := ideaLink(dir, m, false); got != "https://corgi.example.com/idea/brewshift?k=abc" {
		t.Fatalf("public link: %s", got)
	}
	if ideaLink(dir, idea.Meta{Slug: "x"}, false) != "" {
		t.Fatal("a closed page has no link")
	}
}

func TestAFailedAnswerIsSaidOnceAndTriedAgainLater(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	was := runIdeaPrint
	runIdeaPrint = func(_ context.Context, _, _, system, _ string) (string, error) {
		if strings.Contains(system, "product-minded") {
			return `{"name":"BrewShift","markdown":` + jsonString(fakePage) + `}`, nil
		}
		return "", errors.New("claude: not logged in")
	}
	t.Cleanup(func() { runIdeaPrint = was })
	wasHere, wasDelay := ideaAutoAnswerHere, ideaAnswerDelay
	ideaAutoAnswerHere, ideaAnswerDelay = true, 30*time.Millisecond
	t.Cleanup(func() { ideaAutoAnswerHere, ideaAnswerDelay = wasHere, wasDelay; ideaAnswerFailed.Delete("brewshift") })
	id, _, err := newIdea(dir, ideaRequest{Text: "shifts", Model: "sonnet"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, _ = idea.Update(dir, id.Meta.Slug, func(id *idea.Idea) error {
			_, err := id.AddComment(idea.KindComment, "andrii", "please", nil, nil, nil, time.Now())
			return err
		})
		scheduleIdeaAnswer(dir, id.Meta.Slug)
		time.Sleep(150 * time.Millisecond)
	}
	after, _ := idea.Load(dir, id.Meta.Slug)
	notes := 0
	for _, c := range after.Comments.Comments {
		if strings.Contains(c.Text, "could not answer") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("one note for a failure that repeats: %d", notes)
	}
	if _, failed := ideaAnswerFailed.Load(id.Meta.Slug); !failed {
		t.Fatal("the failure is remembered")
	}
	// The loop ticks and, with the page set to answer on its own, schedules; the failure holds it back.
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	ideaAnswerLoop(ctx, dir, 20*time.Millisecond)
	if after, _ := idea.Load(dir, id.Meta.Slug); len(after.ForAgent()) != 2 {
		t.Fatalf("still waiting, no new runs: %d", len(after.ForAgent()))
	}
}

func TestPrintInRunsTheHarnessInTheWorkspace(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$FAIL\" = 1 ]; then echo boom >&2; exit 1; fi\npwd\necho \"$@\" | tr -d '\\n' | head -c 40\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	work := t.TempDir()
	out, err := printIn(context.Background(), work, "haiku", "be brief", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, work) || !strings.Contains(out, "-p hello") {
		t.Fatalf("ran in the workspace with the prompt: %q", out)
	}
	t.Setenv("FAIL", "1")
	if _, err := printIn(context.Background(), "", "", "", "x"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("the failure carries stderr: %v", err)
	}
}
