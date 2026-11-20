package helpers

import (
	"os/exec"
	"strings"
	"testing"
)

func TestEveryHelperHasItsSource(t *testing.T) {
	for _, name := range Names() {
		source, err := Source(name)
		if err != nil || len(source) == 0 {
			t.Fatalf("%s: %v", name, err)
		}
		if Describe(name) == "" {
			t.Fatalf("%s has no description", name)
		}
	}
	if _, err := Source("../../etc/passwd"); err == nil {
		t.Fatal("an unknown name read a file")
	}
}

func TestRunPassesArgumentsAndTheExitCode(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3")
	}
	code, err := Run("mine-sessions", []string{"--help"})
	if err != nil || code != 0 {
		t.Fatalf("--help: code %d, err %v", code, err)
	}
	code, err = Run("mine-sessions", []string{"--no-such-flag"})
	if err != nil || code == 0 {
		t.Fatalf("a bad flag: code %d, err %v", code, err)
	}
	if code, err := Run("nope", nil); err == nil || code != 2 || !strings.Contains(err.Error(), "mine-sessions") {
		t.Fatalf("unknown helper: code %d, err %v", code, err)
	}
}
