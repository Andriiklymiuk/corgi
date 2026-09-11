package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The daemon's binary path must not change between updates, or macOS asks
// for Documents access again at the next login. So it is a copy, refreshed
// only when the bytes differ, swapped in by rename under a running daemon.
func TestDaemonRunsFromAStableCopy(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CORGI_DATA_DIR", data)
	src := filepath.Join(t.TempDir(), "corgi")
	if err := os.WriteFile(src, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "corgi")
	if err := os.Symlink(src, link); err != nil {
		t.Fatal(err)
	}

	dest, err := refreshStableDaemonBinary(link)
	if err != nil {
		t.Fatal(err)
	}
	if dest != filepath.Join(data, "bin", "corgi") {
		t.Fatalf("the copy lives under the data dir, got %s", dest)
	}
	if got, _ := os.ReadFile(dest); string(got) != "v1" {
		t.Fatalf("copy has %q", got)
	}
	if info, _ := os.Stat(dest); info.Mode()&0o111 == 0 {
		t.Fatal("the copy has to be executable")
	}
	if info, _ := os.Lstat(dest); info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("a symlink would resolve to the versioned path again")
	}

	first, _ := os.Stat(dest)
	if _, err := refreshStableDaemonBinary(link); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.Stat(dest); !again.ModTime().Equal(first.ModTime()) {
		t.Fatal("an unchanged binary is not rewritten")
	}

	if err := os.WriteFile(src, []byte("v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := refreshStableDaemonBinary(link); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "v2" {
		t.Fatal("an update refreshes the copy")
	}
}

func TestProgramFromServiceFile(t *testing.T) {
	plist := renderedLaunchdPlist("/Users/me/Library/Application Support/corgi/bin/corgi", "/o", "/e", nil)
	if got := programFromServiceFile(plist); got != "/Users/me/Library/Application Support/corgi/bin/corgi" {
		t.Fatalf("plist program: %q", got)
	}
	unit := renderedSystemdUnit("/usr/local/bin/corgi", nil)
	if got := programFromServiceFile(unit); got != "/usr/local/bin/corgi" {
		t.Fatalf("unit program: %q", got)
	}
	if !strings.Contains(plist, "bin/corgi") {
		t.Fatal("sanity")
	}
}
