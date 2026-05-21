package utils

import (
	"path/filepath"
	"testing"
)

func TestRunStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".state.json")
	in := RunState{
		ComposePath: "/x/corgi-compose.yml",
		Services: []RunStateEntry{
			{Name: "api", Kind: "service", PID: 1234, Port: 8080, Status: "running"},
		},
	}
	if err := WriteRunState(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadRunState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.Services) != 1 || got.Services[0].Name != "api" || got.Services[0].PID != 1234 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestReadRunStateMissingFile(t *testing.T) {
	_, err := ReadRunState(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Error("expected error for missing state file")
	}
}
