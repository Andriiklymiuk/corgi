package watch

import (
	"testing"
	"time"
)

func TestClaimsAreHeldTakenOverReleasedAndForgotten(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	l := LoadFileClaims(dir)
	if taken, err := l.Set("/w/api", "s1", "api·auth", []string{"auth/refresh.go", "auth/session.go"}, now); err != nil || len(taken) != 0 {
		t.Fatalf("first claim: %v %v", taken, err)
	}
	taken, _ := l.Set("/w/api", "s2", "api·cart", []string{"auth/session.go"}, now)
	if len(taken) != 1 || taken[0].Session != "s1" {
		t.Fatalf("taking over says whose it was: %+v", taken)
	}
	live := LoadFileClaims(dir).Live(map[string]bool{"s1": true, "s2": true}, now)
	if len(live) != 2 || live[0].Path != "auth/refresh.go" || live[0].Session != "s1" || live[1].Session != "s2" {
		t.Fatalf("live: %+v", live)
	}
	crossed := Crossed(live, "/w/api", "s2", []string{"auth/refresh.go", "cart/total.go"})
	if len(crossed) != 1 || crossed[0].Label != "api·auth" {
		t.Fatalf("crossed: %+v", crossed)
	}
	if len(Crossed(live, "/w/web", "s2", []string{"auth/refresh.go"})) != 0 {
		t.Fatal("another repository's path is not the same file")
	}
	if got := LoadFileClaims(dir).Live(map[string]bool{"s2": true}, now); len(got) != 1 || got[0].Session != "s2" {
		t.Fatalf("gone: %+v", got)
	}
	if got := LoadFileClaims(dir).Live(nil, now.Add(FileClaimFor+time.Minute)); len(got) != 0 {
		t.Fatalf("a day: %+v", got)
	}
	l = LoadFileClaims(dir)
	_, _ = l.Set("/w/api", "s3", "", []string{"a.go", "b.go"}, now)
	if n, _ := l.Release("s3", []string{"a.go"}); n != 1 {
		t.Fatalf("released %d", n)
	}
	if n, _ := l.Release("s3", nil); n != 1 {
		t.Fatalf("released the rest: %d", n)
	}
}
