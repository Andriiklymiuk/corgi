package cmd

import (
	"strings"
	"testing"
)

func TestTelegramControlFromParsesTokenAndChat(t *testing.T) {
	c := telegramControlFrom("https://api.telegram.org/bot12345:AAsecret/sendMessage?chat_id=99", "/agent")
	if c == nil {
		t.Fatal("a telegram notifyUrl must produce a controller")
	}
	if c.token != "12345:AAsecret" {
		t.Errorf("token = %q", c.token)
	}
	if c.chatID != "99" {
		t.Errorf("chatID = %q", c.chatID)
	}
}

func TestTelegramControlFromIgnoresOtherDestinations(t *testing.T) {
	for _, raw := range []string{
		"",
		"https://ntfy.sh/topic",
		"https://discord.com/api/webhooks/1/x",
		"https://hooks.slack.com/services/x",
		"https://api.telegram.org/bot123/sendMessage",
		"https://api.telegram.org/sendMessage?chat_id=1",
		"::broken",
	} {
		if got := telegramControlFrom(raw, "/agent"); got != nil {
			t.Errorf("%q must not start a telegram controller, got %+v", raw, got)
		}
	}
}

func TestTelegramHelpNamesEveryCommand(t *testing.T) {
	for _, verb := range []string{"/status", "/start", "/stop", "/help"} {
		if !strings.Contains(telegramHelp, verb) {
			t.Errorf("help does not mention %s", verb)
		}
	}
}
