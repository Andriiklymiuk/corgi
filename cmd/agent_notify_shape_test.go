package cmd

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestWebhookShapeFor(t *testing.T) {
	cases := map[string]string{
		"ntfy.sh":                    webhookNtfy,
		"my-ntfy.example.com":        webhookNtfy,
		"discord.com":                webhookDiscord,
		"ptb.discord.com":            webhookDiscord,
		"discordapp.com":             webhookDiscord,
		"hooks.slack.com":            webhookSlack,
		"api.telegram.org":           webhookTelegram,
		"notdiscord.com.example.org": webhookNtfy,
	}
	for host, want := range cases {
		if got := webhookShapeFor(host); got != want {
			t.Errorf("webhookShapeFor(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestBuildNotifyRequestNtfyUsesHeaders(t *testing.T) {
	req, err := buildNotifyRequest(webhookNtfy, "https://ntfy.sh/topic", "corgi agent · idid", "needs you", "https://x.test/app")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Title"); got != "corgi agent - idid" {
		t.Errorf("Title = %q, non-ASCII must be folded", got)
	}
	if got := req.Header.Get("Click"); got != "https://x.test/app" {
		t.Errorf("Click = %q", got)
	}
	body, _ := io.ReadAll(req.Body)
	if string(body) != "needs you" {
		t.Errorf("body = %q", body)
	}
}

func decodeBody(t *testing.T, shape, target string) map[string]any {
	t.Helper()
	req, err := buildNotifyRequest(shape, target, "corgi agent · idid", "needs you", "https://x.test/app")
	if err != nil {
		t.Fatal(err)
	}
	if ct := req.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var out map[string]any
	if err := json.NewDecoder(req.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBuildNotifyRequestJSONShapes(t *testing.T) {
	discord := decodeBody(t, webhookDiscord, "https://discord.com/api/webhooks/1/x")
	if text, _ := discord["content"].(string); text == "" {
		t.Errorf("discord payload = %v, want a content field", discord)
	}

	slack := decodeBody(t, webhookSlack, "https://hooks.slack.com/services/x")
	if text, _ := slack["text"].(string); text == "" {
		t.Errorf("slack payload = %v, want a text field", slack)
	}

	tg := decodeBody(t, webhookTelegram, "https://api.telegram.org/bot123/sendMessage?chat_id=987")
	if tg["chat_id"] != "987" {
		t.Errorf("telegram must carry chat_id from the url, got %v", tg["chat_id"])
	}
	if text, _ := tg["text"].(string); text == "" {
		t.Errorf("telegram payload = %v, want a text field", tg)
	}
}

func TestBuildNotifyRequestJSONCarriesTitleAndLink(t *testing.T) {
	body := decodeBody(t, webhookDiscord, "https://discord.com/api/webhooks/1/x")
	text, _ := body["content"].(string)
	for _, want := range []string{"corgi agent", "needs you", "https://x.test/app"} {
		if !strings.Contains(text, want) {
			t.Errorf("payload text %q is missing %q", text, want)
		}
	}
}
