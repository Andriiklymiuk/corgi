package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestScriptInstallDirs_IncludesUsrLocalBin(t *testing.T) {
	dirs := scriptInstallDirs()
	found := false
	for _, d := range dirs {
		if d == "/usr/local/bin" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /usr/local/bin in %v", dirs)
	}
}

func TestWindowsInstallDirs_UsesLocalAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join(os.TempDir(), "appdata"))
	dirs := windowsInstallDirs()
	want := filepath.Join(os.TempDir(), "appdata", "corgi", "bin")
	found := false
	for _, d := range dirs {
		if d == want {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %q in %v", want, dirs)
	}
}

func TestPathsEqual(t *testing.T) {
	dir := t.TempDir()
	if !pathsEqual(dir, dir) {
		t.Error("identical paths must be equal")
	}
	if !pathsEqual(dir+"/", dir) {
		t.Error("trailing slash must not matter")
	}
	if pathsEqual(dir, filepath.Join(dir, "sub")) {
		t.Error("different paths must not be equal")
	}
}

func TestDetectInstallMethod_UnknownDir(t *testing.T) {
	// A throwaway temp dir is neither the brew prefix nor a known script dir,
	// so detection falls through to unknown (on non-Windows hosts).
	if runtime.GOOS == "windows" {
		t.Skip("script-dir detection path is non-Windows")
	}
	if got := detectInstallMethod(t.TempDir()); got != installMethodUnknown {
		t.Errorf("expected installMethodUnknown for temp dir, got %v", got)
	}
}
