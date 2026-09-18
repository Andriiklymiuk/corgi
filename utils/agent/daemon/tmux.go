package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"andriiklymiuk/corgi/utils/agent/sessions"
)

// tmuxSendArgs turns text into one tmux invocation: literal runs go through
// `send-keys -l`, Return and Escape go as key names, chained with ";".
func tmuxSendArgs(pane, text string, enter bool) []string {
	var args []string
	var literal strings.Builder
	flush := func() {
		if literal.Len() == 0 {
			return
		}
		args = append(args, sep(args)...)
		args = append(args, "send-keys", "-t", pane, "-l", literal.String())
		literal.Reset()
	}
	key := func(name string) {
		flush()
		args = append(args, sep(args)...)
		args = append(args, "send-keys", "-t", pane, name)
	}
	for _, r := range text {
		switch r {
		case '\r', '\n':
			key("Enter")
		case 0x1b:
			key("Escape")
		default:
			literal.WriteRune(r)
		}
	}
	flush()
	if enter {
		key("Enter")
	}
	return args
}

func sep(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	return []string{";"}
}

func tmuxFocusArgs(pane string) []string {
	return []string{"select-window", "-t", pane, ";", "select-pane", "-t", pane}
}

func typeIntoTmux(ctx context.Context, t sessions.FocusTarget, text string, enter bool) error {
	if t.Pane == "" {
		return fmt.Errorf("%s: no tmux pane to type into", t.Label)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("%s: tmux is not on the daemon's PATH", t.Label)
	}
	return run(ctx, "tmux", tmuxSendArgs(t.Pane, text, enter)...)
}

func raiseTmuxPane(ctx context.Context, t sessions.FocusTarget) error {
	if t.Pane == "" {
		return fmt.Errorf("%s: no tmux pane to select", t.Label)
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("%s: tmux is not on the daemon's PATH", t.Label)
	}
	return run(ctx, "tmux", tmuxFocusArgs(t.Pane)...)
}
