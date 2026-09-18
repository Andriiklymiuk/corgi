package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type slackFake struct {
	srv      *httptest.Server
	search   string
	history  map[string]string
	replies  string
	convList string
}

func newSlackFake(t *testing.T) *slackFake {
	t.Helper()
	f := &slackFake{
		history: map[string]string{},
		convList: `{"ok":true,"channels":[
			{"id":"C0RE","name":"code-review","is_im":false},
			{"id":"C0IN","name":"incidents","is_im":false},
			{"id":"D0VI","is_im":true,"user":"UVI"}
		],"response_metadata":{"next_cursor":""}}`,
		search:  `{"ok":true,"messages":{"matches":[],"paging":{"page":1,"pages":1}}}`,
		replies: `{"ok":true,"messages":[]}`,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"UME","team":"acme"}`))
		case "/conversations.list":
			_, _ = w.Write([]byte(f.convList))
		case "/search.messages":
			_, _ = w.Write([]byte(f.search))
		case "/conversations.history":
			body := f.history[r.URL.Query().Get("channel")]
			if body == "" {
				body = `{"ok":true,"messages":[],"has_more":false}`
			}
			_, _ = w.Write([]byte(body))
		case "/conversations.replies":
			_, _ = w.Write([]byte(f.replies))
		case "/users.info":
			id := r.URL.Query().Get("user")
			_, _ = w.Write([]byte(`{"ok":true,"user":{"id":"` + id + `","name":"` + strings.ToLower(id) + `","real_name":"Real ` + id + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func newTestSlack(f *slackFake, cfg SlackWatchConfig) *Slack {
	s := NewSlack(Secrets{SlackUser: "xoxp-t"}, cfg)
	s.api.URL = f.srv.URL
	s.api.Client = f.srv.Client()
	return s
}

func TestSlackFirstRoundOnlyBookmarks(t *testing.T) {
	f := newSlackFake(t)
	f.history["C0IN"] = `{"ok":true,"messages":[
		{"type":"message","user":"ULE","text":"deploying","ts":"1726000000.000100"}],"has_more":false}`
	s := newTestSlack(f, SlackWatchConfig{Channels: []string{"#incidents"}})

	events, cursor, err := s.Poll(context.Background(), Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("the first round sets the bookmark and rings nothing: %+v", events)
	}
	if cursor["me"] != "UME" || cursor["team"] != "acme" {
		t.Fatalf("the cursor must remember who I am: %+v", cursor)
	}
	if cursor["hist:C0IN"] != "1726000000.000100" {
		t.Fatalf("the channel bookmark must be the newest message: %+v", cursor)
	}
}

func TestSlackChannelMessagesAfterTheBookmark(t *testing.T) {
	f := newSlackFake(t)
	f.history["C0IN"] = `{"ok":true,"messages":[
		{"type":"message","user":"ULE","text":"and now it is green","ts":"1726000200.000100"},
		{"type":"message","user":"UME","text":"mine, not news","ts":"1726000150.000100"},
		{"type":"message","bot_id":"B1","subtype":"bot_message","text":"deploy finished","ts":"1726000100.000100"}
	],"has_more":false}`
	s := newTestSlack(f, SlackWatchConfig{Channels: []string{"#incidents"}})

	events, cursor, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "hist:C0IN": "1726000000.000100", "chan:#incidents": "C0IN"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("every message after the bookmark is an event; the rules decide, not the source: %d", len(events))
	}
	byTS := map[string]Event{}
	for _, e := range events {
		byTS[strings.TrimPrefix(e.Key, "slack:C0IN:")] = e
	}
	lena := byTS["1726000200.000100"]
	if lena.Kind != KindChatMessage || lena.State != "#incidents" || lena.Author != "@ule" {
		t.Fatalf("channel message = %+v", lena)
	}
	if lena.URL != "https://acme.slack.com/archives/C0IN/p1726000200000100" {
		t.Fatalf("link = %q", lena.URL)
	}
	if !byTS["1726000150.000100"].Self {
		t.Error("a message I wrote must be marked mine")
	}
	if !byTS["1726000100.000100"].Bot {
		t.Error("a bot message must be marked as one")
	}
	if cursor["hist:C0IN"] != "1726000200.000100" {
		t.Fatalf("the bookmark must move to the newest: %+v", cursor)
	}
}

func TestSlackMentionsComeFromSearchAndCarryTheThread(t *testing.T) {
	f := newSlackFake(t)
	f.search = `{"ok":true,"messages":{"matches":[
		{"ts":"1726000300.000100","text":"<@UME> updated","user":"UVI","username":"vincent",
		 "channel":{"id":"C0RE","name":"code-review"},
		 "permalink":"https://acme.slack.com/archives/C0RE/p1726000300000100?thread_ts=1726000100.000100"}
	],"paging":{"page":1,"pages":1}}}`
	f.replies = `{"ok":true,"messages":[
		{"type":"message","user":"UVI","text":"[HUM-1409] review please https://github.com/acme/api/pull/515","ts":"1726000100.000100"}]}`
	s := newTestSlack(f, SlackWatchConfig{Mentions: true})

	events, cursor, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "search": "1726000200.000000"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("one mention expected, got %d", len(events))
	}
	e := events[0]
	if e.Kind != KindChatMention || !e.Mine {
		t.Fatalf("a mention is mine by construction: %+v", e)
	}
	if !strings.Contains(e.Body, "In reply to @uvi:") || !strings.Contains(e.Body, "HUM-1409") {
		t.Fatalf("a reply in a thread must carry the parent, which is where the links are: %q", e.Body)
	}
	if len(e.Links) != 1 || e.Links[0] != "https://github.com/acme/api/pull/515" {
		t.Fatalf("the parent's pull requests must travel with it: %v", e.Links)
	}
	if cursor["thread:"+e.Key] != "1726000100.000100" {
		t.Fatalf("the thread must be remembered so a reply lands in it: %+v", cursor)
	}
	if cursor["search"] != "1726000300.000100" {
		t.Fatalf("the search bookmark must move: %+v", cursor)
	}
}

func TestSlackReviewChannelPostIsOneEventWithEveryLink(t *testing.T) {
	f := newSlackFake(t)
	f.history["C0RE"] = `{"ok":true,"messages":[
		{"type":"message","user":"UVI","ts":"1726000400.000100",
		 "text":"[HUM-1473] Send the consultation request\nAPI: https://github.com/acme/api/pull/519\nAdmin: https://github.com/acme/admin-portal/pull/76"},
		{"type":"message","user":"UVI","ts":"1726000350.000100","text":"no links here"}
	],"has_more":false}`
	s := newTestSlack(f, SlackWatchConfig{ReviewChannels: []string{"#code-review"}})

	events, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "hist:C0RE": "1726000300.000100", "chan:#code-review": "C0RE"})
	if err != nil {
		t.Fatal(err)
	}
	var review []Event
	for _, e := range events {
		if e.Kind == KindReviewRequested {
			review = append(review, e)
		}
	}
	if len(review) != 1 {
		t.Fatalf("one post with two links is one review to do, not two: %+v", events)
	}
	if len(review[0].Links) != 2 || review[0].Links[0] != "https://github.com/acme/api/pull/519" {
		t.Fatalf("links = %v", review[0].Links)
	}
	if review[0].Ref != "slack-1726000400" {
		t.Fatalf("ref = %q", review[0].Ref)
	}
	for _, e := range events {
		if e.Key == "slack:C0RE:1726000350.000100" && e.Kind == KindReviewRequested {
			t.Fatal("a post with no pull request is not a review request")
		}
	}
}

func TestSlackDMIsAMention(t *testing.T) {
	f := newSlackFake(t)
	f.history["D0VI"] = `{"ok":true,"messages":[
		{"type":"message","user":"UVI","text":"got a minute?","ts":"1726000500.000100"}],"has_more":false}`
	s := newTestSlack(f, SlackWatchConfig{Mentions: true})

	events, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "hist:D0VI": "1726000400.000100"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != KindChatMention {
		t.Fatalf("a direct message is a mention by construction: %+v", events)
	}
	if !strings.HasPrefix(events[0].Title, "DM from @uvi") {
		t.Fatalf("title = %q", events[0].Title)
	}
}
