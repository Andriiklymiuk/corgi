package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/push"
	"andriiklymiuk/corgi/utils/agent/rooms"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

func roomStubs(t *testing.T, agents []roomAgent) (*int, *[]push.Message) {
	t.Helper()
	oldAgents, oldDaemon, oldPush := roomAgents, roomDaemon, roomPush
	nudges := 0
	var pushes []push.Message
	roomAgents = func(string) []roomAgent { return agents }
	roomDaemon = func(string) (func(), bool) { return func() { nudges++ }, true }
	roomPush = func(_ string, m push.Message) { pushes = append(pushes, m) }
	t.Cleanup(func() { roomAgents, roomDaemon, roomPush = oldAgents, oldDaemon, oldPush })
	return &nudges, &pushes
}

func sentTexts(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, _ := os.ReadDir(command.Dir(dir))
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(command.Dir(dir), e.Name()))
		if err != nil {
			continue
		}
		var c command.Command
		if json.Unmarshal(raw, &c) == nil && c.Action == command.ActionSend {
			out[c.SessionID] = c.Text
		}
	}
	return out
}

func TestANamedSessionGetsTheMessageAsItsNextPrompt(t *testing.T) {
	home, srv, pair := roomsServer(t)
	nudges, _ := roomStubs(t, []roomAgent{{Handle: "api", Name: "api", Status: "done", id: "s-api"}, {Handle: "web", Name: "web", Status: "working", id: "s-web", Busy: "waits on a permission"}})
	phone := pair("phone", "")

	code, out := call(t, srv, phone, http.MethodPost, "/launch/rooms", `{"room":"plans/search","text":"@api what does the index cost? @web and you? @nobody"}`, nil)
	if code != http.StatusOK {
		t.Fatalf("post = %d %v", code, out)
	}
	sent, _ := out["sent"].([]any)
	missed, _ := out["missed"].(map[string]any)
	if len(sent) != 1 || sent[0] != "api" || missed["web"] == nil || missed["nobody"] == nil {
		t.Fatalf("sent %v missed %v", sent, missed)
	}
	texts := sentTexts(t, home)
	if len(texts) != 1 || !strings.Contains(texts["s-api"], "corgi agent room post plans/search --thread m1") || !strings.Contains(texts["s-api"], "from phone") {
		t.Fatalf("commands = %v", texts)
	}
	if *nudges != 1 {
		t.Fatalf("nudges = %d", *nudges)
	}
	list, _ := rooms.Read(home, rooms.Query{Room: "plans/search"})
	if len(list) != 1 || len(list[0].To) != 1 || list[0].To[0] != "api" || list[0].Via != roomViaDevice {
		t.Fatalf("kept = %+v", list)
	}
}

func TestAReadOnlyDeviceNamesNobody(t *testing.T) {
	home, srv, pair := roomsServer(t)
	roomStubs(t, []roomAgent{{Handle: "api", Name: "api", id: "s-api"}})
	if _, err := rooms.AddGrant(home, "guest", "all=read,post", time.Now()); err != nil {
		t.Fatal(err)
	}
	guest := pair("guest", pairing.RoleViewer)
	code, out := call(t, srv, guest, http.MethodPost, "/launch/rooms", `{"room":"plans","text":"@api do it"}`, nil)
	if code == http.StatusOK {
		if sent, _ := out["sent"].([]any); len(sent) != 0 {
			t.Fatalf("a viewer reached a session: %v", out)
		}
	}
	if len(sentTexts(t, home)) != 0 {
		t.Fatal("a viewer wrote a command")
	}
}

func TestAnAgentsReplyToAPhonesThreadRingsThePhone(t *testing.T) {
	home, _, _ := roomsServer(t)
	_, pushes := roomStubs(t, []roomAgent{{Handle: "api", Name: "api", id: "s-api"}})
	now := time.Now()
	first, err := postToRoom(home, rooms.Message{Room: "plans", By: "phone", Text: "@api cost?", Via: roomViaDevice}, true, now)
	if err != nil || len(first.Sent) != 1 {
		t.Fatalf("first = %+v %v", first, err)
	}
	if len(*pushes) != 0 {
		t.Fatal("a phone's own message rang the phone")
	}
	reply, err := postToRoom(home, rooms.Message{Room: "plans", By: "api", Text: "about 40 MB", Thread: first.Message.ID, Via: roomViaAgent}, true, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(*pushes) != 1 || (*pushes)[0].Data["room"] != "plans" || (*pushes)[0].Data["thread"] != first.Message.ID {
		t.Fatalf("pushes = %+v", *pushes)
	}
	if len(reply.Sent) != 0 {
		t.Fatalf("a reply without a name was sent on: %+v", reply)
	}
	if _, err := postToRoom(home, rooms.Message{Room: "notes", By: "api", Text: "nobody asked", Via: roomViaAgent}, true, now); err != nil || len(*pushes) != 1 {
		t.Fatalf("an unasked note rang: %v %d", err, len(*pushes))
	}
	if _, err := postToRoom(home, rooms.Message{Room: "notes", By: "api", Text: "@you look", Via: roomViaAgent}, true, now); err != nil || len(*pushes) != 2 {
		t.Fatalf("@you did not ring: %v %d", err, len(*pushes))
	}
}

func TestARoomStopsTwoAgentsCallingEachOtherWithoutEnd(t *testing.T) {
	home, _, _ := roomsServer(t)
	roomStubs(t, []roomAgent{{Handle: "api", Name: "api", id: "s-api"}, {Handle: "web", Name: "web", id: "s-web"}})
	now := time.Now()
	for i := 0; i < roomMentionLimit; i++ {
		by, to := "api", "@web"
		if i%2 == 1 {
			by, to = "web", "@api"
		}
		out, err := postToRoom(home, rooms.Message{Room: "loop", By: by, Text: to + " again"}, true, now)
		if err != nil || len(out.Sent) != 1 {
			t.Fatalf("round %d: %+v %v", i, out, err)
		}
	}
	out, err := postToRoom(home, rooms.Message{Room: "loop", By: "api", Text: "@web again"}, true, now)
	if err != nil || len(out.Sent) != 0 || out.Missed["web"] == "" {
		t.Fatalf("over the limit: %+v %v", out, err)
	}
	if out, _ := postToRoom(home, rooms.Message{Room: "loop", By: "api", Text: "@api me"}, true, now); len(out.Sent) != 0 {
		t.Fatal("an agent named itself")
	}
}

func TestHereIsEveryoneWhoWroteInTheRoom(t *testing.T) {
	home, _, _ := roomsServer(t)
	roomStubs(t, []roomAgent{{Handle: "api", Name: "api", id: "s-api"}, {Handle: "web", Name: "web", id: "s-web"}, {Handle: "ios", Name: "ios", id: "s-ios"}})
	now := time.Now()
	for _, by := range []string{"api", "web"} {
		if _, err := postToRoom(home, rooms.Message{Room: "r", By: by, Text: "in"}, true, now); err != nil {
			t.Fatal(err)
		}
	}
	out, err := postToRoom(home, rooms.Message{Room: "r", By: "phone", Text: "@here one more round", Via: roomViaDevice}, true, now)
	if err != nil || len(out.Sent) != 2 {
		t.Fatalf("here = %+v %v", out, err)
	}
}

func TestSessionsGetNamesThatCanBeTyped(t *testing.T) {
	list := roomAgentsOf([]sessions.Session{
		{ID: "abcd1234", Label: "corgi", Display: "corgi·fix login", Status: sessions.StatusWorking},
		{ID: "ef561234", Label: "corgi", Display: "corgi·fix login", Status: sessions.StatusDone},
		{ID: "gone", Label: "old", Status: sessions.StatusGone},
		{ID: "p1", Label: "api", Status: sessions.StatusNeedsInput, Pending: &sessions.Pending{}},
	})
	if len(list) != 3 || list[0].Handle != "corgi-fix-login" || list[1].Handle != "corgi-fix-login-ef56" || list[2].Busy == "" {
		t.Fatalf("agents = %+v", list)
	}
	if got := rooms.Mentions("hi @Api, and (@web-2) not mail@host.com @api"); len(got) != 2 || got[0] != "api" || got[1] != "web-2" {
		t.Fatalf("mentions = %v", got)
	}
}
