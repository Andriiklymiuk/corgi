package cmd

import (
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
)

const (
	maxSessionNameLen = 60
	sessionNameSep    = " · "
	minBranchRoom     = 8
)

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

func sessionNamePrefix(id, profile string) string {
	parts := []string{strings.TrimSpace(id)}
	if p := strings.TrimSpace(profile); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, "-")
}

func clipSessionName(name string) string {
	r := []rune(name)
	if len(r) <= maxSessionNameLen {
		return name
	}
	return strings.TrimSpace(string(r[:maxSessionNameLen]))
}

func runeLen(s string) int { return len([]rune(s)) }
