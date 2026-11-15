package cmd

import (
	"testing"

	"andriiklymiuk/corgi/utils"
	"time"

	"andriiklymiuk/corgi/utils/agent/timers"
)

func TestAgentLater(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 15, 14, 0, 0, 0, time.Local)
	laterSession = "api"
	t.Cleanup(func() { laterSession = "" })
	if err := runAgentLater(dir, []string{"45m", "look", "at", "CI"}, now); err != nil {
		t.Fatal(err)
	}
	laterSession = ""
	if err := runAgentLater(dir, []string{"tomorrow", "9:30", "standup"}, now); err != nil {
		t.Fatal(err)
	}
	list, _ := timers.List(dir)
	if len(list) != 2 || list[0].Text != "look at CI" || list[0].Session != "api" || list[1].Text != "standup" || list[1].At.Hour() != 9 {
		t.Fatalf("timers: %+v", list)
	}
	if err := runAgentLater(dir, []string{"cancel", list[0].ID}, now); err != nil {
		t.Fatal(err)
	}
	if err := runAgentLater(dir, []string{"cancel"}, now); err == nil {
		t.Fatal("cancel without id")
	}
	if err := runAgentLater(dir, []string{"whenever", "x"}, now); err == nil {
		t.Fatal("bad time accepted")
	}
	if err := runAgentLater(dir, nil, now); err != nil {
		t.Fatal(err)
	}
	prev := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = prev })
	if err := runAgentLater(dir, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := runAgentLater(dir, []string{"2h", "json"}, now); err != nil {
		t.Fatal(err)
	}
	left, _ := timers.List(dir)
	if err := runAgentLater(dir, []string{"cancel", left[0].ID}, now); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	if err := runAgentLater(empty, nil, now); err != nil {
		t.Fatal(err)
	}
}
