package watch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSlackCallReportsNotOKAndRateLimits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			if r.Header.Get("Authorization") != "Bearer xoxp-t" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"U1","team":"acme"}`))
		case "/conversations.history":
			_, _ = w.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
		case "/search.messages":
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()

	api := &slackAPI{Token: "xoxp-t", URL: srv.URL, Client: srv.Client()}
	var who struct {
		UserID string `json:"user_id"`
		Team   string `json:"team"`
	}
	if err := api.call(context.Background(), "auth.test", nil, &who); err != nil {
		t.Fatal(err)
	}
	if who.UserID != "U1" || who.Team != "acme" {
		t.Fatalf("auth.test = %+v", who)
	}

	err := api.call(context.Background(), "conversations.history", url.Values{"channel": {"C1"}}, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "missing_scope") {
		t.Fatalf("a not-ok body must name the error slack gave: %v", err)
	}

	err = api.call(context.Background(), "search.messages", nil, &struct{}{})
	if !errors.Is(err, errSlackRateLimited) {
		t.Fatalf("429 must be recognisable so the round can back off: %v", err)
	}
}

func TestSlackPermalinkAndTimestamp(t *testing.T) {
	got := slackPermalink("acme", "C0123", "1726000000.000100", "")
	want := "https://acme.slack.com/archives/C0123/p1726000000000100"
	if got != want {
		t.Fatalf("permalink = %q, want %q", got, want)
	}
	if inThread := slackPermalink("acme", "C0123", "1726000000.000100", "1725999999.000100"); !strings.Contains(inThread, "?thread_ts=1725999999.000100") {
		t.Fatalf("a message in a thread links into the thread: %q", inThread)
	}
	if at := slackTSTime("1726000000.000100"); at.Unix() != 1726000000 {
		t.Fatalf("ts time = %v", at)
	}
	if !slackTSTime("nonsense").IsZero() {
		t.Fatal("an unparseable ts is the zero time, not 1970")
	}
	if !slackTSNewer("1726000000.000100", "1725999999.999999") {
		t.Fatal("timestamps compare as numbers")
	}
}
