package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadWindowsDropsDeadAndBroken(t *testing.T) {
	dir := t.TempDir()
	wdir := WindowsDir(dir)
	if err := os.MkdirAll(wdir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, w any) {
		data, _ := json.Marshal(w)
		if err := os.WriteFile(filepath.Join(wdir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("live.json", Window{ID: "live", ExtHostPID: 10, Folders: []string{"/a"}})
	write("dead.json", Window{ID: "dead", ExtHostPID: 20})
	write("noid.json", map[string]any{"extHostPid": 30})
	_ = os.WriteFile(filepath.Join(wdir, "junk.json"), []byte("{"), 0o600)
	_ = os.WriteFile(filepath.Join(wdir, "notes.txt"), []byte("x"), 0o600)

	got := LoadWindows(dir, func(pid int) bool { return pid == 10 })
	if len(got) != 1 || got[0].ID != "live" || got[0].UpdatedAt.IsZero() {
		t.Fatalf("windows = %+v", got)
	}
	for _, gone := range []string{"dead.json", "noid.json", "junk.json"} {
		if _, err := os.Stat(filepath.Join(wdir, gone)); err == nil {
			t.Errorf("%s should have been removed", gone)
		}
	}
	if LoadWindows(t.TempDir(), nil) != nil {
		t.Fatal("no windows dir is no windows")
	}
}

func TestWriteReveal(t *testing.T) {
	dir := t.TempDir()
	if err := WriteReveal(dir, Reveal{}); err != nil {
		t.Fatal("an empty window id is a no-op")
	}
	if err := WriteReveal(dir, Reveal{WindowID: "win/1 x", SessionID: "s", ShellPID: 5}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(RevealDir(dir), "win_1_x.json"))
	if err != nil {
		t.Fatal(err)
	}
	var req Reveal
	if json.Unmarshal(data, &req) != nil || req.ShellPID != 5 || req.RequestedAt.IsZero() {
		t.Fatalf("reveal = %+v", req)
	}
	if time.Since(req.RequestedAt) > time.Minute {
		t.Fatal("stamped now")
	}
}
