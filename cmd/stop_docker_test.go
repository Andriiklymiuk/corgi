package cmd

import (
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestHasContainerBackedEntries(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{{Name: "a", PID: 42}}}
	if hasContainerBackedEntries(st) {
		t.Fatal("pid-tracked only → false")
	}
	st.Services = append(st.Services, utils.RunStateEntry{Name: "b", PID: 0})
	if !hasContainerBackedEntries(st) {
		t.Fatal("pid==0 entry → true")
	}
}

func TestContainerBackedNotInCompose(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "flipped", PID: 0},
		{Name: "declared", PID: 0},
		{Name: "native", PID: 99},
	}}
	corgi := &utils.CorgiCompose{Services: []utils.Service{
		{ServiceName: "declared", Runner: utils.Runner{Name: "docker"}},
		{ServiceName: "native"},
	}}
	got := containerBackedNotInCompose(st, corgi)
	if len(got) != 1 || got[0] != "flipped" {
		t.Fatalf("want [flipped], got %v", got)
	}
}
