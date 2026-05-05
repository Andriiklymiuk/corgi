package cmd

import (
	"reflect"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestParseRequestedServices(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]bool
	}{
		{"empty", nil, nil},
		{"single", []string{"api"}, map[string]bool{"api": true}},
		{"csv", []string{"api,worker"}, map[string]bool{"api": true, "worker": true}},
		{"trims spaces", []string{" api , worker "}, map[string]bool{"api": true, "worker": true}},
		{"drops empty", []string{"api,,worker"}, map[string]bool{"api": true, "worker": true}},
		{"multiple args + csv", []string{"a", "b,c"}, map[string]bool{"a": true, "b": true, "c": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRequestedServices(tt.args)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseRequestedServices(%v) = %v; want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestParseServicesFilter(t *testing.T) {
	tests := []struct {
		name string
		raw  []string
		want map[string]struct{}
	}{
		{"empty", nil, map[string]struct{}{}},
		{"single", []string{"api"}, map[string]struct{}{"api": {}}},
		{"csv", []string{"api,worker"}, map[string]struct{}{"api": {}, "worker": {}}},
		{"trims spaces", []string{" api , worker "}, map[string]struct{}{"api": {}, "worker": {}}},
		{"drops empty", []string{"api,,worker"}, map[string]struct{}{"api": {}, "worker": {}}},
		{"drops 'none'", []string{"api,none"}, map[string]struct{}{"api": {}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseServicesFilter(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseServicesFilter(%v) = %v; want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCollectScriptName(t *testing.T) {
	t.Run("empty name skipped", func(t *testing.T) {
		seen := map[string]struct{}{}
		_, ok := collectScriptName(utils.Script{Name: ""}, map[string]struct{}{}, seen)
		if ok {
			t.Errorf("expected empty name to be skipped")
		}
	})
	t.Run("already-completed entry skipped", func(t *testing.T) {
		already := map[string]struct{}{"build": {}}
		seen := map[string]struct{}{}
		_, ok := collectScriptName(utils.Script{Name: "build"}, already, seen)
		if ok {
			t.Errorf("expected already-completed name to be skipped")
		}
	})
	t.Run("dedup via seen", func(t *testing.T) {
		seen := map[string]struct{}{}
		name1, ok1 := collectScriptName(utils.Script{Name: "build"}, map[string]struct{}{}, seen)
		_, ok2 := collectScriptName(utils.Script{Name: "build"}, map[string]struct{}{}, seen)
		if !ok1 || name1 != "build" {
			t.Errorf("first call should return (build, true), got (%q, %v)", name1, ok1)
		}
		if ok2 {
			t.Errorf("second call should be skipped via seen map")
		}
	})
}

func TestUsesDocker(t *testing.T) {
	t.Run("UseDocker flag wins", func(t *testing.T) {
		c := &utils.CorgiCompose{UseDocker: true}
		if !usesDocker(c) {
			t.Error("expected true when UseDocker is set")
		}
	})
	t.Run("docker runner triggers", func(t *testing.T) {
		c := &utils.CorgiCompose{Services: []utils.Service{{Runner: utils.Runner{Name: "docker"}}}}
		if !usesDocker(c) {
			t.Error("expected true when a service has docker runner")
		}
	})
	t.Run("no docker", func(t *testing.T) {
		c := &utils.CorgiCompose{Services: []utils.Service{{Runner: utils.Runner{Name: "node"}}}}
		if usesDocker(c) {
			t.Error("expected false when nothing uses docker")
		}
	})
	t.Run("empty compose", func(t *testing.T) {
		if usesDocker(&utils.CorgiCompose{}) {
			t.Error("expected false for empty compose")
		}
	})
}
