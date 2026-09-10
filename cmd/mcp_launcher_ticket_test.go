package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/watch"
)

// ticketHome writes an events log and a board cache under a temp agent dir.
func ticketHome(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir, err := agentDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "watch"), 0o700); err != nil {
		t.Fatal(err)
	}
	events := []watch.Event{
		{Key: "jira:ABC-1", Kind: watch.KindIssueNew, Ref: "ABC-1", Title: "Login loops", Workspace: "api", At: time.Now()},
		{Key: "jira:ORPHAN-1", Kind: watch.KindIssueNew, Ref: "ORPHAN-1", Title: "No workspace", At: time.Now()},
	}
	var lines []string
	for _, e := range events {
		row, _ := json.Marshal(e)
		lines = append(lines, string(row))
	}
	if err := os.WriteFile(filepath.Join(dir, "watch", "events.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := watch.LoadBoardCache(dir).Set("api", watch.BoardInfo{
		Tracker: "jira", Project: "ABC", FetchedAt: time.Now(),
		Statuses: []watch.Status{{ID: "1", Name: "In Progress"}, {ID: "2", Name: "TO TEST STAGING"}},
		Me:       watch.Identity{ID: "u1", Name: "Andrii"},
	}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The phone draws a "move to" menu from these, so they must arrive with the
// events rather than costing a round trip to the tracker per tap.
func TestInboxCarriesTheCachedColumns(t *testing.T) {
	ticketHome(t)
	rec := httptest.NewRecorder()
	launchEventsHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/events", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("events = %d", rec.Code)
	}
	var got struct {
		Events []struct {
			Key       string `json:"key"`
			Workspace string `json:"workspace"`
		} `json:"events"`
		Boards map[string]struct {
			Columns []string `json:"columns"`
			Me      string   `json:"me"`
		} `json:"boards"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	api, ok := got.Boards["api"]
	if !ok {
		t.Fatalf("the watched workspace's columns must travel with its events: %+v", got.Boards)
	}
	if len(api.Columns) != 2 || api.Columns[1] != "TO TEST STAGING" {
		t.Fatalf("the real column names, in order: %+v", api.Columns)
	}
	if api.Me != "Andrii" {
		t.Fatalf("assign-to-me needs a name to show: %q", api.Me)
	}
	if _, ok := got.Boards[""]; ok {
		t.Fatal("an event with no workspace has no board")
	}
}

func TestIgnoreTakesARowOutOfTheInboxWithoutTouchingTheTracker(t *testing.T) {
	dir := ticketHome(t)

	rec := post(launchTicketHandler, "/launch/ticket", `{"key":"jira:ABC-1","do":"ignore"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ignore = %d: %s", rec.Code, rec.Body.String())
	}
	if !watch.LoadState(dir).IsSeen("jira:ABC-1") {
		t.Fatal("ignoring must mark it seen, so the unattended mode leaves it alone too")
	}
}

func TestTicketRefusesWhatItCannotDo(t *testing.T) {
	ticketHome(t)
	cases := []struct {
		body string
		want int
		why  string
	}{
		{`{"key":"nope","do":"ignore"}`, http.StatusNotFound, "an event nobody has seen"},
		{`{"key":"jira:ABC-1","do":"burn"}`, http.StatusBadRequest, "a verb that is not one of ours"},
		{`{"key":"jira:ORPHAN-1","do":"move","status":"Done"}`, http.StatusBadRequest, "an event with no workspace has no tracker"},
		{``, http.StatusBadRequest, "no body at all"},
	}
	for _, c := range cases {
		if rec := post(launchTicketHandler, "/launch/ticket", c.body); rec.Code != c.want {
			t.Errorf("%s: got %d want %d (%s)", c.why, rec.Code, c.want, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	launchTicketHandler(rec, httptest.NewRequest(http.MethodGet, "/launch/ticket", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET = %d", rec.Code)
	}
}
