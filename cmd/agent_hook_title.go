package cmd

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode"
)

type titleHookOutput struct {
	HookSpecificOutput titleHookSpecific `json:"hookSpecificOutput"`
}

type titleHookSpecific struct {
	HookEventName string `json:"hookEventName"`
	SessionTitle  string `json:"sessionTitle"`
}

type titleHookInput struct {
	Prompt       string `json:"prompt"`
	SessionTitle string `json:"session_title"`
	Event        string `json:"hook_event_name"`
}

const maxTitleLen = 60

func runAgentTitleHook(workspaceID string, stdin io.Reader, stdout io.Writer) {
	title, ok := titleFromPrompt(workspaceID, stdin)
	if !ok {
		return
	}
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
		return "", false
	}
	prefix := strings.TrimSpace(workspaceID)
	if prefix == "" {
		return clipTitle(ask, maxTitleLen), true
	}
	prefix += " · "
	return prefix + clipTitle(ask, maxTitleLen-len([]rune(prefix))), true
}

func firstAsk(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "/") {
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
	rest, cut := strings.CutPrefix(title, id+"-")
	if !cut || rest == "" {
		return false
	}
	if len(rest) <= 4 && isShortSuffix(rest) {
		return true
	}
	return isGeneratedPair(rest)
}

func isGeneratedPair(s string) bool {
	words := strings.Split(s, "-")
	if len(words) != 2 {
		return false
	}
	for _, w := range words {
		if w == "" {
			return false
		}
		for _, r := range w {
			if !unicode.IsLower(r) {
				return false
			}
		}
	}
	return true
}

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

func clipTitle(s string, limit int) string {
	if limit < 8 {
		limit = 8
	}
	r := []rune(strings.TrimSpace(s))
	if len(r) <= limit {
		return string(r)
	}
	return strings.TrimSpace(string(r[:limit-1])) + "…"
}

var titleHookStdin io.Reader = os.Stdin
