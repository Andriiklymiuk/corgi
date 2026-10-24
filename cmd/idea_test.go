package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

const fakePage = "# BrewShift\n\n## What it is\n\nShift scheduling for one coffee shop.\n\n## Infra\n\nSQLite on a single VPS, no auth beyond a shared PIN.\n\n## Tasks\n\n- [ ] Scaffold the app\n- [ ] Shift calendar\n"

func fakeWriter(t *testing.T, calls *[]string) {
	t.Helper()
	was := runIdeaPrint
	runIdeaPrint = func(_ context.Context, dir, model, system, prompt string) (string, error) {
		*calls = append(*calls, system[:30])
		switch {
		case strings.Contains(system, "product-minded"):
			return `{"name":"BrewShift","tags":["Scheduling","coffee","Shift Work","scheduling"],"summary":"Shifts for a coffee shop.","markdown":` + jsonString(fakePage) + `,"tasks":[{"title":"Scaffold the app","body":"Next.js + SQLite."},{"title":"Shift calendar","body":"Week view."}],"proposals":[{"quote":"SQLite on a single VPS","text":"Postgres on Fly would survive a second shop."},{"quote":"not on the page","text":"Add a waitlist."}]}`, nil
		case strings.Contains(system, "keeps it"):
			page := strings.Replace(fakePage, "SQLite on a single VPS", "Postgres on Fly", 1)
			return "Sure:\n" + `{"page":` + jsonString(page) + `,"answers":[{"id":3,"reply":"Switched Infra to Postgres on Fly.","changed":true}],"proposals":[{"quote":"Postgres on Fly","text":"Add pgbouncer in front."}]}`, nil
		case strings.Contains(system, "staff engineer breaking"):
			return `{"summary":"Three steps.","tasks":[{"title":"Scaffold","body":"go"},{"title":"Calendar","body":"go","after":[1]},{"title":"PIN login","body":"go","after":[1]}]}`, nil
		}
		return "", nil
	}
	t.Cleanup(func() { runIdeaPrint = was })
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func quietIdeaNotify(t *testing.T) *[]string {
	t.Helper()
	var got []string
	was := ideaNotify
	ideaNotify = func(dir string, m idea.Meta, what, detail string) { got = append(got, m.Slug+": "+what) }
	t.Cleanup(func() { ideaNotify = was })
	return &got
}

func TestANewIdeaIsNamedTaggedTaskedAndProposedOn(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	rang := quietIdeaNotify(t)
	id, tasks, err := newIdea(dir, "a scheduling app for a coffee shop", "", "sonnet", "cli", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if id.Meta.Slug != "brewshift" || id.Meta.Name != "BrewShift" || id.Meta.Title != "a scheduling app for a coffee shop" {
		t.Fatalf("meta: %+v", id.Meta)
	}
	if strings.Join(id.Meta.Tags, ",") != "scheduling,coffee,shift-work" {
		t.Fatalf("tags are cleaned and deduplicated: %v", id.Meta.Tags)
	}
	if len(tasks) != 2 || tasks[0].Idea != "brewshift" || len(tasks[0].Tags) != 3 || !strings.Contains(tasks[0].Body, "corgi idea open brewshift") {
		t.Fatalf("tasks on the board carry the idea: %+v", tasks)
	}
	if got := watch.LoadTasks(dir).ByIdea("brewshift"); len(got) != 2 {
		t.Fatalf("tasks found by idea: %d", len(got))
	}
	cs := id.Comments.Comments
	if len(cs) != 2 || cs[0].Kind != idea.KindProposal || cs[0].By != idea.Agent || cs[0].Anchor == nil || cs[0].Anchor.Heading != "Infra" || cs[1].Anchor != nil {
		t.Fatalf("proposals anchored where the words are: %+v", cs)
	}
	if len(id.ForAgent()) != 0 {
		t.Fatal("proposals wait on a person, not on the agent")
	}
	if _, err := os.Stat(filepath.Join(id.Dir, idea.DocName)); err != nil {
		t.Fatal(err)
	}
	if len(*rang) != 0 {
		t.Fatalf("writing the page rings nobody: %v", *rang)
	}
	// A dead-end workspace is refused before the agent runs.
	if _, _, err := newIdea(dir, "x", "nope", "sonnet", "cli", true, time.Now()); err == nil {
		t.Fatal("an unknown workspace is refused")
	}
}

func TestTheAgentAnswersCommentsAndAmendsThePage(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	rang := quietIdeaNotify(t)
	id, _, err := newIdea(dir, "coffee shop shifts", "", "sonnet", "cli", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if did, err := answerIdea(dir, id.Meta.Slug, ""); did || err != nil {
		t.Fatalf("nothing to answer yet: %v %v", did, err)
	}
	_, err = idea.Update(dir, id.Meta.Slug, func(id *idea.Idea) error {
		_, err := id.AddComment(idea.KindComment, "andrii", "use postgres", anchorFor(id.Doc, "SQLite on a single VPS"), nil, nil, time.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	did, err := answerIdea(dir, id.Meta.Slug, "")
	if !did || err != nil {
		t.Fatalf("answered: %v %v", did, err)
	}
	after, _ := idea.Load(dir, id.Meta.Slug)
	if !strings.Contains(after.Doc, "Postgres on Fly") || strings.Contains(after.Doc, "SQLite") {
		t.Fatalf("the page changed:\n%s", after.Doc)
	}
	c, _ := after.Find(3)
	if c.Status != idea.StatusFixed || len(c.Replies) != 1 || !c.Replies[0].Changed || c.Replies[0].By != idea.Agent {
		t.Fatalf("the comment is fixed with the agent's reply: %+v", c)
	}
	if n := len(after.Comments.Comments); n != 4 || after.Comments.Comments[3].Kind != idea.KindProposal || after.Comments.Comments[3].Anchor == nil {
		t.Fatalf("the new proposal is anchored on the new page: %+v", after.Comments.Comments)
	}
	if after.Meta.Answering {
		t.Fatal("answering is cleared")
	}
	if len(*rang) != 1 || !strings.Contains((*rang)[0], "updated the page and answered 1") {
		t.Fatalf("one notification for the answer: %v", *rang)
	}
	if got := strings.Count(strings.Join(calls, "|"), "engineer who wrote"); got != 1 {
		t.Fatalf("one agent run: %v", calls)
	}
}

func TestBuildHandsThePageToAPlanAndTheBoard(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	quietIdeaNotify(t)
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "api", AbsPath: t.TempDir(), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}
	id, first, err := newIdea(dir, "coffee shop shifts", "", "sonnet", "cli", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := buildIdea(dir, id.Meta.Slug, "", "", 1, true); err == nil || !strings.Contains(err.Error(), "--workspace") {
		t.Fatalf("no workspace: %v", err)
	}
	// One of the first-cut tasks is already being worked on; it stays.
	tasks := watch.LoadTasks(dir)
	if _, err := tasks.Move(first[1].Key(), "Doing", time.Now()); err != nil {
		t.Fatal(err)
	}
	res, err := buildIdea(dir, id.Meta.Slug, "api", "", 2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tasks) != 3 || res.Plan.Workspace != "api" || res.Plan.Slots != 2 || res.Running {
		t.Fatalf("plan: %+v running=%v", res.Plan, res.Running)
	}
	if res.Why == "" || !strings.Contains(res.Why, "watch enable") {
		t.Fatalf("an unwatched workspace is said: %q", res.Why)
	}
	left := watch.LoadTasks(dir).ByIdea(id.Meta.Slug)
	if len(left) != 4 {
		t.Fatalf("the Todo first cut is replaced, the Doing one stays: %+v", left)
	}
	for _, task := range left {
		if task.Workspace != "api" && task.State != "Doing" {
			t.Fatalf("new tasks are in the workspace: %+v", task)
		}
	}
	after, _ := idea.Load(dir, id.Meta.Slug)
	if after.Meta.Status != idea.Building || after.Meta.Plan != res.Plan.ID || after.Meta.Workspace != "api" || len(after.Meta.Tasks) != 3 {
		t.Fatalf("meta after build: %+v", after.Meta)
	}
	last := after.Comments.Comments[len(after.Comments.Comments)-1]
	if last.By != idea.Agent || last.Status != idea.StatusAnswered || !strings.Contains(last.Text, res.Plan.Ref()) {
		t.Fatalf("the page says what happened: %+v", last)
	}
	if _, err := os.Stat(filepath.Join(dir, "plans", res.Plan.Ref()+".md")); err != nil {
		t.Fatal(err)
	}
}

func ideaGet(h http.HandlerFunc, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestTheOpenPageAnswersToItsKeyAndTheDashboardToItsToken(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	rang := quietIdeaNotify(t)
	was := ideaAutoAnswerHere
	ideaAutoAnswerHere = false
	t.Cleanup(func() { ideaAutoAnswerHere = was })
	id, _, err := newIdea(dir, "coffee shop shifts", "", "sonnet", "cli", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	open := ideaOpenHandler("secret", "")
	k := "?k=" + id.Meta.Key
	base := "/idea/" + id.Meta.Slug

	if rec := ideaGet(open, base); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Fatalf("the shell: %d", rec.Code)
	}
	if rec := ideaGet(open, base+"/doc"); rec.Code != 403 {
		t.Fatalf("no key, no page: %d %s", rec.Code, rec.Body)
	}
	if rec := ideaGet(open, base+"/doc?k=wrong"); rec.Code != 403 {
		t.Fatalf("wrong key: %d", rec.Code)
	}
	if rec := ideaGet(open, "/idea/nope/doc"+k); rec.Code != 404 {
		t.Fatalf("unknown page: %d", rec.Code)
	}
	rec := ideaGet(open, base+"/doc"+k)
	if rec.Code != 200 {
		t.Fatalf("doc: %d %s", rec.Code, rec.Body)
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if !strings.Contains(got["html"].(string), "<h2 id=\"infra\">Infra</h2>") || len(got["tasks"].([]any)) != 2 || got["forAgent"].(float64) != 0 {
		t.Fatalf("doc: %v", got)
	}

	// A comment with the key, with an attachment uploaded first.
	rec = post(open, base+"/file"+k, `{"name":"../sketch.png","type":"image/png","data":"cG5n"}`)
	if rec.Code != 200 {
		t.Fatalf("file: %d %s", rec.Code, rec.Body)
	}
	var up struct {
		File idea.Attachment `json:"file"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	if up.File.Name != "sketch.png" || !strings.HasPrefix(up.File.Path, "files/") {
		t.Fatalf("attachment: %+v", up.File)
	}
	body := `{"text":"use postgres","by":"maria","quote":"SQLite on a single VPS","links":["https://fly.io","ftp://no"],"files":[{"name":"sketch.png","path":"` + up.File.Path + `"},{"name":"x","path":"files/../../meta.json"}]}`
	rec = post(open, base+"/comment"+k, body)
	if rec.Code != 200 {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body)
	}
	after, _ := idea.Load(dir, id.Meta.Slug)
	c := after.Comments.Comments[len(after.Comments.Comments)-1]
	if c.By != "maria" || c.Anchor == nil || c.Anchor.Heading != "Infra" || len(c.Links) != 1 || len(c.Files) != 1 || c.Files[0].Size != 3 {
		t.Fatalf("the comment, its anchor, one good link and only this page's file: %+v", c)
	}
	if len(after.ForAgent()) != 1 {
		t.Fatal("it waits on the agent")
	}
	if len(*rang) != 1 || !strings.Contains((*rang)[0], "comment by maria") {
		t.Fatalf("the owner is told: %v", *rang)
	}
	name := filepath.Base(up.File.Path)
	if rec := ideaGet(open, base+"/file/"+name+k); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("the file comes back: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := ideaGet(open, base+"/file/meta.json"+k); rec.Code != 404 {
		t.Fatalf("only files: %d", rec.Code)
	}
	// The key may not build; the dashboard's token may.
	if rec := post(open, base+"/build"+k, `{"workspace":"api"}`); rec.Code != 403 {
		t.Fatalf("key cannot build: %d %s", rec.Code, rec.Body)
	}
	req := httptest.NewRequest(http.MethodPost, base+"/status", strings.NewReader(`{"id":`+itoa(c.ID)+`,"status":"resolved"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	open(rec, req)
	if rec.Code != 200 {
		t.Fatalf("token without key: %d %s", rec.Code, rec.Body)
	}
	if after, _ := idea.Load(dir, id.Meta.Slug); after.Open() != 2 {
		t.Fatalf("resolved: %d open", after.Open())
	}
	// Closing the page: the key stops working, the token still does.
	_, _ = idea.Update(dir, id.Meta.Slug, func(id *idea.Idea) error { id.Meta.Key = ""; return nil })
	if rec := ideaGet(open, base+"/doc"+k); rec.Code != 403 {
		t.Fatalf("closed: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/doc", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	open(rec, req)
	if rec.Code != 200 {
		t.Fatalf("the dashboard reads a closed page: %d", rec.Code)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestThePhoneListsPagesAndActsThroughTheLauncher(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	quietIdeaNotify(t)
	was := ideaAutoAnswerHere
	ideaAutoAnswerHere = false
	t.Cleanup(func() { ideaAutoAnswerHere = was })
	rec := post(launchIdeaHandler, "/launch/idea", `{"do":"new","text":"coffee shop shifts"}`)
	if rec.Code != 200 {
		t.Fatalf("new from the phone: %d %s", rec.Code, rec.Body)
	}
	rec = ideaGet(launchIdeasHandler, "/launch/ideas")
	var list struct {
		Ideas  []ideaRow `json:"ideas"`
		Notify bool      `json:"notify"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Ideas) != 1 || list.Ideas[0].Slug != "brewshift" || list.Ideas[0].Open != 2 || !list.Notify {
		t.Fatalf("list: %+v", list)
	}
	rec = post(launchIdeaHandler, "/launch/idea", `{"do":"comment","slug":"brewshift","text":"more shops later"}`)
	if rec.Code != 200 {
		t.Fatalf("comment: %d %s", rec.Code, rec.Body)
	}
	id, _ := idea.Load(dir, "brewshift")
	if c := id.Comments.Comments[len(id.Comments.Comments)-1]; c.By != "phone" || c.Anchor != nil {
		t.Fatalf("a page-level comment from the phone: %+v", c)
	}
	if rec := post(launchIdeaHandler, "/launch/idea", `{"do":"dance","slug":"brewshift"}`); rec.Code != 400 {
		t.Fatalf("unknown action: %d", rec.Code)
	}
	if rec := post(launchIdeaHandler, "/launch/idea", `{"do":"notify","auto":false}`); rec.Code != 200 || ideaNotifyOn(dir) {
		t.Fatalf("notify off: %d %v", rec.Code, ideaNotifyOn(dir))
	}
	if rec := post(launchIdeaHandler, "/launch/idea", `{"do":"notify","auto":true}`); rec.Code != 200 || !ideaNotifyOn(dir) {
		t.Fatalf("notify on: %d", rec.Code)
	}
	if rec := ideaGet(launchIdeaHandler, "/launch/idea?slug=nope"); rec.Code != 404 {
		t.Fatalf("unknown: %d", rec.Code)
	}
}

func TestACommentSchedulesTheAgentOnceForABurst(t *testing.T) {
	dir := phoneBoard(t, false)
	var calls []string
	fakeWriter(t, &calls)
	quietIdeaNotify(t)
	wasHere, wasDelay := ideaAutoAnswerHere, ideaAnswerDelay
	ideaAutoAnswerHere, ideaAnswerDelay = true, 60*time.Millisecond
	t.Cleanup(func() { ideaAutoAnswerHere, ideaAnswerDelay = wasHere, wasDelay })
	id, _, err := newIdea(dir, "coffee shop shifts", "", "sonnet", "cli", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	open := ideaOpenHandler("", "")
	for _, text := range []string{"use postgres", "and a waitlist"} {
		if rec := post(open, "/idea/"+id.Meta.Slug+"/comment", `{"text":"`+text+`","quote":"SQLite on a single VPS"}`); rec.Code != 200 {
			t.Fatalf("comment: %d %s", rec.Code, rec.Body)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		after, _ := idea.Load(dir, id.Meta.Slug)
		if len(after.ForAgent()) == 0 && !after.Meta.Answering {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	after, _ := idea.Load(dir, id.Meta.Slug)
	if n := len(after.ForAgent()); n != 0 {
		t.Fatalf("%d still wait on the agent", n)
	}
	if got := strings.Count(strings.Join(calls, "|"), "engineer who wrote"); got != 1 {
		t.Fatalf("two comments in a burst are one agent run: %v", calls)
	}
}

func TestIdeaAnswersAreParsedLeniently(t *testing.T) {
	if _, err := parseIdeaPage("I cannot"); err == nil {
		t.Fatal("prose is refused")
	}
	if _, err := parseIdeaPage(`{"name":"x","markdown":""}`); err == nil {
		t.Fatal("an empty page is refused")
	}
	ans, err := parseIdeaComments("Here you go:\n```json\n{\"page\":\"\",\"answers\":[{\"id\":1,\"reply\":\"ok\"}]}\n```")
	if err != nil || len(ans.Answers) != 1 || ans.Page != "" {
		t.Fatalf("fenced JSON: %+v %v", ans, err)
	}
	if a := anchorFor("# T\n\nsome words here\n", "words"); a == nil || a.Prefix != "# T\n\nsome " || a.Suffix != " here\n" || a.Heading != "T" {
		t.Fatalf("anchor: %+v", a)
	}
	if anchorFor("x", "nope") != nil {
		t.Fatal("missing words have no anchor")
	}
	if got := renderIdeaHTML("# Hi\n\n<script>x</script> **b** [a](javascript:alert(1)) \"q\""); strings.Contains(got, "<script") || !strings.Contains(got, "<strong>b</strong>") || !strings.Contains(got, "&quot;q&quot;") || strings.Contains(got, "javascript:") {
		t.Fatalf("html: %s", got)
	}
}
