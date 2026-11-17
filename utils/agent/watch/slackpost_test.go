package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/sendgate"
)

func TestSlackPosterPicksTheVoiceAndThreads(t *testing.T) {
	type call struct {
		auth string
		body map[string]any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		calls = append(calls, call{auth: r.Header.Get("Authorization"), body: body})
		switch r.URL.Path {
		case "/chat.postMessage":
			_, _ = w.Write([]byte(`{"ok":true,"channel":"C0RE","ts":"1726000900.000100"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	p := NewSlackPoster(Secrets{SlackUser: "xoxp-me", SlackBot: "xoxb-app"})
	p.User.URL, p.Bot.URL = srv.URL, srv.URL
	p.User.Client, p.Bot.Client = srv.Client(), srv.Client()
	p.Team = "acme"

	got, err := p.Post(context.Background(), SlackTarget{Channel: "C0RE", ThreadTS: "1726000100.000100"}, "on it")
	if err != nil {
		t.Fatal(err)
	}
	if got.As != "bot" || calls[0].auth != "Bearer xoxb-app" {
		t.Fatalf("with a bot token stored the default voice is the bot: %+v / %s", got, calls[0].auth)
	}
	if calls[0].body["thread_ts"] != "1726000100.000100" {
		t.Fatalf("a reply must land in the thread: %+v", calls[0].body)
	}
	if got.Permalink == "" {
		t.Fatal("the caller needs a link back to what it said")
	}

	if _, err := p.Post(context.Background(), SlackTarget{Channel: "C0RE", As: "me"}, "approved"); err != nil {
		t.Fatal(err)
	}
	if calls[1].auth != "Bearer xoxp-me" {
		t.Fatalf("--as me must use the user token: %s", calls[1].auth)
	}

	if err := p.React(context.Background(), SlackTarget{Channel: "C0RE", ThreadTS: "1726000100.000100"}, "white_check_mark"); err != nil {
		t.Fatal(err)
	}
	if calls[2].body["name"] != "white_check_mark" || calls[2].body["timestamp"] != "1726000100.000100" {
		t.Fatalf("reaction = %+v", calls[2].body)
	}
}

func TestSlackPosterRefusesWithoutAToken(t *testing.T) {
	p := NewSlackPoster(Secrets{})
	if _, err := p.Post(context.Background(), SlackTarget{Channel: "C1"}, "hi"); err == nil {
		t.Fatal("posting with no token must say so rather than fail at the wire")
	}
	only := NewSlackPoster(Secrets{SlackBot: "xoxb-app"})
	if _, _, err := only.voice("me"); err == nil {
		t.Fatal("asking for a voice with no token must fail, not quietly use the other one")
	}
}

func TestSlackPosterStopsAtTheGateAndBacksOffOn429(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p := NewSlackPoster(Secrets{SlackBot: "xoxb-app"})
	p.Bot.URL, p.Bot.Client = srv.URL, srv.Client()
	p.Gate = sendgate.InMemory()

	for i := 0; i < 1000; i++ {
		_, _ = p.Post(context.Background(), SlackTarget{Channel: "C1", ThreadTS: fmt.Sprint(i)}, fmt.Sprintf("Could not do this (%d)", i))
	}
	if hits != 1 {
		t.Fatalf("a 429 must pause every send until Slack is ready again; it was called %d times", hits)
	}

	quiet := NewSlackPoster(Secrets{SlackBot: "xoxb-app"})
	quiet.Bot.URL, quiet.Bot.Client = srv.URL, srv.Client()
	quiet.Gate = sendgate.InMemory()
	hits = 0
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"ok":false,"error":"internal_error"}`))
	})
	for i := 0; i < 1000; i++ {
		_, _ = quiet.Post(context.Background(), SlackTarget{Channel: fmt.Sprint("C", i%7)}, fmt.Sprint("retry ", i))
	}
	if hits != sendgate.Default.PerFamily {
		t.Fatalf("failed sends must spend the budget; %d reached Slack, want %d", hits, sendgate.Default.PerFamily)
	}
}

func TestARevokedUserTokenDoesNotSilenceTheBot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer xoxp-dead" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"token_revoked"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"channels":[{"id":"C0DE","name":"code-review"}]}`))
	}))
	defer srv.Close()
	p := NewSlackPoster(Secrets{SlackUser: "xoxp-dead", SlackBot: "xoxb-app"})
	p.User.URL, p.Bot.URL = srv.URL, srv.URL
	p.User.Client, p.Bot.Client = srv.Client(), srv.Client()

	for _, as := range []string{"bot", "", "me"} {
		got, err := p.Resolve(context.Background(), "#code-review", as)
		if err != nil || got != "C0DE" {
			t.Fatalf("as %q: the other token finds the channel: %q %v", as, got, err)
		}
	}
	dead := NewSlackPoster(Secrets{SlackUser: "xoxp-dead"})
	dead.User.URL, dead.User.Client = srv.URL, srv.Client()
	if got, err := dead.Resolve(context.Background(), "#code-review", "me"); err != nil || got != "#code-review" {
		t.Fatalf("no list at all: the name goes to chat.postMessage, which says what is wrong: %q %v", got, err)
	}
}

type slackCall struct {
	method string
	path   string
	auth   string
	body   map[string]any
}

func fakeSlack(t *testing.T, reply func(path string) string) (*httptest.Server, *[]slackCall) {
	t.Helper()
	var calls []slackCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		calls = append(calls, slackCall{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), body: body})
		_, _ = w.Write([]byte(reply(r.URL.Path)))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func posterAt(srv *httptest.Server, s Secrets) *SlackPoster {
	p := NewSlackPoster(s)
	for _, api := range []*slackAPI{p.User, p.Bot} {
		if api != nil {
			api.URL, api.Client = srv.URL, srv.Client()
		}
	}
	p.Gate = sendgate.InMemory()
	return p
}

func TestSlackPosterUpdateRewritesTheMessageInPlace(t *testing.T) {
	srv, calls := fakeSlack(t, func(string) string {
		return `{"ok":true,"channel":"C0RE","ts":"1726000400.000100","text":"new"}`
	})
	p := posterAt(srv, Secrets{SlackUser: "xoxp-me", SlackBot: "xoxb-app"})
	p.Team = "acme"
	msg := SlackMessage{Channel: "C0RE", TS: "1726000400.000100"}

	got, err := p.Update(context.Background(), msg, "", "[ABC-12] Phone field\napi: https://github.com/acme/api/pull/5")
	if err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if c.method != http.MethodPost || c.path != "/chat.update" {
		t.Fatalf("an edit is a POST to chat.update, never a new chat.postMessage: %s %s", c.method, c.path)
	}
	if c.auth != "Bearer xoxb-app" || got.As != "bot" {
		t.Fatalf("with a bot token stored the default voice is the bot, as for post: %s / %+v", c.auth, got)
	}
	want := map[string]any{"channel": "C0RE", "ts": "1726000400.000100", "text": "[ABC-12] Phone field\napi: https://github.com/acme/api/pull/5"}
	if len(c.body) != len(want) {
		t.Fatalf("chat.update gets the channel, the ts and the text, nothing else: %+v", c.body)
	}
	for k, v := range want {
		if c.body[k] != v {
			t.Fatalf("%s = %v, want %v (body %+v)", k, c.body[k], v, c.body)
		}
	}
	if got.Permalink != "https://acme.slack.com/archives/C0RE/p1726000400000100" || got.Channel != "C0RE" || got.TS != "1726000400.000100" {
		t.Fatalf("the caller gets the link back to what it changed: %+v", got)
	}

	msg.Team = "other"
	got, err = p.Update(context.Background(), msg, "me", "approved")
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[1].auth != "Bearer xoxp-me" || got.As != "me" {
		t.Fatalf("--as me edits with the user token: %s / %+v", (*calls)[1].auth, got)
	}
	if got.Permalink != "https://other.slack.com/archives/C0RE/p1726000400000100" {
		t.Fatalf("a link the person pasted names its own workspace: %s", got.Permalink)
	}

	if _, err := p.Update(context.Background(), SlackMessage{Channel: "C0RE"}, "", "x"); err == nil {
		t.Fatal("an edit without a message timestamp must not reach Slack")
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %d", len(*calls))
	}
	if _, err := NewSlackPoster(Secrets{}).Update(context.Background(), SlackMessage{Channel: "C0RE", TS: "1.2"}, "", "x"); !errors.Is(err, ErrNoSlackToken) {
		t.Fatalf("no token says so: %v", err)
	}
}

func TestSlackEditRefusalsSayWhatToDo(t *testing.T) {
	cases := []struct {
		code    string
		secrets Secrets
		as      string
		want    []string
	}{
		{"cant_update_message", Secrets{SlackUser: "xoxp-me", SlackBot: "xoxb-app"}, "", []string{"not posted as the bot", "try --as me"}},
		{"cant_update_message", Secrets{SlackUser: "xoxp-me", SlackBot: "xoxb-app"}, "me", []string{"not posted as you", "try --as bot"}},
		{"cant_update_message", Secrets{SlackBot: "xoxb-app"}, "", []string{"not posted as the bot", "only whoever posted it"}},
		{"message_not_found", Secrets{SlackUser: "xoxp-me", SlackBot: "xoxb-app"}, "bot", []string{"no message at 1726000400.000100 in C0RE", "check the link", "try --as me"}},
		{"edit_window_closed", Secrets{SlackBot: "xoxb-app"}, "", []string{"edit window closed", "post a new one"}},
		{"channel_not_found", Secrets{SlackUser: "xoxp-me", SlackBot: "xoxb-app"}, "", []string{"the bot cannot see channel C0RE", "try --as me"}},
		{"not_in_channel", Secrets{SlackUser: "xoxp-me", SlackBot: "xoxb-app"}, "", []string{"cannot see channel C0RE", "add the app"}},
		{"is_inactive", Secrets{SlackBot: "xoxb-app"}, "", []string{"archived"}},
		{"msg_too_long", Secrets{SlackBot: "xoxb-app"}, "", []string{"too long"}},
	}
	for _, c := range cases {
		srv, _ := fakeSlack(t, func(string) string { return `{"ok":false,"error":"` + c.code + `"}` })
		p := posterAt(srv, c.secrets)
		_, err := p.Update(context.Background(), SlackMessage{Channel: "C0RE", TS: "1726000400.000100"}, c.as, "new text")
		if err == nil {
			t.Fatalf("%s: must fail", c.code)
		}
		msg := err.Error()
		if strings.Contains(msg, "\n") {
			t.Fatalf("%s: one line: %q", c.code, msg)
		}
		for _, w := range append(c.want, c.code) {
			if !strings.Contains(msg, w) {
				t.Fatalf("%s as %q: %q missing from %q", c.code, c.as, w, msg)
			}
		}
	}

	srv, _ := fakeSlack(t, func(string) string { return `{"ok":false,"error":"missing_scope","needed":"chat:write"}` })
	_, err := posterAt(srv, Secrets{SlackBot: "xoxb-app"}).Update(context.Background(), SlackMessage{Channel: "C0RE", TS: "1726000400.000100"}, "", "x")
	if err == nil || err.Error() != "slack chat.update: missing_scope (needs chat:write)" {
		t.Fatalf("an error with nothing to add reads as Slack said it: %v", err)
	}
}

func TestSlackPosterHoldsBackTheSameEditTwice(t *testing.T) {
	srv, calls := fakeSlack(t, func(string) string { return `{"ok":true,"channel":"C0RE","ts":"1726000400.000100"}` })
	p := posterAt(srv, Secrets{SlackBot: "xoxb-app"})
	msg := SlackMessage{Channel: "C0RE", TS: "1726000400.000100"}
	if _, err := p.Update(context.Background(), msg, "", "same"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Update(context.Background(), msg, "", "same"); !errors.Is(err, sendgate.ErrThrottled) {
		t.Fatalf("an edit goes through the same gate as a post: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("calls = %d", len(*calls))
	}

	refused, _ := fakeSlack(t, func(string) string { return `{"ok":false,"error":"cant_update_message"}` })
	both := posterAt(refused, Secrets{SlackUser: "xoxp-me", SlackBot: "xoxb-app"})
	if _, err := both.Update(context.Background(), msg, "", "retry"); err == nil || !strings.Contains(err.Error(), "try --as me") {
		t.Fatalf("refused as the bot: %v", err)
	}
	if _, err := both.Update(context.Background(), msg, "me", "retry"); errors.Is(err, sendgate.ErrThrottled) {
		t.Fatalf("the retry the refusal suggests must not be held back as a duplicate: %v", err)
	}
}
