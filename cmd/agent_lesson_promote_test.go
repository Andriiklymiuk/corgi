package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/lessons"
)

func TestPromoteWritesALessonIntoClaudeMdOnce(t *testing.T) {
	agent, repo := t.TempDir(), t.TempDir()
	_ = lessons.Add(agent, "api", lessons.Lesson{At: time.Now(), Source: "review acme/api#7 (dan)", Text: "never log the token"})
	_ = lessons.Add(agent, "api", lessons.Lesson{At: time.Now(), Source: "done-when", Text: "run go vet before saying done"})

	if _, err := promoteLesson(agent, "api", repo, 3); err == nil || !strings.Contains(err.Error(), "2 lessons") {
		t.Fatalf("out of range: %v", err)
	}
	line, err := promoteLesson(agent, "api", repo, 2)
	if err != nil {
		t.Fatal(err)
	}
	if line != "- run go vet before saying done" {
		t.Fatalf("line %q", line)
	}
	data, _ := os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if !strings.Contains(string(data), "## Lessons\n") || !strings.Contains(string(data), "- run go vet before saying done\n") {
		t.Fatalf("CLAUDE.md:\n%s", data)
	}
	if _, err := promoteLesson(agent, "api", repo, 2); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if strings.Count(string(data), "run go vet") != 1 {
		t.Fatalf("written twice:\n%s", data)
	}
	_ = os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# api\n\nUse make test.\n"), 0o644)
	if _, err := promoteLesson(agent, "api", repo, 1); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if !strings.HasPrefix(string(data), "# api\n\nUse make test.\n") || !strings.Contains(string(data), "\n## Lessons\n\n- never log the token\n") {
		t.Fatalf("CLAUDE.md:\n%s", data)
	}
}
