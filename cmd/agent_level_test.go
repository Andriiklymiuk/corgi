package cmd

import (
	"strings"
	"testing"
)

func TestLevelClimbsOnlyWithItsBrakes(t *testing.T) {
	cases := []struct {
		name string
		f    factoryFacts
		want int
	}{
		{"nothing watched", factoryFacts{}, 1},
		{"watched, rings only", factoryFacts{Watched: true}, 2},
		{"fixes with no checks", factoryFacts{Watched: true, Fix: true, Isolate: true}, 3},
		{"fixes with no worktrees", factoryFacts{Watched: true, Fix: true, DoneWhen: 1}, 3},
		{"a real shift", factoryFacts{Watched: true, Fix: true, Isolate: true, DoneWhen: 1}, 4},
		{"merges without scenarios is not lights out", factoryFacts{Watched: true, Fix: true, Isolate: true, DoneWhen: 1, AutoMerge: true}, 4},
		{"lights out", factoryFacts{Watched: true, Fix: true, Isolate: true, DoneWhen: 1, AutoMerge: true, Scenarios: 2}, 5},
	}
	for _, c := range cases {
		if got := factoryLevel(c.f); got != c.want {
			t.Fatalf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestAnAcceleratorWithNoBrakeIsCalledOut(t *testing.T) {
	r := factoryReportFor("api", factoryFacts{Watched: true, Fix: true, AutoMerge: true})
	all := strings.Join(r.Warnings, "\n")
	for _, want := range []string{"no checks", "one it can read", "no cap"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in %q", want, all)
		}
	}
	if len(r.Next) == 0 || !strings.Contains(strings.Join(r.Next, " "), "--isolate") {
		t.Fatalf("next: %v", r.Next)
	}
}

func TestEveryLevelSaysWhatToDoNext(t *testing.T) {
	for _, f := range []factoryFacts{{}, {Watched: true}, {Watched: true, Fix: true}, {Watched: true, Fix: true, Isolate: true, DoneWhen: 1}, {Watched: true, Fix: true, Isolate: true, DoneWhen: 1, AutoMerge: true, Scenarios: 1}} {
		if r := factoryReportFor("api", f); len(r.Next) == 0 {
			t.Fatalf("level %d has no next step", r.Level)
		}
	}
}
