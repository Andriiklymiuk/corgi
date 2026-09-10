package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/brief"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/usage"
)

func TestSessionContextTellsTheSessionWhatTheDaemonKnows(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(t.TempDir(), "acme-api")
	os.MkdirAll(filepath.Join(root, ".corgi", "memory"), 0o755)
	os.WriteFile(filepath.Join(root, ".corgi", "memory", "index.md"), []byte("# memory\n- [a](a.md) one\n- [b](b.md) two\n"), 0o644)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/feat/x\n"), 0o644)

	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.Local)
	st := sessions.State{
		Sessions: []sessions.Session{
			{ID: "me", Cwd: root, Folder: root, Status: sessions.StatusDone},
			{ID: "s2", Cwd: root, Folder: root, Title: "Referrals", Status: sessions.StatusWorking, Detail: "Bash go test", Branch: "fix/login", StatusSince: now.Add(-3 * time.Minute)},
			{ID: "s3", Cwd: "/elsewhere", Folder: "/elsewhere", Status: sessions.StatusWorking},
			{ID: "s4", Cwd: root, Folder: root, Status: sessions.StatusGone},
		},
		Accounts: []sessions.Account{{ConfigDir: "", Limits: &usage.Limits{
			FiveHour: usage.Window{Percent: 57, ResetsAt: now.Add(2 * time.Hour)},
			SevenDay: usage.Window{Percent: 41},
		}, Forecast: &usage.Forecast{FiveHour: &usage.WindowForecast{Safe: false, ExhaustAt: now.Add(time.Hour)}}}},
	}
	raw, _ := json.Marshal(st)
	os.WriteFile(daemon.SessionsPath(dir), raw, 0o644)
	if err := brief.Write(dir, brief.Brief{WorkspaceID: "acme-api", EndedAt: now.Add(-2 * time.Hour), Repos: []brief.RepoState{{Service: "api", Branch: "feat/x", Dirty: true}}}); err != nil {
		t.Fatal(err)
	}

	got := sessionContext(dir, contextHookInput{SessionID: "me", Cwd: root, Source: "startup"}, "", now)
	for _, want := range []string{
		"corgi · acme-api on feat/x",
		`another session in this workspace: ● "Referrals" working on fix/login (Bash go test) 3m`,
		"budget: 5h 57% (resets 12:00) · week 41% — at this pace the 5h window runs out at 11:00",
		"last session here ended 2h ago: was on feat/x · 1 repo has uncommitted changes",
		"workspace memory: 2 facts in .corgi/memory/index.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "elsewhere") || strings.Contains(got, "s4") {
		t.Fatalf("other workspaces and gone sessions stay out:\n%s", got)
	}

	if got := sessionContext(dir, contextHookInput{SessionID: "me", Cwd: root, Source: "resume"}, "", now); strings.Contains(got, "last session here") {
		t.Fatal("a resumed session does not need its own handover note")
	}
}

func TestSessionContextIsSilentWithNothingToSay(t *testing.T) {
	dir := t.TempDir()
	if got := sessionContext(dir, contextHookInput{SessionID: "me", Cwd: t.TempDir()}, "", time.Now()); got != "" {
		t.Fatalf("empty board, no brief, no memory: %q", got)
	}
}

func TestContextHookPrintsHookSpecificOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	var out bytes.Buffer
	runContextHook(strings.NewReader(`{"session_id":"s","hook_event_name":"SessionStart","cwd":"/nowhere","source":"compact"}`), &out, func(string) string { return "" }, time.Now())
	if out.Len() != 0 {
		t.Fatalf("compact says nothing: %s", out.String())
	}
	runContextHook(strings.NewReader(`not json`), &out, func(string) string { return "" }, time.Now())
	if out.Len() != 0 {
		t.Fatal("garbage says nothing")
	}
}

func TestRoughAge(t *testing.T) {
	cases := map[time.Duration]string{20 * time.Second: "just now", 5 * time.Minute: "5m", 3 * time.Hour: "3h", 72 * time.Hour: "3d"}
	for d, want := range cases {
		if got := roughAge(d); got != want {
			t.Errorf("%v = %q, want %q", d, got, want)
		}
	}
}
