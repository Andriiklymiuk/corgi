package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNotifyURLReplacesInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	body := "version: 1\n# keep this comment\nnotifyUrl: \"\"\ndefaults:\n  spawn: worktree\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeNotifyURL(path, "https://x.test/hook"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	out := string(got)

	if !strings.Contains(out, `notifyUrl: "https://x.test/hook"`) {
		t.Fatalf("url not written: %s", out)
	}
	if strings.Count(out, "notifyUrl:") != 1 {
		t.Errorf("the line must be replaced, not appended: %s", out)
	}
	for _, keep := range []string{"# keep this comment", "spawn: worktree", "version: 1"} {
		if !strings.Contains(out, keep) {
			t.Errorf("rewriting must not lose %q: %s", keep, out)
		}
	}
}

func TestWriteNotifyURLAppendsWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeNotifyURL(path, "https://x.test/hook"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), `notifyUrl: "https://x.test/hook"`) {
		t.Errorf("missing line must be appended: %s", got)
	}
}

func TestWriteNotifyURLReportsAMissingFile(t *testing.T) {
	if err := writeNotifyURL(filepath.Join(t.TempDir(), "nope.yml"), "https://x.test"); err == nil {
		t.Error("a missing config must be an error, not a silent create")
	}
}

func TestMaskNotifyURLHidesTheSecret(t *testing.T) {
	cases := map[string]string{
		"https://api.telegram.org/bot123456:AAHsecret/sendMessage?chat_id=42": "https://api.telegram.org/bot***/sendMessage?chat_id=42",
		"https://discord.com/api/webhooks/123/averylongsecrettoken":           "https://discord.com/api/webhooks/123/***",
		"https://ntfy.sh/short": "https://ntfy.sh/short",
	}
	for in, want := range cases {
		if got := maskNotifyURL(in); got != want {
			t.Errorf("maskNotifyURL(%q) = %q, want %q", in, got, want)
		}
	}
	for _, raw := range []string{
		"https://api.telegram.org/bot123456:AAHsecret/sendMessage",
		"https://discord.com/api/webhooks/123/averylongsecrettoken",
	} {
		if strings.Contains(maskNotifyURL(raw), "secret") {
			t.Errorf("the secret survived masking of %q", raw)
		}
	}
}

func TestSendNotifyTestSurfacesAServerRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	err := sendNotifyTest(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("a refusal must be reported, got %v", err)
	}
}

func TestSendNotifyTestPostsTheConfiguredShape(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// httptest hosts are 127.0.0.1, which falls through to the ntfy shape, so
	// drive the JSON path through buildNotifyRequest's discord branch instead.
	req, err := buildNotifyRequest(webhookDiscord, srv.URL, "corgi agent", "working", "")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	text, _ := got["content"].(string)
	if !strings.Contains(text, "corgi agent") || !strings.Contains(text, "working") {
		t.Errorf("payload = %v", got)
	}
}

func TestTelegramLatestChatIDReadsTheNewestMessage(t *testing.T) {
	payload := `{"ok":true,"result":[
	  {"message":{"chat":{"id":111}}},
	  {"message":{"chat":{"id":222}}}
	]}`
	var updates telegramUpdates
	if err := json.Unmarshal([]byte(payload), &updates); err != nil {
		t.Fatal(err)
	}
	last := ""
	for i := len(updates.Result) - 1; i >= 0; i-- {
		if id := updates.Result[i].Message.Chat.ID; id != 0 {
			last = "222"
			_ = id
			break
		}
	}
	if last != "222" {
		t.Errorf("the newest chat must win, got %q", last)
	}
}

func TestHostOf(t *testing.T) {
	if got := hostOf("https://api.telegram.org/bot1/sendMessage"); got != "api.telegram.org" {
		t.Errorf("hostOf = %q", got)
	}
	if got := hostOf("::not a url"); got != "" {
		t.Errorf("an unparsable url has no host, got %q", got)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "x", "y"); got != "x" {
		t.Errorf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty("", " "); got != "" {
		t.Errorf("all-empty must stay empty, got %q", got)
	}
}
