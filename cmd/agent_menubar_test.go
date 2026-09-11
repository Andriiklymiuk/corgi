package cmd

import (
	"strings"
	"testing"
)

// `agent up --at-login` opens the menu bar once when it is installed and not
// running — its first run registers the login item — and stays quiet about
// an app that is not installed.
func TestAtLoginOpensTheMenuBarOnceWhenInstalled(t *testing.T) {
	origLook, origOpen := lookForMenuBar, openMenuBar
	defer func() { lookForMenuBar, openMenuBar = origLook, origOpen }()
	opened := 0
	openMenuBar = func() error { opened++; return nil }

	var said []string
	say := func(s string) { said = append(said, s) }

	lookForMenuBar = func() (bool, bool) { return false, false }
	startMenuBarIfInstalled(say)
	lookForMenuBar = func() (bool, bool) { return true, true }
	startMenuBarIfInstalled(say)
	if opened != 0 || len(said) != 0 {
		t.Fatalf("not installed or already running: nothing to do, got opened=%d said=%v", opened, said)
	}

	lookForMenuBar = func() (bool, bool) { return true, false }
	startMenuBarIfInstalled(say)
	if opened != 1 || len(said) != 1 || !strings.Contains(said[0], "login") {
		t.Fatalf("installed and down: open it and say so, got opened=%d said=%v", opened, said)
	}
}
