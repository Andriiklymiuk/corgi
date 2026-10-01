package watch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
			{"id":"D0TM","is_im":true,"user":"UTM"}
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
		{"type":"message","user":"USM","text":"deploying","ts":"1726000000.000100"}],"has_more":false}`
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
		{"type":"message","user":"USM","text":"and now it is green","ts":"1726000200.000100"},
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
	reviewer := byTS["1726000200.000100"]
	if reviewer.Kind != KindChatMessage || reviewer.State != "#incidents" || reviewer.Author != "@usm" {
		t.Fatalf("channel message = %+v", reviewer)
	}
	if reviewer.URL != "https://acme.slack.com/archives/C0IN/p1726000200000100" {
		t.Fatalf("link = %q", reviewer.URL)
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
		{"ts":"1726000300.000100","text":"<@UME> updated","user":"UTM","username":"teammate",
		 "channel":{"id":"C0RE","name":"code-review"},
		 "permalink":"https://acme.slack.com/archives/C0RE/p1726000300000100?thread_ts=1726000100.000100"}
	],"paging":{"page":1,"pages":1}}}`
	f.replies = `{"ok":true,"messages":[
		{"type":"message","user":"UTM","text":"[ABC-12] review please https://github.com/acme/api/pull/515","ts":"1726000100.000100"}]}`
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
	if !strings.Contains(e.Body, "In reply to @utm:") || !strings.Contains(e.Body, "ABC-12") {
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
		{"type":"message","user":"UTM","ts":"1726000400.000100",
		 "text":"[ABC-34] Send the welcome email\nAPI: https://github.com/acme/api/pull/519\nAdmin: https://github.com/acme/web/pull/76"},
		{"type":"message","user":"UTM","ts":"1726000350.000100","text":"no links here"}
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
	f.history["D0TM"] = `{"ok":true,"messages":[
		{"type":"message","user":"UTM","text":"got a minute?","ts":"1726000500.000100"}],"has_more":false}`
	s := newTestSlack(f, SlackWatchConfig{Mentions: true})

	events, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "hist:D0TM": "1726000400.000100"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != KindChatMention {
		t.Fatalf("a direct message is a mention by construction: %+v", events)
	}
	if !strings.HasPrefix(events[0].Title, "DM from @utm") {
		t.Fatalf("title = %q", events[0].Title)
	}
}

func TestSlackMentionsSurviveAMissingListScope(t *testing.T) {
	f := newSlackFake(t)
	f.convList = `{"ok":false,"error":"missing_scope"}`
	f.search = `{"ok":true,"messages":{"matches":[
		{"ts":"1726000300.000100","text":"<@UME> ping","user":"UTM","username":"teammate",
		 "channel":{"id":"C0IN","name":"incidents"},
		 "permalink":"https://acme.slack.com/archives/C0IN/p1726000300000100"}
	],"paging":{"page":1,"pages":1}}}`
	s := newTestSlack(f, SlackWatchConfig{Mentions: true})

	events, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme", "search": "1726000200.000000"})
	if err != nil {
		t.Fatalf("a token with search:read alone still finds mentions: %v", err)
	}
	if len(events) != 1 || events[0].Kind != KindChatMention {
		t.Fatalf("the mention from search: %+v", events)
	}

	s = newTestSlack(f, SlackWatchConfig{Mentions: true, Channels: []string{"#incidents"}})
	if _, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme"}); err == nil || !strings.Contains(err.Error(), "channels:read") {
		t.Fatalf("a listened channel needs the list, and the error must name the scope: %v", err)
	}
}

func TestSlackListsOnlyTheTypesTheTokenMayRead(t *testing.T) {
	f := newSlackFake(t)
	var asked []string
	full := f.convList
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/conversations.list" {
			types := r.URL.Query().Get("types")
			asked = append(asked, types)
			if strings.Contains(types, "private_channel") || strings.Contains(types, "im") {
				_, _ = w.Write([]byte(`{"ok":false,"error":"missing_scope","needed":"groups:read,im:read,mpim:read"}`))
				return
			}
			_, _ = w.Write([]byte(full))
			return
		}
		if r.URL.Path == "/search.messages" {
			_, _ = w.Write([]byte(f.search))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"messages":[],"has_more":false}`))
	})
	s := newTestSlack(f, SlackWatchConfig{Mentions: true, ReviewChannels: []string{"#code-review"}})
	_, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme"})
	if err != nil {
		t.Fatalf("a token that reads public channels only still lists them: %v", err)
	}
	if len(asked) != 4 || asked[0] != "public_channel" {
		t.Fatalf("each type is asked for on its own: %v", asked)
	}
}

func TestSlackKeepsPrivateChannelsWhenOnlyDMsAreOutOfScope(t *testing.T) {
	f := newSlackFake(t)
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/conversations.list" {
			types := r.URL.Query().Get("types")
			switch {
			case strings.Contains(types, "im"):
				_, _ = w.Write([]byte(`{"ok":false,"error":"missing_scope","needed":"im:read,mpim:read"}`))
			case types == "private_channel":
				_, _ = w.Write([]byte(`{"ok":true,"channels":[{"id":"GPRIV","name":"code-review","is_private":true}]}`))
			default:
				_, _ = w.Write([]byte(`{"ok":true,"channels":[{"id":"CPUB","name":"general"}]}`))
			}
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"messages":[],"has_more":false}`))
	})
	s := newTestSlack(f, SlackWatchConfig{})
	convs, err := s.conversations(context.Background())
	if err != nil {
		t.Fatalf("private channels list without a DM scope: %v", err)
	}
	var names []string
	for _, c := range convs {
		names = append(names, c.Name)
	}
	if len(names) != 2 || names[0] != "general" || names[1] != "code-review" {
		t.Fatalf("want the public and the private channel, got %v", names)
	}
}

func TestSlackReadsAHundredQuietDMsInTurnsNotEveryRound(t *testing.T) {
	f := newSlackFake(t)
	var list strings.Builder
	list.WriteString(`{"ok":true,"channels":[`)
	for i := 0; i < 100; i++ {
		if i > 0 {
			list.WriteString(",")
		}
		fmt.Fprintf(&list, `{"id":"D%03d","is_im":true,"user":"U%03d"}`, i, i)
	}
	list.WriteString(`],"response_metadata":{"next_cursor":""}}`)
	f.convList = list.String()
	calls := map[string]int{}
	read := map[string]bool{}
	inner := f.srv.Config.Handler
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		if r.URL.Path == "/conversations.history" {
			read[r.URL.Query().Get("channel")] = true
		}
		inner.ServeHTTP(w, r)
	})
	s := newTestSlack(f, SlackWatchConfig{Mentions: true})

	cursor := Cursor{"me": "UME", "team": "acme", "search": "1.0"}
	rounds := 10
	for i := 0; i < rounds; i++ {
		var err error
		if _, cursor, err = s.Poll(context.Background(), cursor); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls["/conversations.list"]; got != 4 {
		t.Errorf("the list is asked for once (4 types) per half hour, not every round: %d calls", got)
	}
	if got := calls["/conversations.history"]; got > rounds*slackColdPerRound {
		t.Errorf("%d DM reads in %d rounds; quiet DMs take turns, %d a round", got, rounds, slackColdPerRound)
	}
	if len(read) != 100 {
		t.Errorf("every DM still gets its turn: %d of 100 read", len(read))
	}
}

func TestSlackANewDMIsNotLostWhileItWaitsItsTurn(t *testing.T) {
	f := newSlackFake(t)
	s := newTestSlack(f, SlackWatchConfig{Mentions: true})
	s.now = func() time.Time { return time.Unix(1726000000, 0) }
	cursor := Cursor{"me": "UME", "team": "acme", "search": "1.0", "dmfloor": "1726000000.000000"}
	f.history["D0TM"] = `{"ok":true,"messages":[
		{"type":"message","user":"UTM","text":"can you look at the login bug","ts":"1726000300.000100"}],"has_more":false}`
	events, _, err := s.Poll(context.Background(), cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != KindChatMention {
		t.Fatalf("a DM read for the first time counts from when the watch began: %+v", events)
	}
}

func TestSlackPollWaitsOutA429(t *testing.T) {
	f := newSlackFake(t)
	hits := 0
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	s := newTestSlack(f, SlackWatchConfig{Mentions: true})
	now := time.Unix(1726000000, 0)
	s.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if _, _, err := s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme"}); err == nil {
			t.Fatal("a 429 is an error")
		}
	}
	if hits != 1 {
		t.Fatalf("polls inside Retry-After must not call Slack: %d calls", hits)
	}
	now = now.Add(2 * time.Minute)
	_, _, _ = s.Poll(context.Background(), Cursor{"me": "UME", "team": "acme"})
	if hits != 2 {
		t.Fatalf("after Retry-After the poll goes again: %d calls", hits)
	}
}
