package command

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteThenDrainRoundTrips(t *testing.T) {
	dir := t.TempDir()
	c, err := Write(dir, Command{Action: ActionStart, WorkspaceID: "acme", Profile: "work", Source: "mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.RequestedAt.IsZero() {
		t.Fatalf("Write must fill ID and RequestedAt, got %+v", c)
	}

	got, err := Drain(dir, time.Now(), TTL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].WorkspaceID != "acme" || got[0].Profile != "work" || got[0].Action != ActionStart {
		t.Fatalf("Drain = %+v", got)
	}
	if again, _ := Drain(dir, time.Now(), TTL); len(again) != 0 {
		t.Error("a drained command must not be seen twice")
	}
}

func TestDrainReturnsOldestFirst(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	if _, err := Write(dir, Command{Action: ActionStop, WorkspaceID: "second", RequestedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, Command{Action: ActionStart, WorkspaceID: "first", RequestedAt: now.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	got, err := Drain(dir, now, TTL)
	if err != nil || len(got) != 2 {
		t.Fatalf("Drain = %+v, %v", got, err)
	}
	if got[0].WorkspaceID != "first" {
		t.Errorf("oldest command must come first, got %s", got[0].WorkspaceID)
	}
}

func TestDrainDeletesStaleCommandsUnexecuted(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * TTL)
	if _, err := Write(dir, Command{Action: ActionStart, WorkspaceID: "acme", RequestedAt: old}); err != nil {
		t.Fatal(err)
	}
	got, err := Drain(dir, time.Now(), TTL)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Error("a command older than the TTL must never execute")
	}
	entries, _ := os.ReadDir(Dir(dir))
	if len(entries) != 0 {
		t.Error("a stale command must be deleted, not kept for a second look")
	}
}

func TestDrainDeletesCorruptFilesAndIgnoresTmp(t *testing.T) {
	dir := t.TempDir()
	spool := Dir(dir)
	if err := os.MkdirAll(spool, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spool, "bad.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spool, "half.json.tmp"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Drain(dir, time.Now(), TTL)
	if err != nil || len(got) != 0 {
		t.Fatalf("Drain = %+v, %v", got, err)
	}
	entries, _ := os.ReadDir(spool)
	for _, e := range entries {
		if e.Name() == "bad.json" {
			t.Error("a corrupt command must be deleted")
		}
	}
}

func TestDrainMissingDirIsEmptyNotError(t *testing.T) {
	got, err := Drain(t.TempDir(), time.Now(), TTL)
	if err != nil || got != nil {
		t.Fatalf("Drain on a fresh dir = %+v, %v; want empty", got, err)
	}
}

func TestWriteRejectsBadInput(t *testing.T) {
	if _, err := Write(t.TempDir(), Command{Action: "reboot", WorkspaceID: "acme"}); err == nil {
		t.Error("unknown action must be rejected")
	}
	if _, err := Write(t.TempDir(), Command{Action: ActionStart}); err == nil {
		t.Error("missing workspaceId must be rejected")
	}
}
