package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestAttemptsGroupAndPick(t *testing.T) {
	list := []sessions.Session{
		{ID: "a", Label: "api", Attempt: "ABC-1/1", Status: sessions.StatusDone, Branch: "corgi/ABC-1-1", Changes: &sessions.Changes{Files: 3, Lines: 80}, Tests: &sessions.TestRun{OK: true, Cmd: "go test"}, Gate: &sessions.GateRun{OK: true}},
		{ID: "b", Label: "api", Attempt: "ABC-1/2", Status: sessions.StatusWorking, Branch: "corgi/ABC-1-2"},
		{ID: "c", Label: "api", Attempt: "ABC-1/3", Status: sessions.StatusGone},
		{ID: "d", Label: "web", Attempt: "XYZ-9/1", Status: sessions.StatusDone},
		{ID: "e", Label: "web", Status: sessions.StatusDone},
	}
	groups := attemptGroups(list, "")
	if len(groups) != 2 || groups[0].Ref != "ABC-1" || len(groups[0].Attempts) != 2 || groups[1].Ref != "XYZ-9" {
		t.Fatalf("%+v", groups)
	}
	first := groups[0].Attempts[0]
	if first.N != "1" || first.Changes != "3 files · 80 lines" || first.Tests != "tests ✓" || first.Gate != "done ✓" {
		t.Fatalf("%+v", first)
	}
	if only := attemptGroups(list, "xyz-9"); len(only) != 1 || only[0].Ref != "XYZ-9" {
		t.Fatalf("%+v", only)
	}
	cmds := pickCommands(groups[0], "1")
	if len(cmds) != 3 || cmds[0].Action != command.ActionNote || cmds[0].SessionID != "a" || cmds[0].Note != "picked of ABC-1" {
		t.Fatalf("%+v", cmds)
	}
	if cmds[1].Action != command.ActionInterrupt || cmds[1].SessionID != "b" || cmds[2].Action != command.ActionNote || cmds[2].Note != "not picked - attempt 1 was" {
		t.Fatalf("%+v", cmds[1:])
	}
}

func TestWorkOnFansOut(t *testing.T) {
	dir := phoneBoard(t, true)
	os.MkdirAll(filepath.Join(dir, "watch"), 0o700)
	data, _ := json.Marshal(watch.Event{Key: "linear:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "Login loops", Workspace: "api"})
	os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), append(data, '\n'), 0o600)
	key := "linear:ABC-1"
	cmds, status, msg := workOnCommands(dir, []string{key}, workOnOptions{Source: "cli"}, 3, []string{"opus", "sonnet"})
	if status != 0 {
		t.Fatal(msg)
	}
	if len(cmds) != 3 {
		t.Fatalf("%d commands", len(cmds))
	}
	for i, c := range cmds {
		if !strings.Contains(c.Command, fmt.Sprintf(" --attempt %d ", i+1)) || !strings.Contains(c.Command, " --isolate ") {
			t.Fatalf("attempt %d: %s", i+1, c.Command)
		}
	}
	if !strings.Contains(cmds[0].Command, "--model opus") || !strings.Contains(cmds[1].Command, "--model sonnet") || !strings.Contains(cmds[2].Command, "--model opus") {
		t.Fatalf("models in turn: %v", cmds)
	}
	if _, status, _ := workOnCommands(dir, []string{key}, workOnOptions{}, 9, nil); status != 400 {
		t.Fatal("capped")
	}
	one, _, _ := workOnCommands(dir, []string{key}, workOnOptions{}, 0, nil)
	if len(one) != 1 || strings.Contains(one[0].Command, "--attempt") {
		t.Fatalf("one is one: %v", one)
	}
}
