package cmd

import (
	"errors"
	"github.com/mark3labs/mcp-go/mcp"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestChatTargetFromFlagsAndEvent(t *testing.T) {
	state := watch.LoadState(t.TempDir())
	state.SetThreadForTest("slack:C0RE:1726000400.000100", "1726000100.000100")

	got, err := chatTarget(chatRequest{Reply: "slack:C0RE:1726000400.000100"}, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel != "C0RE" || got.ThreadTS != "1726000100.000100" {
		t.Fatalf("a reply to a message in a thread goes to that thread: %+v", got)
	}

	got, err = chatTarget(chatRequest{Reply: "slack:C0RE:1726000900.000100"}, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.ThreadTS != "1726000900.000100" {
		t.Fatalf("a reply to a top-level post opens a thread on it: %+v", got)
	}

	if _, err := chatTarget(chatRequest{Reply: "github:acme/api#1"}, state, nil); err == nil {
		t.Fatal("a key from another source is not a slack target")
	}

	if _, err := chatTarget(chatRequest{}, state, nil); err == nil || !strings.Contains(err.Error(), "--to") {
		t.Fatalf("with no target and no config the error must name the flag: %v", err)
	}

	got, err = chatTarget(chatRequest{}, state, &slackDefaults{PostTo: "C0IN", ReplyAs: "me"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Channel != "C0IN" || got.As != "me" {
		t.Fatalf("the workspace's own default target and voice: %+v", got)
	}

	got, err = chatTarget(chatRequest{As: "bot"}, state, &slackDefaults{PostTo: "C0IN", ReplyAs: "me"})
	if err != nil {
		t.Fatal(err)
	}
	if got.As != "bot" {
		t.Fatalf("an explicit voice beats the workspace's: %+v", got)
	}
}

func TestASessionCannotAskToSpeakAsThePerson(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"text": "approved", "to": "#code-review", "as": "me"}

	if _, err := mcpChatPost(req); err == nil {
		t.Fatal("a session must not be able to put words in its owner's mouth")
	} else if !strings.Contains(err.Error(), "under your name") {
		t.Fatalf("the refusal should say why and what to do instead: %v", err)
	}

	req.Params.Arguments = map[string]any{"text": "approved", "as": "ME"}
	if _, err := mcpChatPost(req); err == nil || !strings.Contains(err.Error(), "under your name") {
		t.Fatalf("the check is on the meaning, not the spelling: %v", err)
	}
}

func TestChatEditChecksTheMessageBeforeAnyToken(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	t.Setenv("SLACK_USER_TOKEN", "")
	t.Setenv("SLACK_BOT_TOKEN", "")

	for _, ref := range []string{"", "github:acme/api#1", "https://acme.slack.com/archives/C0RE", "slack:C0RE:latest"} {
		if err := runChatEdit(chatEditRequest{Message: ref, Text: "new", Workspace: "acme"}); err == nil {
			t.Fatalf("%q names no message and must be refused", ref)
		}
	}
	link := "https://acme.slack.com/archives/C0RE/p1726000400000100"
	if err := runChatEdit(chatEditRequest{Message: link, Text: "  ", Workspace: "acme"}); err == nil || !strings.Contains(err.Error(), "new text") {
		t.Fatalf("an empty edit would blank the message: %v", err)
	}
	if err := runChatEdit(chatEditRequest{Message: link, Text: "new", Workspace: "acme"}); !errors.Is(err, watch.ErrNoSlackToken) {
		t.Fatalf("with no token stored it says so: %v", err)
	}

	if got := chatVoice("", &slackDefaults{ReplyAs: "me"}); got != "me" {
		t.Fatalf("an edit speaks in the workspace's replyAs, as a post does: %q", got)
	}
	if got := chatVoice(" bot ", &slackDefaults{ReplyAs: "me"}); got != "bot" {
		t.Fatalf("--as beats replyAs: %q", got)
	}
	if found, _, err := agentChatCmd.Find([]string{"edit"}); err != nil || found != agentChatEditCmd {
		t.Fatalf("chat edit is a subcommand: %v", err)
	}
}
