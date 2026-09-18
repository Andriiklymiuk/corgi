package cmd

import "testing"

func TestAnnounceText(t *testing.T) {
	got := announceText("[ABC-12] Policy request from the insurer", []string{
		"https://github.com/acme/api/pull/522",
		"https://github.com/acme/client-hybrid-mobile-app/pull/268",
		"https://gitlab.com/acme/svc/onboarding/-/merge_requests/12",
		"https://example.test/not-a-pr",
	})
	want := "[ABC-12] Policy request from the insurer\n" +
		"api: https://github.com/acme/api/pull/522\n" +
		"client-hybrid-mobile-app: https://github.com/acme/client-hybrid-mobile-app/pull/268\n" +
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
