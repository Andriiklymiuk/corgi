package cmd

import (
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
)

func TestAwayParsers(t *testing.T) {
	if off, known := macUpdatesOff("1\n"); off || !known {
		t.Fatalf("1 means on: off=%v known=%v", off, known)
	}
	if off, known := macUpdatesOff("0\n"); !off || !known {
		t.Fatalf("0 means off: off=%v known=%v", off, known)
	}
	if _, known := macUpdatesOff(""); known {
		t.Fatal("empty output is unknown")
	}
	if !lidSleepDisabled(" SleepDisabled\t\t1\n sleep 1\n") {
		t.Fatal("SleepDisabled 1 should count")
	}
	if lidSleepDisabled(" sleep 1\n SleepDisabled 0\n") {
		t.Fatal("no SleepDisabled 1 line means the lid sleeps")
	}
	if !onACPower("Now drawing from 'AC Power'\n") || onACPower("Now drawing from 'Battery Power'\n") {
		t.Fatal("AC detection")
	}
	if !captiveOK("<HTML><BODY>Success</BODY></HTML>") || captiveOK("<html>Hotel login</html>") {
		t.Fatal("captive detection")
	}
	if l, ok := cpuSpeedLimit("CPU_Speed_Limit \t= 43\nCPU_Available_CPUs = 8\n"); !ok || l != 43 {
		t.Fatalf("speed limit: %d %v", l, ok)
	}
	if _, ok := cpuSpeedLimit("Note: No thermal warning level has been recorded\n"); ok {
		t.Fatal("no limit line means unknown")
	}
	if !fileVaultOn("FileVault is On.\n") || fileVaultOn("FileVault is Off.\n") {
		t.Fatal("filevault")
	}
}

func TestAwayWorkspaceChecks(t *testing.T) {
	user := &config.UserConfig{Workspaces: map[string]config.WorkspaceConfig{
		"a": {Watch: &config.WatchConfig{Enabled: true, Action: "fix"}},
		"b": {Watch: &config.WatchConfig{Enabled: true, Action: "notify"}, Routines: []config.Routine{{Name: "digest", Kind: "digest", Schedule: "daily 08:30"}}},
		"c": {Watch: &config.WatchConfig{Enabled: true, Action: "fix", Isolate: true, PruneAfter: "7d"}},
		"d": {},
	}}
	digest := digestChecks(user)
	if len(digest) != 3 || digest[0].OK || !digest[1].OK || digest[2].OK {
		t.Fatalf("digest: %+v", digest)
	}
	iso := isolationChecks(user)
	if len(iso) != 2 || iso[0].OK || !iso[1].OK {
		t.Fatalf("isolation: %+v", iso)
	}
}
