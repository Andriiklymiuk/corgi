package main

import "testing"

func TestShouldPromptToContinueSkippedNonInteractive(t *testing.T) {
	if shouldPromptToContinue(true) {
		t.Errorf("must not prompt to continue when non-interactive")
	}
	if !shouldPromptToContinue(false) {
		t.Errorf("interactive mode should allow the prompt")
	}
}
