package daemon

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

func TestAHeadlessTurnResumesAGoneSession(t *testing.T) {
	d := trackingDaemon(t)
	d.Sessions.Load()
	var ran []string
	var mu sync.Mutex
	prev := claudeCommand
	claudeCommand = func(ctx context.Context, dir string, env []string, args ...string) *exec.Cmd {
		mu.Lock()
		ran = append(ran, dir+" "+strings.Join(env, ",")+" "+strings.Join(args, " "))
		mu.Unlock()
		return exec.CommandContext(ctx, "echo", `{"result":"done","total_cost_usd":0.01}`)
	}
	t.Cleanup(func() { claudeCommand = prev })
	now := time.Now()
	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s1", Cwd: "/tmp/acme-api", ConfigDir: "/tmp/cfg", ClaudePID: 100, At: now})
	d.Sessions.Apply(sessions.Event{Name: "SessionEnd", SessionID: "s1", At: now})
	if s, ok := d.Sessions.LookupEnded("s1"); !ok || s.Status != sessions.StatusGone || s.Cwd != "/tmp/acme-api" {
		t.Fatalf("a session that left is remembered with its checkout: %+v %v", s, ok)
	}
	handled := d.handleSessionCommand(context.Background(), command.Command{Action: command.ActionContinue, SessionID: "s1", Text: "run the tests"})
	d.runs.Wait()
	if !handled {
		t.Fatal("continue is a board command")
	}
	if n := d.Sessions.Snapshot(time.Now()).Notice; n != "" {
		t.Fatalf("notice: %s", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 1 || !strings.HasPrefix(ran[0], "/tmp/acme-api CLAUDE_CONFIG_DIR=/tmp/cfg,CORGI_OMIT=useAwsVpn -p run the tests --resume s1") {
		t.Fatalf("one headless turn in the session's checkout under its account: %v", ran)
	}
	if s, _ := d.Sessions.LookupEnded("s1"); s.Headless == nil || s.Headless.Turns != 1 || s.Headless.At.IsZero() {
		t.Fatalf("the ended row says a headless turn ran: %+v", s.Headless)
	}
	if st := d.Sessions.Snapshot(time.Now()); len(st.Ended) != 1 || st.Ended[0].ID != "s1" {
		t.Fatalf("the board carries the ended: %+v", st.Ended)
	}

	d.Sessions.Apply(sessions.Event{Name: "UserPromptSubmit", SessionID: "s2", Cwd: "/tmp/b", ClaudePID: 200, Ancestors: []int{200}, At: now})
	d.handleSessionCommand(context.Background(), command.Command{Action: command.ActionContinue, SessionID: "s2", Text: "hello"})
	d.runs.Wait()
	if len(ran) != 1 {
		t.Fatalf("a session with a terminal is typed into, not resumed: %v", ran)
	}
}
