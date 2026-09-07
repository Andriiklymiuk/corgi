package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/proc"
	"andriiklymiuk/corgi/utils/agent/sessions"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

func fakeEnv(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func fakeChain(t *testing.T) {
	t.Helper()
	orig := proc.Lookup
	table := map[int]proc.Process{
		50: {PID: 50, PPID: 40, Name: "sh"},
		40: {PID: 40, PPID: 30, Name: "claude"},
		30: {PID: 30, PPID: 20, Name: "zsh"},
		20: {PID: 20, PPID: 1, Name: "Code Helper (Plugin)"},
	}
	proc.Lookup = func(pid int) (proc.Process, bool) { p, ok := table[pid]; return p, ok }
	t.Cleanup(func() { proc.Lookup = orig })
}

func TestEmitHookBuildsAnEventFromStdinAndTheProcessTree(t *testing.T) {
	fakeChain(t)
	stdin := strings.NewReader(`{"session_id":"s1","hook_event_name":"PermissionRequest","cwd":"/home/me/dev/acme","tool_name":"Bash","tool_input":{"command":"rm -rf /"},"transcript_path":"/secret"}`)
	env := fakeEnv(map[string]string{
		"CLAUDE_CONFIG_DIR": "/home/me/.claude-work", "CORGI_VSCODE_WINDOW": "win-1",
		"TERM_PROGRAM": "vscode", "TERM_SESSION_ID": "t-9",
	})
	ev, ok := runEmitHook(stdin, env, 50)
	if !ok {
		t.Fatal("a real hook payload must produce an event")
	}
	if ev.Name != "PermissionRequest" || ev.SessionID != "s1" || ev.Tool != "Bash" || ev.Cwd != "/home/me/dev/acme" {
		t.Fatalf("event = %+v", ev)
	}
	if ev.ClaudePID != 40 || len(ev.Ancestors) != 4 || ev.Ancestors[0] != 50 {
		t.Fatalf("pid chain: claude=%d ancestors=%v", ev.ClaudePID, ev.Ancestors)
	}
	if ev.Window != "win-1" || ev.ConfigDir != "/home/me/.claude-work" || ev.TermProgram != "vscode" || ev.TermSession != "t-9" {
		t.Fatalf("env: %+v", ev)
	}
	if ev.At.IsZero() {
		t.Fatal("stamped")
	}
	raw, _ := json.Marshal(ev)
	if strings.Contains(string(raw), "rm -rf") || strings.Contains(string(raw), "/secret") {
		t.Fatalf("tool inputs and the transcript path must never leave the hook: %s", raw)
	}
}

func TestEmitHookIgnoresWhatIsNotASession(t *testing.T) {
	fakeChain(t)
	cases := map[string]struct {
		stdin string
		env   map[string]string
	}{
		"subagent":    {stdin: `{"session_id":"s1","hook_event_name":"Stop","agent_id":"a1"}`},
		"remote":      {stdin: `{"session_id":"s1","hook_event_name":"Stop"}`, env: map[string]string{"CLAUDE_CODE_REMOTE": "true"}},
		"no session":  {stdin: `{"hook_event_name":"Stop"}`},
		"no event":    {stdin: `{"session_id":"s1"}`},
		"not json":    {stdin: `hello`},
		"empty stdin": {stdin: ``},
	}
	for name, c := range cases {
		if _, ok := runEmitHook(strings.NewReader(c.stdin), fakeEnv(c.env), 50); ok {
			t.Errorf("%s: must not emit", name)
		}
	}
	if _, ok := runEmitHook(nil, fakeEnv(nil), 50); ok {
		t.Error("nil stdin")
	}
}

func TestEmitHookReadsStopFailureErrorShapes(t *testing.T) {
	fakeChain(t)
	for stdin, want := range map[string]string{
		`{"session_id":"s","hook_event_name":"StopFailure","error_type":"rate_limit"}`:       "rate_limit",
		`{"session_id":"s","hook_event_name":"StopFailure","error":"overloaded"}`:            "overloaded",
		`{"session_id":"s","hook_event_name":"StopFailure","error":{"type":"server_error"}}`: "server_error",
		`{"session_id":"s","hook_event_name":"StopFailure"}`:                                 "",
	} {
		ev, _ := runEmitHook(strings.NewReader(stdin), fakeEnv(nil), 50)
		if ev.Error != want {
			t.Errorf("%s: error = %q, want %q", stdin, ev.Error, want)
		}
	}
}

func TestEmitHookWithoutAProbeStillEmits(t *testing.T) {
	orig := proc.Lookup
	proc.Lookup = func(int) (proc.Process, bool) { return proc.Process{}, false }
	t.Cleanup(func() { proc.Lookup = orig })
	ev, ok := runEmitHook(strings.NewReader(`{"session_id":"s1","hook_event_name":"Stop"}`), fakeEnv(nil), 50)
	if !ok || ev.ClaudePID != 0 || len(ev.Ancestors) != 0 {
		t.Fatalf("no chain is still an event: %+v %v", ev, ok)
	}
}

func TestDeliverEventSpoolsOnlyForARunningDaemon(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, _ := agentDir()
	deliverEvent(sessions.Event{Name: "Stop", SessionID: "s1"})
	if entries, _ := os.ReadDir(filepath.Join(dir, "commands")); len(entries) != 0 {
		t.Fatalf("no daemon, no spool file — the spool would grow forever: %d entries", len(entries))
	}
	// A record naming this very process passes the liveness and name checks.
	exe, _ := os.Executable()
	data, _ := json.Marshal(daemon.Info{PID: os.Getpid(), Executable: exe, Commands: true})
	_ = os.MkdirAll(dir, 0o700)
	if err := os.WriteFile(filepath.Join(dir, "daemon.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	deliverEvent(sessions.Event{Name: "Stop", SessionID: "s1"})
	if entries, _ := os.ReadDir(filepath.Join(dir, "commands")); len(entries) != 1 {
		t.Fatalf("spool has %d entries", len(entries))
	}
}

func TestTabTitleHook(t *testing.T) {
	label := func(cwd string) string { return "acme-api" }
	for stdin, want := range map[string]string{
		`{"session_id":"s","hook_event_name":"UserPromptSubmit","cwd":"/x"}`:                                        "● acme-api",
		`{"session_id":"s","hook_event_name":"Stop"}`:                                                               "✓ acme-api",
		`{"session_id":"s","hook_event_name":"StopFailure"}`:                                                        "▲ acme-api FAILED",
		`{"session_id":"s","hook_event_name":"Notification","notification_type":"permission_prompt"}`:               "▲ acme-api NEEDS YOU",
		`{"session_id":"s","hook_event_name":"Notification","notification_type":"agent_needs_input"}`:               "▲ acme-api NEEDS YOU",
		`{"session_id":"s","hook_event_name":"Notification","notification_type":"idle_prompt","message":"waiting"}`: "",
		`{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"Bash"}`:                                      "",
		`{"session_id":"s","hook_event_name":"Stop","agent_id":"sub"}`:                                              "",
		`nope`: "",
	} {
		var out bytes.Buffer
		runTabTitleHook(strings.NewReader(stdin), &out, label)
		if want == "" {
			if out.Len() != 0 {
				t.Errorf("%s: printed %q, want nothing", stdin, out.String())
			}
			continue
		}
		var got map[string]string
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v (%q)", stdin, err, out.String())
		}
		if got["terminalSequence"] != "\x1b]2;"+want+"\x07" {
			t.Errorf("%s: sequence = %q, want title %q", stdin, got["terminalSequence"], want)
		}
	}
	var out bytes.Buffer
	runTabTitleHook(strings.NewReader(`{"session_id":"s","hook_event_name":"Stop"}`), &out, func(string) string { return "bad\x1b]0;x\x07name" })
	if strings.Count(out.String(), "\\u001b") != 1 {
		t.Fatalf("control characters in a label must not reach the terminal: %q", out.String())
	}
}

func TestEnableTrackingMergesAndDisableStrips(t *testing.T) {
	cfgDir := t.TempDir()
	path := claudeUserSettingsPath(cfgDir)
	theirs := map[string]any{
		"model": "opus",
		"hooks": map[string]any{
			"Stop": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "say done"}}}},
		},
	}
	if err := writeJSONObject(path, theirs); err != nil {
		t.Fatal(err)
	}
	if err := enableTrackingIn(path, "/opt/homebrew/bin/corgi", true); err != nil {
		t.Fatal(err)
	}
	if err := enableTrackingIn(path, "/opt/homebrew/bin/corgi", true); err != nil {
		t.Fatal("enable is idempotent")
	}
	settings, _ := readUserSettings(path)
	if settings["model"] != "opus" {
		t.Fatal("other settings are kept")
	}
	hooks := settings["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop keeps theirs and adds one of ours (not two): %v", stop)
	}
	ours := marshalCompact(stop[1])
	if !strings.Contains(ours, `"/opt/homebrew/bin/corgi agent hook emit"`) || !strings.Contains(ours, `"async":true`) || !strings.Contains(ours, "agent hook tab") {
		t.Fatalf("Stop gets an async emit and a sync tab title: %s", ours)
	}
	pre := marshalCompact(hooks["PreToolUse"])
	if strings.Contains(pre, "agent hook tab") || strings.Contains(pre, "matcher") {
		t.Fatalf("PreToolUse is emit only, every tool: %s", pre)
	}
	start := marshalCompact(hooks["SessionStart"])
	if !strings.Contains(start, `"matcher":"startup|resume|clear|fork"`) {
		t.Fatalf("SessionStart excludes compact: %s", start)
	}
	if !hasTrackingHooks(path) {
		t.Fatal("hasTrackingHooks")
	}

	// --no-tab-title removes the title hook on a re-enable.
	if err := enableTrackingIn(path, "corgi", false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(marshalCompact(readJSONObject(path)["hooks"]), "agent hook tab") {
		t.Fatal("no-tab-title")
	}

	removed, err := disableTrackingIn(path)
	if err != nil || !removed {
		t.Fatalf("disable: %v %v", removed, err)
	}
	after := readJSONObject(path)
	hooks = after["hooks"].(map[string]any)
	if len(hooks) != 1 || len(hooks["Stop"].([]any)) != 1 || after["model"] != "opus" {
		t.Fatalf("only theirs remains: %v", after)
	}
	if removed, _ := disableTrackingIn(path); removed {
		t.Fatal("a second disable finds nothing")
	}
	if hasTrackingHooks(path) {
		t.Fatal("gone")
	}
	if removed, err := disableTrackingIn(filepath.Join(t.TempDir(), "none.json")); removed || err != nil {
		t.Fatal("a missing file has nothing to strip")
	}
}

func TestEnableTrackingRefusesABrokenSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	_ = os.WriteFile(path, []byte("{oops"), 0o600)
	if err := enableTrackingIn(path, "corgi", true); err == nil {
		t.Fatal("an unparseable account settings file must not be replaced")
	}
	if data, _ := os.ReadFile(path); string(data) != "{oops" {
		t.Fatal("left untouched")
	}
	empty := filepath.Join(t.TempDir(), "settings.json")
	_ = os.WriteFile(empty, []byte("  \n"), 0o600)
	if err := enableTrackingIn(empty, "corgi", true); err != nil {
		t.Fatalf("an empty file is an empty object: %v", err)
	}
}

func TestHookCommandQuotesAPathWithSpaces(t *testing.T) {
	if got := hookCommand("/Applications/My Tools/corgi", hookEmit); got != `"/Applications/My Tools/corgi" agent hook emit` {
		t.Fatalf("got %s", got)
	}
	if got := hookCommand("/usr/local/bin/corgi", hookTabTitle); got != "/usr/local/bin/corgi agent hook tab" {
		t.Fatalf("got %s", got)
	}
	if corgiCommandPath() == "" {
		t.Fatal("some path")
	}
}

func TestTrackConfigDirsCoversAccountProfilesAndExtras(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	agentD := t.TempDir()
	if err := addProfile(agentD, "work", config.WorkspaceConfig{ConfigDir: filepath.Join(home, ".claude-work")}); err != nil {
		t.Fatal(err)
	}
	if err := addProfile(agentD, "fast", config.WorkspaceConfig{Bin: "claude"}); err != nil {
		t.Fatal(err)
	}
	dirs := trackConfigDirs(agentD, []string{"~/.claude-work", filepath.Join(home, "other"), " "})
	want := []string{filepath.Join(home, ".claude"), filepath.Join(home, ".claude-work"), filepath.Join(home, "other")}
	if strings.Join(dirs, ",") != strings.Join(want, ",") {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "custom"))
	if got := trackConfigDirs(agentD, nil)[0]; got != filepath.Join(home, "custom") {
		t.Fatalf("CLAUDE_CONFIG_DIR wins: %s", got)
	}
}

func TestSetTrackSlots(t *testing.T) {
	dir := t.TempDir()
	if err := setTrackSlots(dir, 0); err == nil {
		t.Fatal("0 is not a board")
	}
	if err := setTrackSlots(dir, 15); err != nil {
		t.Fatal(err)
	}
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	if user.TrackSlots != 15 {
		t.Fatalf("slots = %d", user.TrackSlots)
	}
}

func TestWorkspaceLabelPrefersTheDeepestRegisteredRoot(t *testing.T) {
	reg := &workspace.Registry{}
	reg.Upsert(workspace.Workspace{ID: "dev", AbsPath: "/home/me/dev"})
	reg.Upsert(workspace.Workspace{ID: "acme-api", AbsPath: "/home/me/dev/acme-api"})
	if l, f := workspaceLabel(reg, "/home/me/dev/acme-api/src"); l != "acme-api" || f != "/home/me/dev/acme-api" {
		t.Fatalf("got %s %s", l, f)
	}
	if l, _ := workspaceLabel(reg, "/home/me/dev/acme-apiary"); l != "dev" {
		t.Fatalf("a sibling with the same prefix is not inside: %s", l)
	}
	if l, f := workspaceLabel(reg, "/tmp/scratch"); l != "scratch" || f != "" {
		t.Fatalf("unregistered: %s %s", l, f)
	}
	if l, _ := workspaceLabel(nil, "/tmp/x"); l != "x" {
		t.Fatal("nil registry")
	}
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	agentD, _ := agentDir()
	if err := workspace.Save(agentRegistryPath(agentD), reg); err != nil {
		t.Fatal(err)
	}
	resolve := workspaceResolver(agentD)
	if l, _ := resolve("/home/me/dev/acme-api"); l != "acme-api" {
		t.Fatalf("resolver: %s", l)
	}
	if l, _ := resolve("/home/me/dev/acme-api"); l != "acme-api" {
		t.Fatal("cached")
	}
}

func TestProfileResolverNamesTheProfileForAConfigDir(t *testing.T) {
	dir := t.TempDir()
	if err := addProfile(dir, "work", config.WorkspaceConfig{ConfigDir: "~/.claude-work"}); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	resolve := profileResolver(dir)
	if got := resolve(filepath.Join(home, ".claude-work")); got != "work" {
		t.Fatalf("got %q", got)
	}
	if got := resolve("/elsewhere"); got != "" {
		t.Fatalf("unknown dir: %q", got)
	}
	if got := resolve(""); got != "" {
		t.Fatal("empty")
	}
}
