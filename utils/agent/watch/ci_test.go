package watch

import "testing"

func TestRedBuildsOnlyArriveWhenAsked(t *testing.T) {
	off := Rules{Enabled: true, PRs: true}
	red := Event{Kind: KindCIFailed, Ref: "acme/api", Mine: true, Title: "e2e failed"}
	if off.Match(red) {
		t.Fatal("nobody gets red builds unasked")
	}
	if why := off.Why(red); why != "red builds need --ci" {
		t.Fatalf("the reason has to name the flag: %q", why)
	}

	on := Rules{Enabled: true, CI: true}
	if !on.Match(red) {
		t.Fatalf("--ci takes them: %s", on.Why(red))
	}
	if on.Match(Event{Kind: KindCIFailed, Ref: "someone/else"}) {
		t.Fatal("a build on something that is not mine is not my problem")
	}

	if on.DeadSource("github") {
		t.Fatal("--ci keeps github worth polling")
	}
	if !(Rules{Enabled: true}).DeadSource("github") {
		t.Fatal("no PRs and no CI means github has nothing to say")
	}
}
