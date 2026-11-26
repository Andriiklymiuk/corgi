package cmd

import (
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/watch"
)

func TestAnnounceText(t *testing.T) {
	got := announceText("[ABC-12] Policy request from the insurer", []string{
		"https://github.com/acme/api/pull/522",
		"https://github.com/acme/mobile/pull/268",
		"https://gitlab.com/acme/svc/onboarding/-/merge_requests/12",
		"https://example.test/not-a-pr",
	})
	want := "[ABC-12] Policy request from the insurer\n" +
		"api: https://github.com/acme/api/pull/522\n" +
		"mobile: https://github.com/acme/mobile/pull/268\n" +
		"onboarding: https://gitlab.com/acme/svc/onboarding/-/merge_requests/12\n" +
		"https://example.test/not-a-pr"
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestAnnounceChannel(t *testing.T) {
	if got := announceChannel("#x", &slackDefaults{PostTo: "#post", ReviewChannels: []string{"#review"}}); got != "#x" {
		t.Fatal(got)
	}
	if got := announceChannel("", &slackDefaults{PostTo: "#post", ReviewChannels: []string{"#review"}}); got != "#review" {
		t.Fatalf("the review channel is where work is announced: %s", got)
	}
	if got := announceChannel("", &slackDefaults{PostTo: "#post"}); got != "#post" {
		t.Fatal(got)
	}
	if got := announceChannel("", nil); got != "" {
		t.Fatal(got)
	}
}

func TestAnnounceEditSendsTheBodyAFreshAnnounceWould(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	args := []string{"[ABC-12] Phone field",
		"https://github.com/acme/api/pull/5",
		"https://github.com/acme/web/pull/9",
		"https://github.com/acme/mobile/pull/301",
		"https://gitlab.com/acme/svc/onboarding/-/merge_requests/12",
	}
	var posted []chatRequest
	var edited []chatEditRequest
	post := func(r chatRequest) (watch.SlackPosted, error) {
		posted = append(posted, r)
		return watch.SlackPosted{Channel: "C0RE", TS: "1726000400.000100"}, nil
	}
	edit := func(r chatEditRequest) error { edited = append(edited, r); return nil }

	if err := runAnnounce(args, announceOptions{To: "#reviews", As: "me", Workspace: "acme"}, post, edit); err != nil {
		t.Fatal(err)
	}
	link := "https://acme.slack.com/archives/C0RE/p1726000400000100"
	if err := runAnnounce(args, announceOptions{Edit: link, As: "me", Workspace: "acme"}, post, edit); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 || len(edited) != 1 {
		t.Fatalf("one post and one edit, never both: %d posts, %d edits", len(posted), len(edited))
	}
	if edited[0].Text != posted[0].Text || posted[0].Text != announceText(args[0], args[1:]) {
		t.Fatalf("the edit carries exactly the announce body:\npost:\n%s\nedit:\n%s", posted[0].Text, edited[0].Text)
	}
	if edited[0].Message != link || edited[0].As != "me" || edited[0].Workspace != "acme" {
		t.Fatalf("the edit names the message and keeps the voice and workspace: %+v", edited[0])
	}

	if err := runAnnounce(args, announceOptions{Edit: link, To: "#reviews"}, post, edit); err == nil || !strings.Contains(err.Error(), "--to") {
		t.Fatalf("the message names its own channel, so --to with --edit is refused: %v", err)
	}
	if err := runAnnounce(args[:1], announceOptions{Edit: link}, post, edit); err == nil || !strings.Contains(err.Error(), "nothing to announce") {
		t.Fatalf("an edit without pull requests would wipe the post: %v", err)
	}
	if len(posted) != 1 || len(edited) != 1 {
		t.Fatalf("refusals send nothing: %d posts, %d edits", len(posted), len(edited))
	}
	if agentChatAnnounceCmd.Flags().Lookup("edit") == nil {
		t.Fatal("announce has --edit")
	}
}

func TestAnAnnounceIsRememberedAsTheReviewPostOfItsPullRequests(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", t.TempDir())
	dir := mustAgentDir()
	args := []string{"[ABC-12] Phone field",
		"https://github.com/acme/api/pull/5",
		"https://gitlab.com/acme/svc/onboarding/-/merge_requests/12",
	}
	post := func(chatRequest) (watch.SlackPosted, error) {
		return watch.SlackPosted{Channel: "C0RE", TS: "1726000400.000100"}, nil
	}
	edit := func(chatEditRequest) error { return nil }
	if err := runAnnounce(args, announceOptions{To: "#reviews", Workspace: "acme"}, post, edit); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"acme/api#5", "acme/svc/onboarding!12"} {
		got := watch.ChannelPostsNaming(dir, "acme", []string{ref})
		if len(got) != 1 || got[0].Channel != "C0RE" || got[0].TS != "1726000400.000100" {
			t.Fatalf("the announce is the review post of %s, known without the watch seeing it: %+v", ref, got)
		}
	}

	link := "https://acme.slack.com/archives/C0RE/p1726000400000100"
	grown := append(args, "https://github.com/acme/web/pull/9")
	if err := runAnnounce(grown, announceOptions{Edit: link, Workspace: "acme"}, post, edit); err != nil {
		t.Fatal(err)
	}
	if got := watch.ChannelPostsNaming(dir, "acme", []string{"acme/web#9"}); len(got) != 1 || got[0].TS != "1726000400.000100" {
		t.Fatalf("an edit that adds a pull request adds it to the same post: %+v", got)
	}
	if all := watch.ChannelPostsNaming(dir, "acme", []string{"acme/api#5"}); len(all) != 1 {
		t.Fatalf("one post, not two: %+v", all)
	}
}
