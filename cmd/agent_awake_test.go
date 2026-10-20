package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %v, want 0600", perm)
	}
}

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

func TestTheAwakePolicyIsOffByDefaultAndFollowsForAndOn(t *testing.T) {
	dir := t.TempDir()
	policy := awakePolicy(dir)
	if hold, _ := policy(); hold {
		t.Fatal("held the machine awake without being asked to")
	}
	path := agentUserConfigPath(dir)
	until := time.Now().Add(time.Hour).Format(time.RFC3339)
	if err := writeUserConfigLine(path, awakeUntilLine, "awakeUntil: "+until); err != nil {
		t.Fatal(err)
	}
	if hold, _ := policy(); !hold {
		t.Fatal("--for holds until then, no restart")
	}
	past := time.Now().Add(-time.Hour).Format(time.RFC3339)
	_ = writeUserConfigLine(path, awakeUntilLine, "awakeUntil: "+past)
	if hold, _ := policy(); hold {
		t.Fatal("a --for that passed holds nothing")
	}
	_ = writeStayAwake(path, true)
	if hold, display := policy(); !hold || display {
		t.Fatalf("on: hold=%v display=%v", hold, display)
	}
}

func TestStrayCaffeinatesSkipTheDaemonsAndItself(t *testing.T) {
	ps := "  PID ARGS\n  880 caffeinate -i -m -s -w 697\n17483 caffeinate -dimsu\n17490 caffeinate -i -t 300\n  999 grep caffeinate\n   42 caffeinate -d\n"
	got := strayCaffeinatesIn(ps, 42)
	if len(got) != 1 || got[0] != "caffeinate -dimsu (pid 17483)" {
		t.Fatalf("got %v", got)
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
