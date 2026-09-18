package cmd

import (
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
