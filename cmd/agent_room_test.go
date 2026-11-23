package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/rooms"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func roomCmdHome(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTheRoomCommandsPostReadShapeAndGrant(t *testing.T) {
	dir := roomCmdHome(t)
	if out := captureStdout(t, func() { agentRoomCmd.Run(agentRoomCmd, nil) }); !strings.Contains(out, "no rooms yet") {
		t.Fatalf("empty = %q", out)
	}
	_ = agentRoomPostCmd.Flags().Set("as", "scanner")
	_ = agentRoomPostCmd.Flags().Set("label", "kind=bug")
	if out := captureStdout(t, func() { agentRoomPostCmd.Run(agentRoomPostCmd, []string{"security", "SQL", "from", "input"}) }); out != "m1 in security\n" {
		t.Fatalf("post = %q", out)
	}
	_ = agentRoomPostCmd.Flags().Set("thread", "m1")
	captureStdout(t, func() { agentRoomPostCmd.Run(agentRoomPostCmd, []string{"security", "fixed in #8"}) })
	_ = agentRoomPostCmd.Flags().Set("thread", "")
	out := captureStdout(t, func() { agentRoomReadCmd.Run(agentRoomReadCmd, []string{"security"}) })
	if !strings.Contains(out, "m1    scanner") || !strings.Contains(out, "          fixed in #8") {
		t.Fatalf("read = %q", out)
	}
	_ = agentRoomReadCmd.Flags().Set("after", "m2")
	if out := captureStdout(t, func() { agentRoomReadCmd.Run(agentRoomReadCmd, []string{"security"}) }); out != "nothing new\n" {
		t.Fatalf("after = %q", out)
	}
	_ = agentRoomReadCmd.Flags().Set("after", "")
	if out := captureStdout(t, func() { agentRoomCmd.Run(agentRoomCmd, nil) }); !strings.Contains(out, "security") || !strings.Contains(out, "2") {
		t.Fatalf("list = %q", out)
	}

	shape := filepath.Join(t.TempDir(), "finding.json")
	_ = os.WriteFile(shape, []byte(`{"type":"object","required":["repo"],"properties":{"repo":{"type":"string"}}}`), 0o600)
	_ = agentRoomShapeAddCmd.Flags().Set("file", shape)
	_ = agentRoomShapeAddCmd.Flags().Set("about", "a finding")
	if out := captureStdout(t, func() { agentRoomShapeAddCmd.Run(agentRoomShapeAddCmd, []string{"finding@1"}) }); out != "saved finding@1\n" {
		t.Fatalf("shape add = %q", out)
	}
	if out := captureStdout(t, func() { agentRoomShapeCmd.Run(agentRoomShapeCmd, nil) }); !strings.Contains(out, "finding@1") || !strings.Contains(out, "a finding") {
		t.Fatalf("shapes = %q", out)
	}
	_ = agentRoomPostCmd.Flags().Set("shape", "finding@1")
	_ = agentRoomPostCmd.Flags().Set("data", `{"nope":1}`)
	if code := exitsWith(t, func() { agentRoomPostCmd.Run(agentRoomPostCmd, []string{"security"}) }); code != 2 {
		t.Fatalf("data that does not fit exits %d", code)
	}
	_ = agentRoomPostCmd.Flags().Set("shape", "")
	_ = agentRoomPostCmd.Flags().Set("data", "")

	if out := captureStdout(t, func() { agentRoomGrantsCmd.Run(agentRoomGrantsCmd, nil) }); !strings.Contains(out, "no grants") {
		t.Fatalf("grants = %q", out)
	}
	if out := captureStdout(t, func() { agentRoomGrantCmd.Run(agentRoomGrantCmd, []string{"ci-runner", "security/=read,post"}) }); out != "g1: ci-runner may post, read in security/\n" {
		t.Fatalf("grant = %q", out)
	}
	captureStdout(t, func() { agentRoomGrantCmd.Run(agentRoomGrantCmd, []string{"everyone", "all=none"}) })
	if out := captureStdout(t, func() { agentRoomCanCmd.Run(agentRoomCanCmd, []string{"ci-runner", "security/x", "post"}) }); !strings.HasPrefix(out, "yes: g1") {
		t.Fatalf("can = %q", out)
	}
	if out := captureStdout(t, func() { agentRoomCanCmd.Run(agentRoomCanCmd, []string{"phone", "deploys"}) }); !strings.HasPrefix(out, "no: g2 gives everyone nothing in every room") {
		t.Fatalf("cannot = %q", out)
	}
	if out := captureStdout(t, func() { agentRoomRevokeCmd.Run(agentRoomRevokeCmd, []string{"g1"}) }); !strings.HasPrefix(out, "revoked g1") {
		t.Fatalf("revoke = %q", out)
	}
	if len(rooms.Grants(dir)) != 1 {
		t.Fatalf("grants left = %d", len(rooms.Grants(dir)))
	}
}

func TestTaskNextTakesWorkAndExitsThreeWhenNothingIsReady(t *testing.T) {
	dir := roomCmdHome(t)
	task, _ := watch.LoadTasks(dir).Add("fix the login", "the body", "", "me", time.Now())
	_ = agentTaskNextCmd.Flags().Set("as", "fixer-1")
	out := captureStdout(t, func() { agentTaskNextCmd.Run(agentTaskNextCmd, nil) })
	if !strings.HasPrefix(out, task.Ref()+"  fix the login\nthe body\nyours until") {
		t.Fatalf("next = %q", out)
	}
	if code := exitsWith(t, func() { agentTaskNextCmd.Run(agentTaskNextCmd, nil) }); code != 3 {
		t.Fatalf("nothing ready exits %d", code)
	}
	_ = agentTaskRenewCmd.Flags().Set("as", "fixer-1")
	if out := captureStdout(t, func() { agentTaskRenewCmd.Run(agentTaskRenewCmd, []string{task.Ref()}) }); !strings.Contains(out, "is fixer-1's until") {
		t.Fatalf("renew = %q", out)
	}
	_ = agentTaskDoneCmd.Flags().Set("as", "fixer-1")
	if out := captureStdout(t, func() { agentTaskDoneCmd.Run(agentTaskDoneCmd, []string{task.Ref(), "fixed,", "see", "#8"}) }); !strings.Contains(out, "done") {
		t.Fatalf("done = %q", out)
	}
	done, _ := watch.LoadTasks(dir).Find(task.Ref())
	if done.State != "Done" || len(done.Comments) != 1 || done.Comments[0].Text != "fixed, see #8" || done.WantsRun() {
		t.Fatalf("done = %+v", done)
	}
}
