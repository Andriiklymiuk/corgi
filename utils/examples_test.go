package utils

import (
	"testing"
)

func TestExtractExamplePaths(t *testing.T) {
	got := ExtractExamplePaths(ExampleProjects)
	if len(got) != len(ExampleProjects) {
		t.Errorf("got %d, want %d", len(got), len(ExampleProjects))
	}
	for _, p := range got {
		if p == "" {
			t.Error("empty path in output")
		}
	}
}

func TestExtractExamplePathsSkipsEmpty(t *testing.T) {
	mixed := []CorgiExample{
		{Path: "a"},
		{Path: ""},
		{Path: "b"},
	}
	got := ExtractExamplePaths(mixed)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v", got)
	}
}

func TestFindExampleByPathFound(t *testing.T) {
	got := FindExampleByPath(ExampleProjects, "echo_example_with_postgres_databases")
	if got == nil {
		t.Fatal("nil")
	}
	if got.Title == "" {
		t.Error("empty title")
	}
}

func TestFindExampleByPathMissing(t *testing.T) {
	got := FindExampleByPath(ExampleProjects, "no-such-example")
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}
