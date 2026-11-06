package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sentryServer(t *testing.T, status int, body string) (*Sentry, *string) {
	t.Helper()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.String() + " " + r.Header.Get("Authorization")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return NewSentry(Secrets{Sentry: "tok", SentryURL: srv.URL}, "acme", []string{"api"}, ""), &seen
}

func TestSentryBringsNewErrorsOnceAndSkipsTheQuietOnes(t *testing.T) {
	first := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	s, seen := sentryServer(t, 200, `[
	  {"id":"1","shortId":"API-3F","title":"TypeError: x","culprit":"cart.total","level":"error","count":"12","userCount":3,"permalink":"https://sentry.io/i/1","firstSeen":"`+first+`","project":{"slug":"api"}},
	  {"id":"2","shortId":"API-3G","title":"just a warning","level":"warning","firstSeen":"`+first+`"}
	]`)
	events, next, err := s.Poll(context.Background(), Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Key != "sentry:1" || events[0].Kind != KindErrorNew || events[0].Ref != "API-3F" {
		t.Fatalf("events: %+v", events)
	}
	if !strings.Contains(events[0].Body, "3 users") || !strings.Contains(*seen, "Bearer tok") || !strings.Contains(*seen, "project%3A%5Bapi%5D") {
		t.Fatalf("body %q, request %q", events[0].Body, *seen)
	}
	again, _, err := s.Poll(context.Background(), next)
	if err != nil || len(again) != 0 {
		t.Fatalf("second poll brought %+v, %v", again, err)
	}
}

func TestSentrySaysWhatToDoWhenTheTokenIsRefused(t *testing.T) {
	s, _ := sentryServer(t, 401, `{}`)
	if _, _, err := s.Poll(context.Background(), Cursor{"firstSeen": "keep"}); err == nil || !strings.Contains(err.Error(), "watch auth sentry") {
		t.Fatalf("err: %v", err)
	}
}

func TestAnAlertWebhookBecomesOnePickableErrorADay(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/hooks/coralogix?workspace=api&token=s", nil)
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	events, err := parseAlertHook("coralogix", r, []byte(`{"alert_name":"5xx spike","alert_id":"a-1","description":"500s on /pay","application":"api","severity":"Critical","alert_url":"https://coralogix.com/a/1"}`), now)
	if err != nil || len(events) != 1 {
		t.Fatalf("%+v %v", events, err)
	}
	e := events[0]
	if e.Workspace != "api" || e.Key != "coralogix:a-1:2026-11-06" || e.Title != "5xx spike" || e.State != "critical" || !strings.Contains(e.Body, "500s on /pay") {
		t.Fatalf("event: %+v", e)
	}
	if _, err := parseAlertHook("coralogix", r, []byte(`{"severity":"high"}`), now); err == nil {
		t.Fatal("an alert with no name went through")
	}
	bad, _ := parseAlertHook("coralogix", r, []byte(`{"title":"x","url":"javascript:alert(1)"}`), now)
	if bad[0].URL != "" {
		t.Fatalf("a non-https link was kept: %q", bad[0].URL)
	}
}

func TestErrorsAreWatchedOnlyWhenAsked(t *testing.T) {
	e := Event{Kind: KindErrorNew, Mine: true}
	if (Rules{Enabled: true}).Match(e) {
		t.Fatal("errors came in without --errors")
	}
	if !(Rules{Enabled: true, Errors: true}).Match(e) {
		t.Fatal("errors did not come in with --errors")
	}
}
