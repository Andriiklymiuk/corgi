package utils

import (
	"path/filepath"
	"testing"
	"time"
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

func TestReconcileMarksCrashed(t *testing.T) {
	s := RunState{Services: []RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Status: "running"},
		{Name: "web", Kind: "service", PID: 2, Status: "running"},
	}}
	alive := func(pid int, command string) bool { return pid == 1 }
	container := func(name string) string { return "" }
	out := ReconcileRunState(s, alive, container)
	if out.Services[0].Status != "running" {
		t.Errorf("api should stay running, got %q", out.Services[0].Status)
	}
	if out.Services[1].Status != "crashed" {
		t.Errorf("web should be crashed, got %q", out.Services[1].Status)
	}
	if out.Services[1].StatusChangedAt.IsZero() {
		t.Error("statusChangedAt should be set when status flips")
	}
}

func TestReconcileStableWhenUnchanged(t *testing.T) {
	t0 := time.Now().Add(-time.Hour).UTC()
	s := RunState{Services: []RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Status: "running", StatusChangedAt: t0},
	}}
	out := ReconcileRunState(s, func(int, string) bool { return true }, func(string) string { return "" })
	if !out.Services[0].StatusChangedAt.Equal(t0) {
		t.Error("statusChangedAt must not change when status is unchanged")
	}
}
