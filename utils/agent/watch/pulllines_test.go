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
