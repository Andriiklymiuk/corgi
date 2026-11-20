package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/idea"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

var twoRepoStack = []stackService{{Name: "api", Repo: "backend"}, {Name: "worker", Repo: "backend"}, {Name: "web", Repo: "frontend"}}

func TestASplitWhereEveryTicketStaysInOneRepositoryIsALayerCut(t *testing.T) {
	layered := []plannerTask{
		{Title: "Store usage limits", Touches: []string{"api"}},
		{Title: "Serve the limits", Touches: []string{"worker"}},
		{Title: "Show the banner", Touches: []string{"web"}},
	}
	why := layerCut(layered, twoRepoStack)
	if !strings.Contains(why, "backend: 1, 2") || !strings.Contains(why, "frontend: 3") {
		t.Fatalf("the reason names the tickets per repository: %q", why)
	}
	whole := []plannerTask{
		{Title: "Show the owner a banner at the limit", Touches: []string{"api", "web"}},
		{Title: "Mail the owner at 80 percent", Touches: []string{"worker"}},
	}
	if why := layerCut(whole, twoRepoStack); why != "" {
		t.Fatalf("a ticket that crosses repositories is a feature: %q", why)
	}
	if why := layerCut(layered[:1], twoRepoStack); why != "" {
		t.Fatalf("one ticket is never a layer cut: %q", why)
	}
	if why := layerCut(layered, twoRepoStack[:2]); why != "" {
		t.Fatalf("one repository has no layers to cross: %q", why)
	}
}

func TestTicketsNamedAfterALayerAreALayerCut(t *testing.T) {
	why := layerCut([]plannerTask{{Title: "Add migration for usage limits"}, {Title: "Backend: limits endpoint"}, {Title: "Show the banner to the owner"}}, nil)
	if !strings.Contains(why, "1, 2") {
		t.Fatalf("the layer titles are named: %q", why)
	}
	if why := layerCut([]plannerTask{{Title: "Add the week grid the owner drags shifts onto"}, {Title: "Add tests-first pricing page for buyers"}}, nil); why != "" {
		t.Fatalf("feature titles pass: %q", why)
	}
}

func TestTheStackPictureGroupsServicesByRepository(t *testing.T) {
	root := t.TempDir()
	compose := "services:\n  api:\n    path: ./backend\n  worker:\n    path: ./backend\n  web:\n    path: ./frontend\n  cache:\n    port: 6379\n"
	if err := os.WriteFile(filepath.Join(root, "corgi-compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	picture := stackPicture(root)
	if !strings.Contains(picture, "- backend: api, worker\n") || !strings.Contains(picture, "- frontend: web\n") || strings.Contains(picture, "cache") {
		t.Fatalf("picture: %q", picture)
	}
	if stackPicture(t.TempDir()) != "" {
		t.Fatal("no compose file, no picture")
	}
}

func TestATicketBodySaysWhatItTouchesAndWhenItIsDone(t *testing.T) {
	body := sliceBody("Build the banner.", []string{"api", "web"}, []string{"The owner sees the banner at 100 requests."})
	for _, want := range []string{"Build the banner.", "Touches: api, web.", "one pull request per repository", "Done when:\n- The owner sees the banner at 100 requests."} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %q", want, body)
		}
	}
	if got := sliceBody("Only this.", nil, nil); got != "Only this." {
		t.Fatalf("nothing is added to a plain ticket: %q", got)
	}
	if got := cleanTouches([]string{" api ", "API", "", "web"}); len(got) != 2 {
		t.Fatalf("touches are trimmed and unique: %v", got)
	}
}

func layeredPage(t *testing.T, dir string) *idea.Idea {
	t.Helper()
	meta := idea.Meta{Type: idea.TypeSpec, Name: "Limits", Workspace: "shop", Tickets: []idea.Ticket{
		{ID: 1, Title: "Add migration for usage limits", Body: "A table."},
		{ID: 2, Title: "Backend: limits endpoint", Body: "An endpoint.", After: []int{1}},
		{ID: 3, Title: "Frontend: limits banner", Body: "A banner.", After: []int{2}},
	}}
	id, err := idea.Create(dir, "", "usage limits with a banner", "# Limits\n\n## Summary\n\nA banner at the limit.\n", meta, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func shopWorkspace(t *testing.T, dir string) {
	t.Helper()
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "shop", AbsPath: t.TempDir(), Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), reg); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCutsALayeredSplitAgainByFeature(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	shopWorkspace(t, dir)
	id := layeredPage(t, dir)
	var prompts []string
	was := runIdeaPrint
	runIdeaPrint = func(_ context.Context, _, _, system, prompt string) (string, error) {
		if !strings.Contains(system, "re-cutting") || !strings.Contains(system, "never by layer") {
			t.Fatalf("the re-cut soul carries the rule: %q", system[:60])
		}
		prompts = append(prompts, prompt)
		return `{"summary":"Two slices.","tasks":[{"title":"Show the owner a banner at the limit","body":"End to end.","touches":["api","web"],"checks":["The owner sees the banner at 100 requests."]},{"title":"Mail the owner at 80 percent","body":"A job and a template.","after":[1],"touches":["worker"],"checks":["A mail arrives at 80 requests."]}]}`, nil
	}
	t.Cleanup(func() { runIdeaPrint = was })
	res, err := buildIdea(dir, id.Meta.Slug, "", "", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || !strings.Contains(prompts[0], "Backend: limits endpoint") {
		t.Fatalf("the planner reads the old tickets once: %d", len(prompts))
	}
	if !res.Recut || len(res.Tasks) != 2 || !res.Plan.Stack {
		t.Fatalf("two feature tasks on a stacked plan: recut=%v tasks=%d stack=%v", res.Recut, len(res.Tasks), res.Plan.Stack)
	}
	first := res.Tasks[0]
	if strings.Join(first.Touches, ",") != "api,web" || !strings.Contains(first.Body, "Done when:\n- The owner sees the banner") || !strings.Contains(first.Body, "Touches: api, web.") {
		t.Fatalf("the task carries what it touches and its checks: %+v", first)
	}
	after, _ := idea.Load(dir, id.Meta.Slug)
	if after.Meta.SplitBy != splitByFeature || len(after.Meta.Tickets) != 2 || len(after.Meta.Tickets[0].Checks) != 1 {
		t.Fatalf("the page keeps the new split: %+v", after.Meta.Tickets)
	}
	last := after.Comments.Comments[len(after.Comments.Comments)-1]
	if !strings.Contains(last.Text, "cut again by feature") {
		t.Fatalf("the page says the tickets were cut again: %q", last.Text)
	}
	again, err := buildIdea(dir, id.Meta.Slug, "", "", 1, false)
	if err != nil || again.Recut || len(prompts) != 1 {
		t.Fatalf("a feature split is built as it reads: recut=%v calls=%d err=%v", again.Recut, len(prompts), err)
	}
}

func TestBuildKeepsThePagesSplitWhenTheReCutFails(t *testing.T) {
	dir := phoneBoard(t, false)
	quietIdeaNotify(t)
	shopWorkspace(t, dir)
	id := layeredPage(t, dir)
	was := runIdeaPrint
	runIdeaPrint = func(context.Context, string, string, string, string) (string, error) {
		return "", errors.New("no agent on this machine")
	}
	t.Cleanup(func() { runIdeaPrint = was })
	res, err := buildIdea(dir, id.Meta.Slug, "", "", 1, false)
	if err != nil || res.Recut || len(res.Tasks) != 3 {
		t.Fatalf("the three tickets are built as they read: recut=%v tasks=%d err=%v", res.Recut, len(res.Tasks), err)
	}
	if left := watch.LoadTasks(dir).ByIdea(id.Meta.Slug); len(left) != 3 {
		t.Fatalf("tasks on the board: %d", len(left))
	}
}

func TestReCutRefusesOnceTicketsAreOnTheBoard(t *testing.T) {
	dir := phoneBoard(t, false)
	id := layeredPage(t, dir)
	id.Meta.Tickets[0].Ref = "TASK-1"
	if _, err := recutIdea(dir, id); err == nil || !strings.Contains(err.Error(), "already on the board") {
		t.Fatalf("refused: %v", err)
	}
	if needsRecut(id.Meta) {
		t.Fatal("a split with a ticket on the board is never cut again")
	}
}

func TestAReCutThatIsStillLayeredIsAskedOnceMore(t *testing.T) {
	root := t.TempDir()
	compose := "services:\n  api:\n    path: ./backend\n  web:\n    path: ./frontend\n"
	if err := os.WriteFile(filepath.Join(root, "corgi-compose.yml"), []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	var prompts []string
	was := runIdeaPrint
	runIdeaPrint = func(_ context.Context, _, _, _, prompt string) (string, error) {
		prompts = append(prompts, prompt)
		if len(prompts) == 1 {
			return `{"tasks":[{"title":"Limits in the api","touches":["api"]},{"title":"Limits in the web","touches":["web"]}]}`, nil
		}
		return `{"tasks":[{"title":"Show the owner a banner at the limit","touches":["api","web"]}]}`, nil
	}
	t.Cleanup(func() { runIdeaPrint = was })
	id := &idea.Idea{Doc: "# Limits\n", Meta: idea.Meta{Tickets: []idea.Ticket{{ID: 1, Title: "Backend: limits"}, {ID: 2, Title: "Frontend: limits"}}}}
	ans, err := recutByFeature(id, root, "sonnet")
	if err != nil || len(ans.Tasks) != 1 || len(prompts) != 2 {
		t.Fatalf("second answer wins: tasks=%d calls=%d err=%v", len(ans.Tasks), len(prompts), err)
	}
	if !strings.Contains(prompts[1], "refused") || !strings.Contains(prompts[1], "backend: 1") {
		t.Fatalf("the second ask says why: %q", prompts[1][len(prompts[1])-200:])
	}
}
