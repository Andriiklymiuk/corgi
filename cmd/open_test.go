package cmd

import (
	"reflect"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestOpenTargets_AllWithPorts(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "api", Port: 3000},
			{ServiceName: "web", Port: 5173},
			{ServiceName: "worker"}, // no port -> skipped
		},
	}
	got := openTargets(corgi, nil)
	want := []openTarget{
		{Service: "api", URL: "http://localhost:3000"},
		{Service: "web", URL: "http://localhost:5173"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("openTargets = %+v, want %+v", got, want)
	}
}

func TestOpenTargets_Subset(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "api", Port: 3000},
			{ServiceName: "web", Port: 5173},
		},
	}
	got := openTargets(corgi, []string{"web"})
	want := []openTarget{{Service: "web", URL: "http://localhost:5173"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("openTargets subset = %+v, want %+v", got, want)
	}
}

func TestOpenTargets_UnknownNamesReturnEmpty(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{{ServiceName: "api", Port: 3000}},
	}
	got := openTargets(corgi, []string{"nope"})
	if len(got) != 0 {
		t.Fatalf("expected no targets for unknown service, got %+v", got)
	}
}

func TestRunOpen_NonInteractiveSkipsLauncher(t *testing.T) {
	called := false
	orig := launcher
	launcher = func(string) error { called = true; return nil }
	defer func() { launcher = orig }()

	origNI := utils.NonInteractive
	utils.NonInteractive = true
	defer func() { utils.NonInteractive = origNI }()

	corgi := &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "api", Port: 3000}}}
	for _, tg := range openTargets(corgi, nil) {
		if utils.NonInteractive {
			continue
		}
		_ = launcher(tg.URL)
	}
	if called {
		t.Fatal("launcher should not be called in NonInteractive mode")
	}
}
