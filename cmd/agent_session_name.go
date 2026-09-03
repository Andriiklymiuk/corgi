package cmd

import (
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
)

const (
	// maxSessionNameLen is sanitizeSessionName's cap, applied to the names
	// corgi composes as well: past it, claude.ai's session list truncates and
	// the tail is lost anyway.
	maxSessionNameLen = 60
	// sessionNameSep reads as one label on a phone, where the list is narrow.
	sessionNameSep = " · "
	// minBranchRoom is the shortest branch fragment still worth showing. Below
	// it the branch is dropped instead: "fix/l…" identifies nothing.
	minBranchRoom = 8
)

// defaultSessionName is what a session corgi started itself is called in
// claude.ai/code's list when nobody named it.
//
// The workspace id alone made every session in a workspace identical — a list
// of four rows all called "corgi" says nothing about which one to open — so
// the branch being worked on and the clock time the session started come with
// it:
//
//	corgi · fix/login-redirect · 18:55
//	corgi (work) · main · 09:02
//
// The branch is dropped when the workspace is not a git checkout or sits on a
// detached HEAD, and shortened before either of the other two parts: the id
// says which workspace and the clock says which run, and a name that loses
// either stops identifying anything.
func defaultSessionName(id, dir, profile string, now time.Time) string {
	head := strings.TrimSpace(id)
	if p := strings.TrimSpace(profile); p != "" {
		head += " (" + p + ")"
	}
	clock := now.Format("15:04")
	short := clipSessionName(head + sessionNameSep + clock)

	branch := utils.CurrentBranch(dir)
	if branch == "" {
		return short
	}
	room := maxSessionNameLen - runeLen(head) - runeLen(clock) - 2*runeLen(sessionNameSep)
	if room < minBranchRoom {
		return short
	}
	if runeLen(branch) > room {
		branch = string([]rune(branch)[:room-1]) + "…"
	}
	return head + sessionNameSep + branch + sessionNameSep + clock
}

// clipSessionName trims by rune, never by byte: cutting a multi-byte character
// in half would put an invalid UTF-8 sequence in the argv.
func clipSessionName(name string) string {
	r := []rune(name)
	if len(r) <= maxSessionNameLen {
		return name
	}
	return strings.TrimSpace(string(r[:maxSessionNameLen]))
}

func runeLen(s string) int { return len([]rune(s)) }
