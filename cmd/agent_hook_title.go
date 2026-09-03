package cmd

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode"
)

// The session title corgi composes at start — "corgi · fix/login · 18:55" —
// answers "which machine, which checkout, when". It cannot answer the question
// you actually scan the list for, which is what the session is *doing*, because
// nothing knows that until you say it.
//
// Claude Code takes a title from a UserPromptSubmit hook, so the first thing
// you ask becomes the name: "corgi · fix the login redirect". After that the
// title is left alone — a session renamed on every prompt would flicker
// through "ok", "continue", "now the tests", and a name that changes under you
// is worse than a dull one.
//
// This is deliberately conservative. The field is in the CLI's own hook schema
// but not in the published hooks reference, so a CLI that ignores it must lose
// nothing: the hook prints a title and exits 0, never blocks a prompt, and
// never fails a turn.

// titleHookOutput is the shape Claude Code reads back from the hook.
type titleHookOutput struct {
	HookSpecificOutput titleHookSpecific `json:"hookSpecificOutput"`
}

type titleHookSpecific struct {
	HookEventName string `json:"hookEventName"`
	SessionTitle  string `json:"sessionTitle"`
}

// titleHookInput is the part of the hook payload this reads. Everything else
// in it — the transcript path, the session id, the cwd — is deliberately
// ignored: the less of a prompt this touches, the less there is to leak.
type titleHookInput struct {
	Prompt       string `json:"prompt"`
	SessionTitle string `json:"session_title"`
	Event        string `json:"hook_event_name"`
}

// maxTitleLen matches the cap on a name typed from the phone, which is what
// claude.ai's list has room for.
const maxTitleLen = 60

func runAgentTitleHook(workspaceID string, stdin io.Reader, stdout io.Writer) {
	title, ok := titleFromPrompt(workspaceID, stdin)
	if !ok {
		return
	}
	// A hook that cannot write its answer has still not harmed the turn.
	_ = json.NewEncoder(stdout).Encode(titleHookOutput{
		HookSpecificOutput: titleHookSpecific{
			HookEventName: "UserPromptSubmit",
			SessionTitle:  title,
		},
	})
}

func titleFromPrompt(workspaceID string, stdin io.Reader) (string, bool) {
	if stdin == nil {
		return "", false
	}
	data, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		return "", false
	}
	var in titleHookInput
	if json.Unmarshal(data, &in) != nil {
		return "", false
	}
	ask := firstAsk(in.Prompt)
	if ask == "" {
		return "", false
	}
	if !titleIsStillCorgis(in.SessionTitle, workspaceID) {
		// Somebody named this session, or the first prompt already named it.
		// Either way it is not corgi's to overwrite.
		return "", false
	}
	prefix := strings.TrimSpace(workspaceID)
	if prefix == "" {
		return clipTitle(ask, maxTitleLen), true
	}
	// The workspace stays in front: claude.ai lists every session you have
	// running, on every machine, and "fix the login redirect" alone does not
	// say where. corgi's own list trims the prefix back off, since the card it
	// sits on has already said which repo this is.
	prefix += " · "
	return prefix + clipTitle(ask, maxTitleLen-len([]rune(prefix))), true
}

// firstAsk reduces a prompt to the one line worth putting in a list: the first
// non-empty line, without a leading slash command, control characters or
// runaway whitespace. An empty result means "nothing worth renaming for".
func firstAsk(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "/") {
			// A slash command is corgi's cue that the turn is a command, not a
			// description of the work.
			continue
		}
		line = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, line)
		line = strings.Join(strings.Fields(line), " ")
		if !worthTitling(line) {
			continue
		}
		return line
	}
	return ""
}

// continuations are the prompts that carry a turn forward without describing
// it. Naming a session "yes" helps nobody.
var continuations = map[string]bool{
	"yes": true, "y": true, "no": true, "n": true, "ok": true, "okay": true,
	"go": true, "go on": true, "continue": true, "carry on": true, "keep going": true,
	"next": true, "do it": true, "fix it": true, "try again": true, "retry": true,
	"stop": true, "thanks": true, "ty": true, "please": true, "run it": true,
}

func worthTitling(line string) bool {
	if len([]rune(line)) < 12 {
		return false
	}
	return !continuations[strings.ToLower(strings.TrimRight(line, ".!?"))]
}

// titleIsStillCorgis says whether the current title is one nobody chose: the
// name corgi started the session with, or the one Claude Code derives from the
// directory (workspace-3e). Anything else — a name typed from the phone, or
// the one this hook already wrote — was a decision, and is left alone.
//
// The tell is the clock: every name corgi composes ends in one
// (workspace · branch · 18:55), and no title made from an ask does. Matching
// the prefix alone would make "corgi · fix the login redirect" look like
// corgi's own name, and the session would be renamed on every prompt.
func titleIsStillCorgis(title, workspaceID string) bool {
	title = strings.TrimSpace(title)
	id := strings.TrimSpace(workspaceID)
	if title == "" {
		return true
	}
	if id == "" {
		return false
	}
	if title == id {
		return true
	}
	if strings.HasPrefix(title, id+" · ") || strings.HasPrefix(title, id+" (") {
		return endsWithClock(title)
	}
	// Claude Code's own derived name: the directory with a short suffix.
	rest, cut := strings.CutPrefix(title, id+"-")
	return cut && rest != "" && len(rest) <= 4 && isShortSuffix(rest)
}

// endsWithClock matches the HH:MM every composed name ends with.
func endsWithClock(title string) bool {
	r := []rune(title)
	if len(r) < 5 {
		return false
	}
	tail := r[len(r)-5:]
	return unicode.IsDigit(tail[0]) && unicode.IsDigit(tail[1]) &&
		tail[2] == ':' && unicode.IsDigit(tail[3]) && unicode.IsDigit(tail[4])
}

func isShortSuffix(s string) bool {
	for _, r := range s {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func clipTitle(s string, max int) string {
	if max < 8 {
		max = 8
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) <= max {
		return string(r)
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// titleHookStdin is a seam for the tests: the real hook reads the payload
// Claude Code pipes in.
var titleHookStdin io.Reader = os.Stdin
