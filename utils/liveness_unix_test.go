//go:build !windows

package utils

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPidAliveCommandMatch(t *testing.T) {
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pid := cmd.Process.Pid

	if !PidAlive(pid, "") {
		t.Error("empty command should report alive (back-compat)")
	}
	if !PidAlive(pid, "sleep 30") {
		t.Error("matching command should report alive")
	}
	if PidAlive(pid, "some-other-binary-xyz") {
		t.Error("mismatched command must report dead (PID-reuse guard)")
	}
}

func TestCommandNeedle(t *testing.T) {
	cases := map[string]string{
		"npm run dev && echo done": "npm run dev",
		"short":                    "short",
		"  spaced  ":               "spaced",
	}
	for in, want := range cases {
		if got := commandNeedle(in); got != want {
			t.Errorf("commandNeedle(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 100)
	if got := commandNeedle(long); len(got) != 60 {
		t.Errorf("long command needle len = %d, want 60", len(got))
	}
}
