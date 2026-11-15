package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookFindsTheSessionsOwnLink(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "sessions", "1.json"), []byte(`{"sessionId":"aaa","cwd":"/x","bridgeSessionId":"session_01ABC"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "sessions", "2.json"), []byte(`{"sessionId":"bbb","cwd":"/x"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "sessions", "3.json"), []byte(`not json`), 0o600)
	if got := claudeSessionLink("aaa", dir); got != "https://claude.ai/code/session_01ABC" {
		t.Fatalf("link: %q", got)
	}
	for _, id := range []string{"bbb", "zzz", ""} {
		if got := claudeSessionLink(id, dir); got != "" {
			t.Fatalf("%q: %q", id, got)
		}
	}
	if claudeSessionLink("aaa", filepath.Join(dir, "nope")) != "" {
		t.Fatal("missing dir")
	}
}

func TestHookPayloadKeepsTheSessionID(t *testing.T) {
	detail, kind, id := hookPayloadOf("", strings.NewReader(`{"message":"Claude needs your permission","notification_type":"permission_prompt","session_id":"aaa"}`))
	if detail != "Claude needs your permission" || kind != "permission_prompt" || id != "aaa" {
		t.Fatalf("%q %q %q", detail, kind, id)
	}
	if d, _ := hookDetailAndKind("", strings.NewReader(`{}`)); d != "a session is waiting for you" {
		t.Fatal(d)
	}
}
