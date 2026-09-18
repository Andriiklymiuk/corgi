package utils

import (
	"strings"
	"testing"
)

func TestInstallHintIsPerOS(t *testing.T) {
	if got := installHintFor("darwin", "gh"); got != "brew install gh" {
		t.Fatalf("darwin: %q", got)
	}
	if got := installHintFor("linux", "gh"); strings.Contains(got, "brew") || got == "" {
		t.Fatalf("linux must not point at brew: %q", got)
	}
	if got := installHintFor("linux", "nothing-known"); got != "" {
		t.Fatalf("unknown tool: %q", got)
	}
}

func TestLinuxCommandInstructionsNeverRunBrew(t *testing.T) {
	for name := range CommandInstructions {
		if name == "brew" {
			continue
		}
		info, ok := commandInstructionsFor("linux", name)
		if !ok {
			t.Errorf("%s: no linux instructions", name)
			continue
		}
		if usesBrew(info.Install) {
			t.Errorf("%s: linux install runs brew: %q", name, info.Install)
		}
		if info.Install == "" && info.Hint == "" {
			t.Errorf("%s: neither an install nor a hint", name)
		}
	}
	if info, _ := commandInstructionsFor("darwin", "yarn"); !usesBrew(info.Install) {
		t.Fatalf("darwin keeps brew: %+v", info)
	}
}
