package utils

import (
	"strings"
	"testing"
)

func TestDockerStartCommand(t *testing.T) {
	cases := map[string]string{
		"darwin":  "open",
		"linux":   "systemctl",
		"windows": "cmd",
	}
	for goos, wantName := range cases {
		name, args := dockerStartCommand(goos)
		if name != wantName {
			t.Errorf("dockerStartCommand(%q) name = %q, want %q", goos, name, wantName)
		}
		if len(args) == 0 {
			t.Errorf("dockerStartCommand(%q) returned no args", goos)
		}
	}
}

func TestKillPortCommand_Unix(t *testing.T) {
	name, args := killPortCommand("darwin", 5432)
	if name != "lsof" {
		t.Fatalf("unix kill name = %q, want lsof", name)
	}
	if args[len(args)-1] != "-i:5432" {
		t.Fatalf("port not in args: %v", args)
	}
}

func TestKillPortCommand_Windows(t *testing.T) {
	name, args := killPortCommand("windows", 8080)
	if name != "cmd" {
		t.Fatalf("windows kill name = %q, want cmd", name)
	}
	if !strings.Contains(args[len(args)-1], ":8080") {
		t.Fatalf("port not in windows command: %v", args)
	}
}
