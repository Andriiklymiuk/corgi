package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
)

func TestWriteStayAwakeCreatesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := writeStayAwake(path, true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "stayAwake: true" {
		t.Fatalf("wrote %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The user config grants capability; corgi refuses to read a loose one.
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %v, want 0600", perm)
	}
}

// The file is hand-edited and commented — a rewrite must touch one line.
func TestWriteStayAwakeLeavesEverythingElseAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	original := "# my notes\nnotifyUrl: \"https://ntfy.sh/x\"\nstayAwake: true\nversion: 1\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeStayAwake(path, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	body := string(data)
	for _, want := range []string{"# my notes", "notifyUrl: \"https://ntfy.sh/x\"", "stayAwake: false", "version: 1"} {
		if !strings.Contains(body, want) {
			t.Errorf("lost %q from:\n%s", want, body)
		}
	}
	if strings.Contains(body, "stayAwake: true") {
		t.Errorf("old value survived:\n%s", body)
	}
}

func TestWriteStayAwakeAppendsToAnExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeStayAwake(path, true); err != nil {
		t.Fatal(err)
	}
	user, err := config.LoadUser(path)
	if err != nil {
		t.Fatal(err)
	}
	if !user.StayAwake {
		t.Fatal("the written line did not survive a parse")
	}
}

func TestParseOnOff(t *testing.T) {
	for _, arg := range []string{"on", "ON", "true", "yes"} {
		if v, err := parseOnOff(arg); err != nil || !v {
			t.Errorf("parseOnOff(%q) = %v, %v", arg, v, err)
		}
	}
	for _, arg := range []string{"off", "false", "no"} {
		if v, err := parseOnOff(arg); err != nil || v {
			t.Errorf("parseOnOff(%q) = %v, %v", arg, v, err)
		}
	}
	if _, err := parseOnOff("maybe"); err == nil {
		t.Error("a typo must not silently pick a side")
	}
}

// Off by default: no config, no wake lock, no caffeinate left running.
func TestDaemonWakeLockIsOffByDefault(t *testing.T) {
	if lock := daemonWakeLock(t.TempDir()); lock != nil {
		lock.Release()
		t.Fatal("held the machine awake without being asked to")
	}
}

func TestStayAwakeEnabledFollowsTheConfig(t *testing.T) {
	dir := t.TempDir()
	if stayAwakeEnabled(dir) {
		t.Fatal("reported on with no config")
	}
	if err := writeStayAwake(agentUserConfigPath(dir), true); err != nil {
		t.Fatal(err)
	}
	if !stayAwakeEnabled(dir) {
		t.Fatal("reported off after being turned on")
	}
}
