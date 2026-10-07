package watch

import "testing"

func TestPullLinesNameEachRepository(t *testing.T) {
	got := PullLines("[ABC-12] Policy request from the insurer", []string{
		"https://github.com/acme/api/pull/522",
		"https://gitlab.com/acme/svc/onboarding/-/merge_requests/12",
		"https://example.test/not-a-pr",
		" ",
	})
	want := "[ABC-12] Policy request from the insurer\n" +
		"api: https://github.com/acme/api/pull/522\n" +
		"onboarding: https://gitlab.com/acme/svc/onboarding/-/merge_requests/12\n" +
		"https://example.test/not-a-pr"
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestPullLinesPostsALonePullRequestAsTheBareLink(t *testing.T) {
	got := PullLines("Mobile: bump version to v1.0.49", []string{
		"https://github.com/acme/client-hybrid-mobile-app/pull/291",
		" ",
	})
	want := "Mobile: bump version to v1.0.49\n" +
		"https://github.com/acme/client-hybrid-mobile-app/pull/291"
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
}

func TestPullLinesKeepsOnlyTheLinkWhenTheCallerPrefixedIt(t *testing.T) {
	got := PullLines("[ABC-12] Phone field", []string{
		"Mobile: https://github.com/acme/mobile/pull/7",
		"api: https://github.com/acme/api/pull/5",
	})
	want := "[ABC-12] Phone field\n" +
		"mobile: https://github.com/acme/mobile/pull/7\n" +
		"api: https://github.com/acme/api/pull/5"
	if got != want {
		t.Fatalf("got:\n%s", got)
	}
	if single := PullLines("Mobile: bump", []string{"Mobile: https://github.com/acme/mobile/pull/7"}); single != "Mobile: bump\nhttps://github.com/acme/mobile/pull/7" {
		t.Fatalf("got:\n%s", single)
	}
}
