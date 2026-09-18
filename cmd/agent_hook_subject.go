package cmd

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const subjectMaxLen = 40

var safeToken = regexp.MustCompile(`^[A-Za-z0-9_./~+-]{1,24}$`)

var subcommandWord = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)

func subjectOf(tool string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var in map[string]json.RawMessage
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	str := func(key string) string {
		var s string
		_ = json.Unmarshal(in[key], &s)
		return strings.TrimSpace(s)
	}
	var subject string
	switch tool {
	case "Read", "Edit", "Write", "MultiEdit", "NotebookEdit":
		subject = filepath.Base(firstNonEmpty(str("file_path"), str("notebook_path")))
	case "Bash":
		subject = commandSubject(str("command"))
	case "Grep", "Glob":
		subject = str("pattern")
	case "Task", "Agent":
		subject = firstNonEmpty(str("description"), str("subagent_type"))
	case "WebFetch", "WebSearch":
		if u, err := url.Parse(str("url")); err == nil && u.Host != "" {
			subject = u.Host
		} else {
			subject = str("query")
		}
	case "Skill":
		subject = str("skill")
	default:
		return ""
	}
	subject = strings.TrimSpace(strings.SplitN(subject, "\n", 2)[0])
	if subject == "." || subject == "/" {
		return ""
	}
	if r := []rune(subject); len(r) > subjectMaxLen {
		subject = strings.TrimSpace(string(r[:subjectMaxLen-1])) + "…"
	}
	return subject
}

func commandSubject(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 || !safeToken.MatchString(fields[0]) {
		return ""
	}
	kept := []string{filepath.Base(fields[0])}
	for _, f := range fields[1:] {
		if !subcommandWord.MatchString(f) || len(kept) == 3 {
			break
		}
		kept = append(kept, f)
	}
	return strings.Join(kept, " ")
}

func riskOf(tool string, input json.RawMessage) string {
	switch tool {
	case "Read", "Grep", "Glob", "WebFetch", "WebSearch", "Task", "Agent", "Skill", "TodoWrite":
		return "reads"
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return "writes"
	case "Bash":
		var in struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(input, &in)
		if destructiveCommand.MatchString(in.Command) {
			return "destructive"
		}
		return "writes"
	}
	return ""
}

var destructiveCommand = regexp.MustCompile(`(?i)(^|[\s;&|])(rm|sudo|mkfs|dd|shutdown|reboot|kill|pkill|killall|chmod|chown|launchctl|diskutil|git\s+push\s+[^\n]*--force|git\s+reset\s+--hard|git\s+clean)(\s|$)|--force\b|--hard\b|--no-verify\b|\bdrop\s+(table|database|schema)\b|\btruncate\b|\bpurge\b|\brm\s+-rf\b`)
