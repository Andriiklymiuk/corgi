package cmd

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// A tool's input is the one part of a hook payload corgi must not keep: a
// Bash command carries tokens, a Write carries the file. What a key can show
// instead is one safe word about it — the file's name, the program, the
// pattern — chosen here, in the hook, before anything is written anywhere.

// subjectMaxLen bounds the line a key shows.
const subjectMaxLen = 40

// safeToken is a command-line word that cannot be a secret or a flag value:
// letters, digits and path punctuation, short.
var safeToken = regexp.MustCompile(`^[A-Za-z0-9_./~+-]{1,24}$`)

// subcommandWord is a bare word after the program: letters and digits,
// no dash, dot or slash, so flags, paths and values never qualify.
var subcommandWord = regexp.MustCompile(`^[a-z][a-z0-9-]{0,15}$`)

// subjectOf reduces tool_input to the safe subject for a tool, or "".
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

// commandSubject keeps the program and up to two subcommand words after it
// — `git push origin`, `go test`, `npm run` — and nothing that could be a
// path, a flag or a value: `rm -rf /` is `rm`, `curl -H "Authorization: …"`
// is `curl`, `export TOKEN=x` is `export`.
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
