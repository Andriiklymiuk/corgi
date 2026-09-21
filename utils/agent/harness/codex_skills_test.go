package harness

import (
	"strings"
	"testing"
)

func TestCodexPromptNamesTheSkillDirectory(t *testing.T) {
	got := codexSkillPrompt("I approve all changes; ship them. /corgi:stories ABC-1 ABC-2")
	if !strings.Contains(got, "$stories ABC-1 ABC-2") || strings.Contains(got, "/corgi:") || !strings.Contains(got, "skills/stories/SKILL.md") {
		t.Fatalf("codex has no slash commands; the skill is a $mention plus its file: %q", got)
	}
	if got := codexSkillPrompt("no skill here"); got != "no skill here" {
		t.Fatalf("untouched without a slash command: %q", got)
	}
}
