package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func titleFor(t *testing.T, workspace, payload string) (string, bool) {
	t.Helper()
	var out strings.Builder
	runAgentTitleHook(workspace, strings.NewReader(payload), &out)
	if strings.TrimSpace(out.String()) == "" {
		return "", false
	}
	var decoded titleHookOutput
	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil {
		t.Fatalf("the hook must print valid JSON, got %q: %v", out.String(), err)
	}
	if decoded.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
		t.Errorf("hookEventName = %q, want UserPromptSubmit", decoded.HookSpecificOutput.HookEventName)
	}
	return decoded.HookSpecificOutput.SessionTitle, true
}

func TestTitleHookNamesASessionAfterTheFirstAsk(t *testing.T) {
	got, ok := titleFor(t, "corgi", `{"prompt":"fix the login redirect loop","session_title":"corgi · main · 18:55"}`)
	if !ok {
		t.Fatal("a first ask on a corgi-named session must produce a title")
	}
	if got != "corgi · fix the login redirect loop" {
		t.Errorf("title = %q", got)
	}
}

func TestTitleHookLeavesAChosenNameAlone(t *testing.T) {
	// Somebody named this session, or the first prompt already did. Either way
	// a name that changes under you is worse than a dull one.
	for _, title := range []string{
		"corgi · fix the login redirect loop", // already titled by this hook
		"ship the release",                    // named from the phone
		"corgi · 18:55 and then some",         // a clock, but not at the end
		"unrelated",
	} {
		if got, ok := titleFor(t, "corgi", `{"prompt":"now add a test for it","session_title":`+quoteJSON(title)+`}`); ok {
			t.Errorf("title %q must be left alone, got %q", title, got)
		}
	}
}

func TestTitleHookAcceptsTheNamesNobodyChose(t *testing.T) {
	for _, title := range []string{
		"corgi",                     // the workspace id
		"corgi-3e",                  // Claude Code's derived name
		"corgi (work) · 09:02",      // corgi's composed name, under a profile
		"corgi · fix/login · 18:55", // and with a branch
		"",                          // nothing recorded yet
	} {
		if _, ok := titleFor(t, "corgi", `{"prompt":"rewrite the payment webhook","session_title":`+quoteJSON(title)+`}`); !ok {
			t.Errorf("an unchosen name (%q) is corgi's to replace", title)
		}
	}
}

func TestTitleHookIgnoresPromptsThatSayNothing(t *testing.T) {
	for _, prompt := range []string{
		"yes", "continue", "ok", "do it", "fix it.", "", "   ",
		"/corgi-remote",             // a slash command is a command, not a description
		"go",                        // too short to name anything
		"/review\n/another-command", // nothing but commands
	} {
		if got, ok := titleFor(t, "corgi", `{"prompt":`+quoteJSON(prompt)+`,"session_title":"corgi"}`); ok {
			t.Errorf("prompt %q must not rename the session, got %q", prompt, got)
		}
	}
}

func TestTitleHookTakesTheFirstRealLine(t *testing.T) {
	prompt := "/context\n\n  make the launcher show the branch  \nand then the tests"
	got, ok := titleFor(t, "corgi", `{"prompt":`+quoteJSON(prompt)+`,"session_title":"corgi"}`)
	if !ok {
		t.Fatal("a prompt with a leading command still describes work")
	}
	if got != "corgi · make the launcher show the branch" {
		t.Errorf("title = %q, want the first line that is not a command, whitespace collapsed", got)
	}
}

func TestTitleHookFitsTheListAndStaysSane(t *testing.T) {
	long := strings.Repeat("refactor the supervisor ", 12)
	got, ok := titleFor(t, "corgi", `{"prompt":`+quoteJSON(long)+`,"session_title":"corgi"}`)
	if !ok {
		t.Fatal("a long ask still names the session")
	}
	if n := len([]rune(got)); n > maxTitleLen {
		t.Errorf("title is %d runes (%q), want at most %d", n, got, maxTitleLen)
	}
	if !strings.HasPrefix(got, "corgi · ") {
		t.Errorf("title = %q, want the workspace kept in front", got)
	}

	// Whatever a prompt contains, a control character must not travel into a
	// name that other software will render.
	withControls := "drop the\ttab and the bell from this ask"
	got, _ = titleFor(t, "corgi", `{"prompt":`+quoteJSON(withControls)+`,"session_title":"corgi"}`)
	if strings.ContainsFunc(got, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		t.Errorf("title = %q, want control characters stripped", got)
	}
}

func TestTitleHookSurvivesJunk(t *testing.T) {
	// A hook that cannot read its input must cost the turn nothing.
	for _, payload := range []string{"", "not json", "{}", `{"prompt":123}`, "null"} {
		if _, ok := titleFor(t, "corgi", payload); ok {
			t.Errorf("payload %q must produce no output", payload)
		}
	}
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
