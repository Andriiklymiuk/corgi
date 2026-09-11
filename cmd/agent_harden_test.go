package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// harden adds what is missing and leaves what is there: deny rules for
// secrets and destruction, and the hook that refuses to write a key.
func TestHardenIsAdditiveAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude", "settings.local.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"permissions":{"deny":["Bash(sudo *)"],"allow":["Bash(go test *)"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	added, err := hardenSettings(path, "/usr/local/bin/corgi", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != len(hardenDeny) { // one deny was there already, the hook is new
		t.Fatalf("added %d: %v", len(added), added)
	}
	settings, _ := readUserSettings(path)
	perms := settings["permissions"].(map[string]any)
	if allow, _ := perms["allow"].([]any); len(allow) != 1 {
		t.Fatal("the allow list is untouched")
	}
	if deny, _ := perms["deny"].([]any); len(deny) != len(hardenDeny) {
		t.Fatalf("deny has %d rules", len(deny))
	}
	if !strings.Contains(marshalCompact(settings["hooks"]), "agent hook secrets") || !strings.Contains(marshalCompact(settings["hooks"]), "echo hi") {
		t.Fatal("the hook is added and the existing one kept")
	}
	again, err := hardenSettings(path, "/usr/local/bin/corgi", false)
	if err != nil || len(again) != 0 {
		t.Fatalf("second run adds nothing: %v %v", again, err)
	}
}

// The secrets hook refuses a real key and lets placeholders and env reads
// through.
func TestTheSecretsHookRefusesARealKey(t *testing.T) {
	run := func(content string) string {
		in, _ := json.Marshal(map[string]any{"tool_input": map[string]any{"file_path": "config.ts", "content": content}})
		var out bytes.Buffer
		runSecretsHook(bytes.NewReader(in), &out)
		return out.String()
	}
	if got := run("const key = process.env.API_KEY\nAPI_KEY=your-key-here\ntoken: ${TOKEN}\n"); got != "" {
		t.Fatalf("placeholders pass: %s", got)
	}
	if got := run("export const key = 'sk-ant-api03-abcdefghijklmnopqrstuvwxyz0123456789'\n"); !strings.Contains(got, "deny") {
		t.Fatalf("a real key is refused: %s", got)
	}
	if got := run("password = \"Tr0ub4dor&3-really-long-password\"\n"); !strings.Contains(got, "deny") {
		t.Fatalf("a password literal is refused: %s", got)
	}
}
