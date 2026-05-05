package utils

import (
	"os/exec"
	"testing"
)

func TestWithBackStringAtTheEnd(t *testing.T) {
	opts := &PickPromptOptions{}
	WithBackStringAtTheEnd()(opts)
	if !opts.backStringAtTheEnd {
		t.Error("expected true")
	}
}

func TestSetProcessGroup(t *testing.T) {
	cmd := exec.Command("echo", "hi")
	SetProcessGroup(cmd)
	if cmd.SysProcAttr == nil {
		t.Error("nil SysProcAttr")
	}
}
