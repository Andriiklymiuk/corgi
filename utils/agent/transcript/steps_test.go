package transcript

import (
	"testing"
)

func TestStepsGroupToolsUnderTheTextThatExplainsThem(t *testing.T) {
	entries := []Entry{
		{ID: "1", Kind: "user", Text: "add a flag"},
		{ID: "2", Kind: "assistant", Text: "Reading the command first.\nThen edit."},
		{ID: "3", Kind: "tool", Tool: "Read", Subject: "cmd/a.go"},
		{ID: "4", Kind: "result", Text: "..."},
		{ID: "5", Kind: "tool", Tool: "Edit", Subject: "cmd/a.go"},
		{ID: "6", Kind: "tool", Tool: "Edit", Subject: "cmd/a.go"},
		{ID: "7", Kind: "tool", Tool: "Write", Subject: "cmd/a_test.go"},
		{ID: "8", Kind: "assistant", Text: "Done."},
	}
	steps := Steps(entries)
	if len(steps) != 3 {
		t.Fatalf("want 3 steps, got %d: %+v", len(steps), steps)
	}
	if steps[0].Kind != "user" || len(steps[0].Tools) != 0 {
		t.Fatalf("user step: %+v", steps[0])
	}
	s := steps[1]
	if s.Text != "Reading the command first." || len(s.Tools) != 4 {
		t.Fatalf("assistant step: %+v", s)
	}
	if len(s.Files) != 2 || s.Files[0] != "cmd/a.go" || s.Files[1] != "cmd/a_test.go" {
		t.Fatalf("files deduped in order: %v", s.Files)
	}
	if steps[2].Text != "Done." || len(steps[2].Tools) != 0 {
		t.Fatalf("last step: %+v", steps[2])
	}
}

func TestStepsOpenAStepForToolsWithNoText(t *testing.T) {
	steps := Steps([]Entry{{ID: "1", Kind: "user", Text: "go"}, {ID: "2", Kind: "tool", Tool: "Bash", Subject: "go test"}})
	if len(steps) != 2 || steps[1].Kind != "assistant" || steps[1].ID != "2" {
		t.Fatalf("%+v", steps)
	}
}

func TestStepsCapTheToolList(t *testing.T) {
	entries := []Entry{{Kind: "assistant", Text: "x"}}
	for i := 0; i < maxStepTools+3; i++ {
		entries = append(entries, Entry{Kind: "tool", Tool: "Read", Subject: "f"})
	}
	s := Steps(entries)[0]
	if len(s.Tools) != maxStepTools+1 || s.Tools[maxStepTools] != "+3 more" {
		t.Fatalf("%v", s.Tools)
	}
}
