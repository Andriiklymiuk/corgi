package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestIsIdleNudge(t *testing.T) {
	if !isIdleNudge("Claude is waiting for your input") {
		t.Error("the idle nudge was not recognised")
	}
	if !isIdleNudge("CLAUDE IS WAITING FOR YOUR INPUT") {
		t.Error("matching must not depend on case")
	}
	if isIdleNudge("Claude needs your permission to use Bash") {
		t.Error("a permission prompt was mistaken for the idle nudge")
	}
	if isIdleNudge("a session finished its turn") {
		t.Error("a finished turn was mistaken for the idle nudge")
	}
}

// The default hook drops the idle nudge; --idle keeps it, and the choice is
// visible in the command written into the settings file.
func TestWithCorgiHookRecordsTheIdleChoice(t *testing.T) {
	plain := marshalCompact(withCorgiHook(nil, "acme", hookEventNotification, false))
	if strings.Contains(plain, "--idle") {
		t.Errorf("the default hook asked for idle notifications: %s", plain)
	}
	withIdle := marshalCompact(withCorgiHook(nil, "acme", hookEventNotification, true))
	if !strings.Contains(withIdle, "--idle") {
		t.Errorf("--idle was not written into the hook: %s", withIdle)
	}
	// Either way it stays removable.
	if !strings.Contains(withIdle, hookMarker) {
		t.Errorf("the hook lost its marker: %s", withIdle)
	}
}

func TestEnableHooksInWritesTheIdleFlag(t *testing.T) {
	dir := t.TempDir()
	if err := enableHooksIn(dir, "acme", false, true, false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "--idle") {
		t.Fatalf("settings do not carry the idle choice:\n%s", body)
	}
}

func TestWantsIdleHook(t *testing.T) {
	if wantsIdleHook(nil) {
		t.Error("no command means no flag")
	}
	cmd := &cobra.Command{}
	cmd.Flags().Bool("idle", false, "")
	if wantsIdleHook(cmd) {
		t.Error("unset flag read as set")
	}
	if err := cmd.Flags().Set("idle", "true"); err != nil {
		t.Fatal(err)
	}
	if !wantsIdleHook(cmd) {
		t.Error("set flag read as unset")
	}
}

// The whole point: an idle nudge with no daemon told, so no toast and no push.
func TestRunAgentHookDropsTheIdleNudge(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.Flags().String("workspace", "", "")
	cmd.Flags().Bool("idle", false, "")
	if err := cmd.Flags().Set("workspace", "acme"); err != nil {
		t.Fatal(err)
	}

	stdin, err := os.CreateTemp(t.TempDir(), "hook")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.WriteString(`{"message":"Claude is waiting for your input","hook_event_name":"Notification"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	original := os.Stdin
	os.Stdin = stdin
	defer func() { os.Stdin = original }()

	runAgentHook(cmd, []string{"Notification"})

	// No command file written means nothing reached the daemon.
	entries, _ := os.ReadDir(filepath.Join(dir, "commands"))
	if len(entries) != 0 {
		t.Fatalf("the idle nudge was forwarded anyway: %d command(s)", len(entries))
	}
}

func TestHookDetailFallbacks(t *testing.T) {
	if got := hookDetail(hookEventStop, nil); got != "a session finished its turn" {
		t.Errorf("stop detail = %q", got)
	}
	if got := hookDetail("", nil); got != "a session is waiting for you" {
		t.Errorf("default detail = %q", got)
	}
}

func TestEnableWritesTheTitleHookAndDisableTakesItBack(t *testing.T) {
	dir := t.TempDir()
	if err := enableHooksIn(dir, "acme", false, false, true); err != nil {
		t.Fatal(err)
	}
	settings := readJSONObject(claudeLocalSettingsPath(dir))
	hooks, _ := settings["hooks"].(map[string]any)
	written := marshalCompact(hooks[hookEventPrompt])
	if !strings.Contains(written, "corgi agent hook --workspace acme title") {
		t.Errorf("UserPromptSubmit hook = %s, want the title hook for this workspace", written)
	}
	// --idle belongs to the notification hook; the title hook has no use for it.
	if strings.Contains(written, "--idle") {
		t.Errorf("the title hook must not carry notification flags: %s", written)
	}

	// Off by request, without touching the notification hook.
	if err := enableHooksIn(dir, "acme", false, false, false); err != nil {
		t.Fatal(err)
	}
	settings = readJSONObject(claudeLocalSettingsPath(dir))
	hooks, _ = settings["hooks"].(map[string]any)
	if _, still := hooks[hookEventPrompt]; still {
		t.Error("--no-title must remove the hook corgi wrote")
	}
	if _, gone := hooks[hookEventNotification]; !gone {
		t.Error("the notification hook must be left in place")
	}

	if _, err := disableHooksIn(dir); err != nil {
		t.Fatal(err)
	}
	settings = readJSONObject(claudeLocalSettingsPath(dir))
	if _, left := settings["hooks"]; left {
		t.Errorf("disable must remove every hook corgi wrote: %v", settings)
	}
}
