package watch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
