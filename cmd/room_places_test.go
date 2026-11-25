package cmd

import (
	"net/http"
	"testing"

	"andriiklymiuk/corgi/utils/agent/rooms"
)

func TestAWorkspaceRoomReachesTheSessionsOnItWithoutAName(t *testing.T) {
	home, srv, pair := roomsServer(t)
	roomStubs(t, []roomAgent{
		{Handle: "api", Name: "api", Workspace: "Acme API", Ticket: "ABC-12", Status: "done", id: "s-api"},
		{Handle: "web", Name: "web", Workspace: "web", Status: "done", id: "s-web"},
	})
	phone := pair("phone", "")

	code, out := call(t, srv, phone, http.MethodPost, "/launch/rooms", `{"room":"ws/acme-api","text":"is the migration safe to run twice?"}`, nil)
	sent, _ := out["sent"].([]any)
	if code != http.StatusOK || len(sent) != 1 || sent[0] != "api" {
		t.Fatalf("the workspace's session gets it: %d %v", code, out)
	}
	if texts := sentTexts(t, home); texts["s-web"] != "" {
		t.Fatal("a session of another workspace does not")
	}
	_, out = call(t, srv, phone, http.MethodPost, "/launch/rooms", `{"room":"ticket/abc-12","text":"what is left?"}`, nil)
	if sent, _ := out["sent"].([]any); len(sent) != 1 || sent[0] != "api" {
		t.Fatalf("the ticket's session gets it: %v", out)
	}
	_, out = call(t, srv, phone, http.MethodPost, "/launch/rooms", `{"room":"ws/nobody","text":"anyone?"}`, nil)
	if missed, _ := out["missed"].(map[string]any); missed["ws/nobody"] == nil {
		t.Fatalf("an empty workspace says so: %v", out)
	}
	_, out = call(t, srv, phone, http.MethodPost, "/launch/rooms", `{"room":"plans/search","text":"no name here"}`, nil)
	if out["sent"] != nil {
		t.Fatalf("a plain room still needs a name: %v", out)
	}
}

func TestPlacesListEveryWorkspaceAndTicketWithALiveSession(t *testing.T) {
	agents := []roomAgent{{Handle: "api", Workspace: "Acme API", Ticket: "ABC-12"}, {Handle: "api-2", Workspace: "Acme API"}}
	places := roomPlaces(agents, []rooms.Info{{Name: "ws/old", Messages: 3}, {Name: "plans/search", Messages: 1}})
	if len(places) != 3 || places[0].Kind != roomKindWorkspace || places[2].Room != "ticket/abc-12" {
		t.Fatalf("workspaces first, then tickets, plain rooms left out: %+v", places)
	}
	for _, p := range places {
		if p.Room == "ws/acme-api" && len(p.Agents) != 2 {
			t.Fatalf("both sessions of the workspace are named: %+v", p)
		}
		if _, err := rooms.CleanName(p.Room); err != nil {
			t.Fatalf("%s is a valid room name: %v", p.Room, err)
		}
	}
}
