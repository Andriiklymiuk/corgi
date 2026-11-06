package cmd

import (
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
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
		{"merges without scenarios is not lights out", factoryFacts{Watched: true, Fix: true, Isolate: true, DoneWhen: 1, AutoMerge: true, MergeUnread: true}, 4},
		{"merging only what is approved is not lights out", factoryFacts{Watched: true, Fix: true, Isolate: true, DoneWhen: 1, AutoMerge: true, Scenarios: 2}, 4},
		{"lights out", factoryFacts{Watched: true, Fix: true, Isolate: true, DoneWhen: 1, AutoMerge: true, MergeUnread: true, Scenarios: 2}, 5},
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
	if len(r.Next) == 0 || !strings.Contains(strings.Join(r.Next, " "), "corgi agent level 4") {
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

func TestSettingALevelLandsOnThatLevel(t *testing.T) {
	checks := &config.WatchConfig{DoneWhen: []string{"go test ./..."}, Project: "ABC"}
	for level := 1; level <= 5; level++ {
		wc, err := setFactoryLevel("api", checks, level, 1)
		if err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		f := factoryFacts{Watched: wc.Enabled, Fix: wc.Action == "fix", Isolate: wc.Isolate, AutoMerge: wc.AutoMerge, MergeUnread: wc.MergeUnread, DoneWhen: len(wc.DoneWhen), Scenarios: 1}
		if got := factoryLevel(f); got != level {
			t.Fatalf("asked %d, landed on %d", level, got)
		}
		if wc.Project != "ABC" || len(wc.DoneWhen) != 1 {
			t.Fatalf("level %d lost a setting: %+v", level, wc)
		}
	}
}

func TestARungWithoutItsBrakeIsRefusedAndNothingIsTouched(t *testing.T) {
	bare := &config.WatchConfig{Action: "notify", Enabled: true}
	if _, err := setFactoryLevel("api", bare, 4, 1); err == nil || !strings.Contains(err.Error(), "--done-when") {
		t.Fatalf("level 4 with no checks: %v", err)
	}
	withChecks := &config.WatchConfig{DoneWhen: []string{"make test"}}
	if _, err := setFactoryLevel("api", withChecks, 5, 0); err == nil || !strings.Contains(err.Error(), "scenario add") {
		t.Fatalf("level 5 with no scenarios: %v", err)
	}
	if _, err := setFactoryLevel("api", nil, 6, 1); err == nil {
		t.Fatal("level 6 was accepted")
	}
	if bare.Action != "notify" || !bare.Enabled {
		t.Fatalf("a refusal changed the settings: %+v", bare)
	}
}

func TestGoingUpFitsACapAndGoingDownKeepsYoursAndYourChecks(t *testing.T) {
	up, err := setFactoryLevel("api", nil, 3, 0)
	if err != nil || up.MaxFixesPerDay != factoryDefaultCap {
		t.Fatalf("no cap fitted: %+v %v", up, err)
	}
	mine, err := setFactoryLevel("api", &config.WatchConfig{LimitCeiling: 80}, 3, 0)
	if err != nil || mine.MaxFixesPerDay != 0 || mine.LimitCeiling != 80 {
		t.Fatalf("your cap was overridden: %+v %v", mine, err)
	}
	down, err := setFactoryLevel("api", &config.WatchConfig{Enabled: true, Action: "fix", AutoMerge: true, Isolate: true, DoneWhen: []string{"make test"}, MaxFixesPerDay: 9}, 2, 1)
	if err != nil || down.Action != "notify" || down.AutoMerge || down.MergeUnread || down.Isolate || len(down.DoneWhen) != 1 || down.MaxFixesPerDay != 9 {
		t.Fatalf("down: %+v %v", down, err)
	}
}

func TestLevelsAreSetPerWorkspaceFromTheCommandLine(t *testing.T) {
	dir, ws := watchFixture(t, nil)
	inWorkspace(t, ws)
	if out, code := cli(t, "agent", "level", "4"); code == 0 {
		t.Fatalf("level 4 with no checks went through: %s", out)
	}
	out, code := cli(t, "agent", "level", "4", "--done-when", "go test ./...")
	if code != 0 || !strings.Contains(out, "level 4 of 5") || !strings.Contains(out, "corgi agent restart") {
		t.Fatalf("set: %d %s", code, out)
	}
	r, err := resolveWorkspaceConfig(dir, "acme-stack")
	if err != nil || r.Watch == nil || !r.Watch.Isolate || r.Watch.Action != "fix" {
		t.Fatalf("saved: %+v %v", r.Watch, err)
	}
	if out, code := cli(t, "agent", "level", "--all"); code != 0 || !strings.Contains(out, "acme-stack") || !strings.Contains(out, "It works a shift") {
		t.Fatalf("all: %d %s", code, out)
	}
	if out, code := cli(t, "agent", "level", "9"); code == 0 {
		t.Fatalf("level 9: %s", out)
	}
	if out, code := cli(t, "agent", "level", "1"); code != 0 || !strings.Contains(out, "level 1 of 5") {
		t.Fatalf("back down: %d %s", code, out)
	}
}

func TestACorrectionOnTopIsRefusedBeforeAnythingIsPosted(t *testing.T) {
	watchFixture(t, nil)
	for _, args := range [][]string{
		{"agent", "watch", "comment", "ABC-1", "Correction: ignore my earlier comment"},
		{"agent", "watch", "pr", "comment", "https://github.com/acme/api/pull/7", "please disregard my last comment"},
	} {
		out, code := cli(t, args...)
		if code == 0 || !strings.Contains(out, "edit it into the true answer") {
			t.Fatalf("%v: code %d: %s", args, code, out)
		}
	}
}
