package cmd

import (
	"andriiklymiuk/corgi/utils"
	"testing"
)

func TestStopTargets(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Status: "running"},
		{Name: "web", Kind: "service", PID: 2, Status: "crashed"},
	}}
	all := stopTargets(st, "")
	if len(all) != 2 {
		t.Errorf("want 2 targets, got %d", len(all))
	}
	one := stopTargets(st, "api")
	if len(one) != 1 || one[0].Name != "api" {
		t.Errorf("want only api, got %+v", one)
	}
	none := stopTargets(st, "nope")
	if len(none) != 0 {
		t.Errorf("unknown service should yield 0 targets, got %d", len(none))
	}
}
