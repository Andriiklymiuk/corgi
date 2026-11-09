package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSkillsAndDocsNameOnlyRealCommandsAndFlags(t *testing.T) {
	files, err := markdownFiles([]string{"../docs", "../plugins", "../README.md"})
	if err != nil || len(files) < 20 {
		t.Fatalf("found %d markdown files: %v", len(files), err)
	}
	for _, f := range checkCommandMentions(files) {
		t.Errorf("%s:%d %s - %s %s (corgi docs commands)", f.File, f.Line, f.Mention, f.Problem, f.Fix)
	}
}

func TestCheckInvocation(t *testing.T) {
	bad := map[string]string{
		"corgi runn --detach":         "did you mean corgi run?",
		"corgi run --detatch":         "did you mean --detach?",
		"corgi logs --errors-only":    "",
		"corgi agent wach":            "did you mean corgi agent watch?",
		"corgi --json status --wach":  "did you mean --watch?",
		"corgi validate --strict --x": "",
	}
	for line, fix := range bad {
		f, found := checkInvocation(shellWords(strings.TrimPrefix(line, "corgi ")))
		if !found {
			t.Errorf("%q: expected a finding", line)
			continue
		}
		if fix != "" && f.Fix != fix {
			t.Errorf("%q: fix %q, want %q", line, f.Fix, fix)
		}
	}
	good := []string{
		"corgi run --detach --services api,web",
		"corgi --json run --detach",
		"corgi --isolate run-7 db -u",
		"corgi --version",
		`corgi feedback note "it took --three tries" --command "run --detach"`,
		"corgi exec api -- npm test --watch",
		"corgi agent running (pid 84639, version 2.29.0)",
		"corgi watch · workspace · pr 12",
		"corgi <command> --help",
		"corgi start",
	}
	for _, line := range good {
		if f, found := checkInvocation(shellWords(strings.TrimPrefix(line, "corgi "))); found {
			t.Errorf("%q: unexpected finding %+v", line, f)
		}
	}
}

func TestOnlyShellFencesAndInlineCodeAreRead(t *testing.T) {
	dir := t.TempDir()
	md := "Prose: corgi runn here is not code.\n" +
		"```text\ncorgi runn in a diagram\n```\n" +
		"```yaml\n# corgi runn\n```\n" +
		"```bash\ncorgi run --detatch   # a typo\n```\n" +
		"Inline `corgi stauts` too.\n"
	path := filepath.Join(dir, "x.md")
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	got := checkCommandMentions([]string{path})
	if len(got) != 2 || got[0].Line != 9 || got[1].Line != 11 {
		t.Fatalf("got %+v", got)
	}
}

func TestSkillsAndDocsNameOnlyRealMCPTools(t *testing.T) {
	notTools := map[string]bool{"corgi_services": true}
	files, _ := markdownFiles([]string{"../docs", "../plugins", "../README.md"})
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, m := range mcpToolMention.FindAllStringSubmatch(string(b), -1) {
			name := m[1]
			if _, ok := mcpToolMeta[name]; !ok && !notTools[name] {
				t.Errorf("%s names MCP tool %s, which corgi mcp does not serve", file, name)
			}
		}
	}
}

var mcpToolMention = regexp.MustCompile("`(corgi_[a-z_]+)`")
