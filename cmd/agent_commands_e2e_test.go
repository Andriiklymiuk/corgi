package cmd

import (
	"os"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func cli(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var console string
	var code int
	out := captureStdout(t, func() { console, code = runCLI(t, args...) })
	utils.JSONOutput = false
	return out + console, code
}

func inWorkspace(t *testing.T, ws string) {
	t.Helper()
	wd, _ := os.Getwd()
	if err := os.Chdir(ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
}

func TestTasksGoThroughTheirColumnsFromTheCommandLine(t *testing.T) {
	dir, ws := watchFixture(t, nil)
	inWorkspace(t, ws)
	if out, code := cli(t, "agent", "task", "add", "Meta", "SDK", "--body", "App Events"); code != 0 || !strings.Contains(out, "TASK-1 on the board in acme-stack: Meta SDK") {
		t.Fatalf("add: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "task", "add", "Other", "--workspace", "nope"); code != 2 {
		t.Fatalf("an unknown workspace is refused: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "task", "list"); code != 0 || !strings.Contains(out, "TASK-1") || !strings.Contains(out, "Todo") {
		t.Fatalf("list: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "task", "move", "TASK-1", "Doing"); code != 0 || !strings.Contains(out, "Doing") {
		t.Fatalf("move: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "task", "edit", "TASK-1", "--title", "Meta SDK on iOS"); code != 0 || !strings.Contains(out, "TASK-1") {
		t.Fatalf("edit: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "task", "done", "1"); code != 0 || !strings.Contains(out, "Done") {
		t.Fatalf("done: %d %s", code, out)
	}
	if task, ok := watch.LoadTasks(dir).Find("TASK-1"); !ok || task.State != "Done" || task.Title != "Meta SDK on iOS" || task.Body != "App Events" {
		t.Fatalf("stored: %+v", task)
	}
	if out, code := cli(t, "--json", "agent", "task", "list"); code != 0 || !strings.Contains(out, `Meta SDK on iOS`) {
		t.Fatalf("json list: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "task", "move", "TASK-1", "Sideways"); code == 0 {
		t.Fatalf("an unknown column is refused: %s", out)
	}
	if out, code := cli(t, "agent", "task", "rm", "TASK-1"); code != 0 || !strings.Contains(out, "TASK-1") {
		t.Fatalf("rm: %d %s", code, out)
	}
	if _, ok := watch.LoadTasks(dir).Find("TASK-1"); ok {
		t.Fatal("removed")
	}
	if _, code := cli(t, "agent", "task", "rm", "TASK-9"); code == 0 {
		t.Fatal("removing what is not there fails")
	}
}

func TestRoutinesAreScheduledListedAndRemoved(t *testing.T) {
	dir, ws := watchFixture(t, &config.WatchConfig{Enabled: true})
	inWorkspace(t, ws)
	if out, code := cli(t, "agent", "routine", "catalog"); code != 0 || !strings.Contains(out, "digest") || !strings.Contains(out, "lessons") {
		t.Fatalf("catalog: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "routine", "add", "digest"); code != 0 || !strings.Contains(out, "digest") {
		t.Fatalf("add: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "routine", "add", "nightly", "--prompt", "Say hi", "--schedule", "daily 03:00"); code != 0 {
		t.Fatalf("add a custom one: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "routine", "add", "nightly2", "--prompt", "Say hi", "--schedule", "whenever"); code == 0 {
		t.Fatalf("a schedule corgi cannot read is refused: %s", out)
	}
	if out, code := cli(t, "agent", "routine", "list"); code != 0 || !strings.Contains(out, "digest") || !strings.Contains(out, "nightly") {
		t.Fatalf("list: %d %s", code, out)
	}
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	if got := user.Workspaces["acme-stack"].Routines; len(got) != 2 {
		t.Fatalf("stored: %+v", got)
	}
	if out, code := cli(t, "agent", "routine", "rm", "nightly"); code != 0 {
		t.Fatalf("rm: %d %s", code, out)
	}
	user, _ = config.LoadUser(agentUserConfigPath(dir))
	if got := user.Workspaces["acme-stack"].Routines; len(got) != 1 || got[0].Kind != "digest" {
		t.Fatalf("after rm: %+v", got)
	}
}

func TestAHandoffIsWrittenShownListedAndRemoved(t *testing.T) {
	_, ws := watchFixture(t, nil)
	newRepo(t, ws)
	inWorkspace(t, ws)
	if out, code := cli(t, "agent", "handoff", "--ref", "ABC-7", "--done", "the API", "--remaining", "the web form", "--next", "wire the form", "--decision", "keep v1 routes", "--verify", "true"); code != 0 {
		t.Fatalf("write: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "handoff", "show", "ABC-7"); code != 0 || !strings.Contains(out, "the web form") || !strings.Contains(out, "keep v1 routes") {
		t.Fatalf("show: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "handoff", "list"); code != 0 || !strings.Contains(out, "ABC-7") {
		t.Fatalf("list: %d %s", code, out)
	}
	if out, code := cli(t, "--json", "agent", "handoff", "show", "ABC-7"); code != 0 || !strings.Contains(out, `"remaining"`) {
		t.Fatalf("json show: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "handoff", "--ref", "ABC-8", "--blocked", "no staging token", "--uncertain", "who owns it"); code != 0 {
		t.Fatalf("blocked: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "handoff", "rm", "ABC-7"); code != 0 {
		t.Fatalf("rm: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "handoff", "show", "ABC-7"); code == 0 {
		t.Fatalf("gone: %s", out)
	}
}

func TestBotsAreAddedShownListedAndRemoved(t *testing.T) {
	_, ws := watchFixture(t, nil)
	inWorkspace(t, ws)
	if out, code := cli(t, "agent", "bot", "add", "reviewer", "--workspace", "acme-stack", "--title", "Reviewer", "--soul", "You review pull requests.", "--model", "sonnet", "--color", "blue"); code != 0 {
		t.Fatalf("add: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "bot", "add", "Bad Name!", "--workspace", "acme-stack", "--soul", "x"); code == 0 {
		t.Fatalf("a name with spaces is refused: %s", out)
	}
	if out, code := cli(t, "agent", "bot", "list"); code != 0 || !strings.Contains(out, "reviewer") {
		t.Fatalf("list: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "bot", "show", "reviewer"); code != 0 || !strings.Contains(out, "You review pull requests.") {
		t.Fatalf("show: %d %s", code, out)
	}
	if out, code := cli(t, "--json", "agent", "bot", "list"); code != 0 || !strings.Contains(out, `"reviewer"`) {
		t.Fatalf("json: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "bot", "rm", "reviewer"); code != 0 {
		t.Fatalf("rm: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "bot", "show", "reviewer"); code == 0 {
		t.Fatalf("gone: %s", out)
	}
}

func TestAScopeIsSetWidenedShownAndCleared(t *testing.T) {
	_, ws := watchFixture(t, nil)
	inWorkspace(t, ws)
	if out, code := cli(t, "agent", "scope", "set", "abc-7", "--path", "api/limits/**", "--done", "go test ./..."); code != 0 {
		t.Fatalf("set: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "scope", "add", "ABC-7", "--path", "shared/types.ts"); code != 0 || !strings.Contains(out, "1 widening") {
		t.Fatalf("add: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "scope", "show", "ABC-7"); code != 0 || !strings.Contains(out, "api/limits/**") || !strings.Contains(out, "shared/types.ts") {
		t.Fatalf("show: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "scope", "show"); code != 0 || !strings.Contains(out, "ABC-7") {
		t.Fatalf("show all: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "scope", "clear", "ABC-7"); code != 0 {
		t.Fatalf("clear: %d %s", code, out)
	}
}

func TestPeersListSaysHowToPairWhenThereAreNone(t *testing.T) {
	watchFixture(t, nil)
	if out, code := cli(t, "agent", "peers"); code != 0 || !strings.Contains(out, "corgi agent peers invite") {
		t.Fatalf("list: %d %s", code, out)
	}
	if out, code := cli(t, "--json", "agent", "peers"); code != 0 || !strings.Contains(out, `"peers"`) {
		t.Fatalf("json: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "peers", "lead"); code != 0 {
		t.Fatalf("lead: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "peers", "lead", "off"); code != 0 {
		t.Fatalf("lead off: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "peers", "rm", "ghost"); code == 0 {
		t.Fatalf("removing a peer that is not there fails: %s", out)
	}
}

func TestPlanStatusAndStopWorkOnAPlanOnDisk(t *testing.T) {
	dir, ws := watchFixture(t, nil)
	inWorkspace(t, ws)
	tasks := watch.LoadTasks(dir)
	a, _ := tasks.Add("Schema", "", "acme-stack", "you", time.Now())
	b, _ := tasks.Add("API", "", "acme-stack", "you", time.Now())
	if _, err := watch.LoadPlans(dir).Add("Ship limits", "two steps", "acme-stack", "", "you", []watch.PlanTask{{ID: a.ID}, {ID: b.ID, After: []int{a.ID}}}, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if out, code := cli(t, "agent", "plan", "status"); code != 0 || !strings.Contains(out, "P-1") || !strings.Contains(out, "Ship limits") {
		t.Fatalf("status: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "plan", "status", "P-1"); code != 0 || !strings.Contains(out, "Schema") {
		t.Fatalf("one plan: %d %s", code, out)
	}
	if out, code := cli(t, "--json", "agent", "plan", "status"); code != 0 || !strings.Contains(out, `"Ship limits"`) {
		t.Fatalf("json: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "plan", "stop", "P-1"); code != 0 {
		t.Fatalf("stop: %d %s", code, out)
	}
	if p, _ := watch.LoadPlans(dir).Find("P-1"); p.State != watch.PlanStopped {
		t.Fatalf("stopped: %+v", p)
	}
	if task, _ := watch.LoadTasks(dir).Find(b.Key()); task.State != "Canceled" {
		t.Fatalf("its Todo tasks go to Canceled: %+v", task)
	}
	if out, code := cli(t, "agent", "plan", "status", "P-9"); code == 0 {
		t.Fatalf("no such plan: %s", out)
	}
}
