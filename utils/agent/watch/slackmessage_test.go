package watch

import (
	"strings"
	"testing"
)

func TestParseSlackMessageTakesALinkOrAnEventKey(t *testing.T) {
	cases := []struct {
		ref  string
		want SlackMessage
	}{
		{"slack:C0RE:1726000400.000100", SlackMessage{Channel: "C0RE", TS: "1726000400.000100"}},
		{"https://acme.slack.com/archives/C0RE/p1726000400000100", SlackMessage{Channel: "C0RE", TS: "1726000400.000100", Team: "acme"}},
		{"https://acme.slack.com/archives/C0RE/p1726000400000100?thread_ts=1726000100.000100&cid=C0RE", SlackMessage{Channel: "C0RE", TS: "1726000400.000100", Team: "acme"}},
		{"  <https://acme.slack.com/archives/C0RE/p1726000400000100/>  ", SlackMessage{Channel: "C0RE", TS: "1726000400.000100", Team: "acme"}},
		{"https://Acme.Enterprise.Slack.com/archives/G0PRIV/p1726000400000100", SlackMessage{Channel: "G0PRIV", TS: "1726000400.000100", Team: "acme.enterprise"}},
		{"https://slack.com/archives/D0DM/p1726000400000100", SlackMessage{Channel: "D0DM", TS: "1726000400.000100"}},
	}
	for _, c := range cases {
		got, err := ParseSlackMessage(c.ref)
		if err != nil {
			t.Fatalf("%q: %v", c.ref, err)
		}
		if got != c.want {
			t.Fatalf("%q: got %+v, want %+v", c.ref, got, c.want)
		}
	}
}

func TestParseSlackMessageRefusesWhatNamesNoMessage(t *testing.T) {
	for _, ref := range []string{
		"",
		"slack:C0RE",
		"slack::1726000400.000100",
		"slack:C0RE:1726000400",
		"slack:C0RE:latest",
		"slack:C0RE:1726000400.000100:extra",
		"github:acme/api#1",
		"C0RE/p1726000400000100",
		"https://acme.slack.com/archives/C0RE",
		"https://acme.slack.com/archives/C0RE/p172600040000010",
		"https://acme.slack.com/archives/C0RE/p17260004000001000",
		"https://acme.slack.com/archives/C0RE/1726000400000100",
		"https://acme.slack.com/archives/C0RE/p1726000400000100/extra",
		"https://acme.slack.com/client/T0TEAM/C0RE",
		"https://example.com/archives/C0RE/p1726000400000100",
		"https://slack.com.example.com/archives/C0RE/p1726000400000100",
		"https://notslack.com/archives/C0RE/p1726000400000100",
		"ftp://acme.slack.com/archives/C0RE/p1726000400000100",
	} {
		_, err := ParseSlackMessage(ref)
		if err == nil {
			t.Fatalf("%q must be refused", ref)
		}
		if strings.Contains(err.Error(), "\n") {
			t.Fatalf("%q: the refusal is one line: %q", ref, err)
		}
		if !strings.Contains(err.Error(), "slack") && !strings.Contains(err.Error(), "Slack") {
			t.Fatalf("%q: the refusal should say what a message looks like: %v", ref, err)
		}
	}
}
