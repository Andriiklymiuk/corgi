package watch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLinearMovesListsColumnChangesSince(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		query = string(raw)
		_, _ = w.Write([]byte(`{"data":{"issues":{"nodes":[
			{"identifier":"ABC-1","title":"Phone field","url":"https://linear.app/x/issue/ABC-1","history":{"nodes":[
				{"createdAt":"2026-10-05T09:00:00Z","actor":{"name":"Ana"},"fromState":{"name":"Backlog"},"toState":{"name":"Todo"}},
				{"createdAt":"2026-10-06T09:00:00Z","actor":{"name":"Ana"},"fromState":{"name":"Todo"},"toState":{"name":"In Progress"}},
				{"createdAt":"2026-10-06T10:00:00Z","actor":{"name":"Ana"}}]}},
			{"identifier":"ABC-2","title":"Logo","url":"u","history":{"nodes":[
				{"createdAt":"2026-10-06T11:00:00Z","fromState":{"name":"Review"},"toState":{"name":"Done"}}]}}]}}}`))
	}))
	defer srv.Close()
	l := &Linear{Token: "lin", Team: "ABC", URL: srv.URL, Client: srv.Client()}

	moves, err := l.Moves(context.Background(), time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, `team: {key: {eq: \"ABC\"}}`) {
		t.Fatalf("the workspace's team only: %s", query)
	}
	if len(moves) != 2 || moves[0].Ref != "ABC-2" || moves[0].To != "Done" || moves[0].By != "" {
		t.Fatalf("newest first; an automation has no name: %+v", moves)
	}
	if m := moves[1]; m.From != "Todo" || m.To != "In Progress" || m.By != "Ana" {
		t.Fatalf("an older change and a change with no column are not moves: %+v", moves)
	}
}

func TestJiraMovesReadsStatusItemsOfTheChangelog(t *testing.T) {
	var jql string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/myself" {
			_, _ = w.Write([]byte(`{"accountId":"me","timeZone":"Europe/Paris"}`))
			return
		}
		jql = r.URL.Query().Get("jql")
		_, _ = w.Write([]byte(`{"issues":[{"key":"OPS-7","fields":{"summary":"Cap the retries"},"changelog":{"histories":[
			{"created":"2026-10-06T14:09:00.000+0200","author":{"displayName":"Kim"},"items":[{"field":"assignee","toString":"Ana"},{"field":"status","fromString":"IN BACKLOG","toString":"CANCELLED"}]},
			{"created":"2026-10-01T10:00:00.000+0200","author":{"displayName":"Kim"},"items":[{"field":"status","fromString":"NEW","toString":"IN BACKLOG"}]}]}}]}`))
	}))
	defer srv.Close()
	j := &Jira{URL: srv.URL, Email: "me@acme.io", Token: "t", Project: "OPS", Client: srv.Client()}

	moves, err := j.Moves(context.Background(), time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(jql, `project = "OPS" AND updated >= "2026-10-05 14:00"`) {
		t.Fatalf("the window goes in the user's own zone: %s", jql)
	}
	if len(moves) != 1 || moves[0].From != "IN BACKLOG" || moves[0].To != "CANCELLED" || moves[0].By != "Kim" || moves[0].URL != srv.URL+"/browse/OPS-7" {
		t.Fatalf("only status items, only since: %+v", moves)
	}
}

func TestMovesSayWhenTheDayWasCut(t *testing.T) {
	if !errors.Is(cutWhen(movesLimit), ErrMovesCut) || cutWhen(movesLimit-1) != nil {
		t.Fatal("a full page means there may be more")
	}
}
