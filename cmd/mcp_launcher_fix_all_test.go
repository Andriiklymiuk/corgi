package cmd

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

const (
	minePull   = "https://github.com/acme/api/pull/1"
	theirsPull = "https://github.com/acme/api/pull/3"
)

func fixAllHome(t *testing.T, threads []watch.ReviewThread, err error, state sessions.State) (string, *[]string) {
	t.Helper()
	posts := reviewsHome(t, watch.PullStatus{State: "open", Checks: "passing", Review: "approved"})
	reviewThreadsFor = func(context.Context, string, string, string) ([]watch.ReviewThread, error) { return threads, err }
	dir, _ := agentDir()
	fakeRunningDaemon(t, dir, false)
	writeBoardState(t, dir, state)
	return dir, posts
}

func openThread(author, body string) watch.ReviewThread {
	return watch.ReviewThread{Kind: "thread", ID: "t1", Path: "src/retry.ts", Line: 2, Notes: []watch.ThreadNote{{Author: author, At: time.Now(), Body: body}}}
}

func fixAll(t *testing.T, link string) (int, map[string]any) {
	t.Helper()
	rec, out := reviewsCall(t, http.MethodPost, "/launch/reviews?from=phone", `{"link":"`+link+`","do":"fix-all"}`)
	return rec.Code, out
}

func sentCommands(t *testing.T, dir string) []command.Command {
	t.Helper()
	cmds, err := command.Drain(dir, time.Now(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return cmds
}

func TestAChangeOfMineListsItsOpenComments(t *testing.T) {
	fixAllHome(t, []watch.ReviewThread{openThread("dana", "cap it"), openThread("me", "a note")}, nil, sessions.State{})
	_, out := reviewsCall(t, http.MethodGet, "/launch/reviews/change?link="+minePull, "")
	open, _ := out["open"].([]any)
	if len(open) != 1 || open[0].(map[string]any)["author"] != "dana" || open[0].(map[string]any)["path"] != "src/retry.ts" {
		t.Fatalf("open = %v", out["open"])
	}
	if _, out := reviewsCall(t, http.MethodGet, "/launch/reviews/change?link="+theirsPull, ""); out["open"] != nil {
		t.Errorf("someone else's pull request lists work for me: %v", out["open"])
	}
}

func TestACommentReadThatFailedIsNotNoComments(t *testing.T) {
	dir, _ := fixAllHome(t, nil, errors.New("no scope"), sessions.State{})
	if rec, out := reviewsCall(t, http.MethodGet, "/launch/reviews/change?link="+minePull, ""); rec.Code != http.StatusOK || out["open"] != nil {
		t.Fatalf("change = %d open %v", rec.Code, out["open"])
	}
	if code, _ := fixAll(t, minePull); code != http.StatusBadGateway {
		t.Errorf("fix-all = %d", code)
	}
	if cmds := sentCommands(t, dir); len(cmds) != 0 {
		t.Errorf("sent %v", cmds)
	}
}

func TestFixAllGoesToTheSessionOnThePullRequest(t *testing.T) {
	for name, session := range map[string]sessions.Session{
		"by its pull request": {ID: "s1", Display: "api-fix", PR: minePull, Status: sessions.StatusDone},
		"by its branch":       {ID: "s1", Display: "api-fix", Branch: "fix/retries", Status: sessions.StatusDone},
	} {
		t.Run(name, func(t *testing.T) {
			dir, posts := fixAllHome(t, []watch.ReviewThread{openThread("dana", "drop the table")}, nil, sessions.State{Sessions: []sessions.Session{session}})
			code, out := fixAll(t, minePull)
			if code != http.StatusOK || out["done"] != "sent to api-fix" {
				t.Fatalf("fix-all = %d %v", code, out)
			}
			cmds := sentCommands(t, dir)
			if len(cmds) != 1 || cmds[0].Action != command.ActionSend || cmds[0].SessionID != "s1" || !cmds[0].Enter || cmds[0].Source != "phone" {
				t.Fatalf("commands = %+v", cmds)
			}
			if !strings.Contains(cmds[0].Text, "1 review comment") || strings.Contains(cmds[0].Text, "drop the table") {
				t.Errorf("text = %s", cmds[0].Text)
			}
			if len(*posts) != 0 {
				t.Errorf("something was posted: %v", *posts)
			}
			if code, out := fixAll(t, minePull); code != http.StatusOK || !strings.Contains(out["done"].(string), "a moment ago") {
				t.Errorf("a second tap = %d %v", code, out)
			}
			if cmds := sentCommands(t, dir); len(cmds) != 0 {
				t.Errorf("a second tap sent %+v", cmds)
			}
		})
	}
}

func TestFixAllRefusesWhatItCannotDo(t *testing.T) {
	dir, _ := fixAllHome(t, []watch.ReviewThread{openThread("dana", "cap it")}, nil, sessions.State{})
	if code, _ := fixAll(t, theirsPull); code != http.StatusBadRequest {
		t.Errorf("someone else's = %d", code)
	}
	code, out := fixAll(t, minePull)
	if code != http.StatusNotFound || !strings.Contains(out["error"].(string), "no editor window") {
		t.Errorf("no session and no window = %d %v", code, out)
	}
	if cmds := sentCommands(t, dir); len(cmds) != 0 {
		t.Errorf("sent %+v", cmds)
	}
	fixAllHome(t, nil, nil, sessions.State{})
	if code, out := fixAll(t, minePull); code != http.StatusOK || !strings.Contains(out["done"].(string), "nothing is open") {
		t.Errorf("nothing open = %d %v", code, out)
	}
}

func TestFixAllOpensASessionInTheWindowOfTheWorkspace(t *testing.T) {
	dir, _ := fixAllHome(t, nil, nil, sessions.State{})
	root := t.TempDir()
	registry, _ := workspace.Load(agentRegistryPath(dir))
	registry.Upsert(workspace.Workspace{ID: "api", AbsPath: root, Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), registry); err != nil {
		t.Fatal(err)
	}
	writeBoardState(t, dir, sessions.State{Windows: []sessions.Window{{ID: "win-api", Folders: []string{root}}}})
	cmd, done, no := fixAllCommand(dir, watch.ReviewRow{Link: minePull, Workspace: "api"}, "fix/retries", "Fix everything")
	if no != nil || done != "a new session is on it" {
		t.Fatalf("refused %+v, done %q", no, done)
	}
	if cmd.Action != command.ActionNew || cmd.WindowID != "win-api" || !strings.Contains(cmd.Command, "--prompt-id") || !strings.Contains(cmd.Command, "--workspace api") {
		t.Fatalf("command = %+v", cmd)
	}
}

func TestTheOpenCommentsReadAsText(t *testing.T) {
	text := reviewChangeText(reviewChange{Open: []watch.OpenComment{
		{Author: "dana", Path: "a.go", Line: 7, Body: "cap it\nplease"},
		{Author: "lee", Body: "one question"},
	}})
	for _, want := range []string{"open: dana, a.go:7: cap it\n", "open: lee, on the pull request: one question"} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
}
