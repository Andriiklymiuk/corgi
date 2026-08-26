package cmd

import (
	"os"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
)

func TestAddProfileWritesAndReloads(t *testing.T) {
	dir := t.TempDir()
	if err := addProfile(dir, "work", config.WorkspaceConfig{ConfigDir: "~/.claude-work"}); err != nil {
		t.Fatal(err)
	}
	// Reload through the real config loader — this is the same path remoteResolver
	// uses, so a profile that saves but does not load would be caught here.
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := user.Profiles["work"].ConfigDir; got != "~/.claude-work" {
		t.Fatalf("configDir = %q, want the saved value", got)
	}
}

func TestAddProfileFileIsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	if err := addProfile(dir, "work", config.WorkspaceConfig{ConfigDir: "~/.claude-work"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(agentUserConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("config mode = %04o; it names credential directories and must be owner-only", perm)
	}
}

func TestAddProfilePreservesExistingProfiles(t *testing.T) {
	dir := t.TempDir()
	if err := addProfile(dir, "work", config.WorkspaceConfig{ConfigDir: "~/.claude-work"}); err != nil {
		t.Fatal(err)
	}
	if err := addProfile(dir, "personal", config.WorkspaceConfig{ConfigDir: "~/.claude-personal"}); err != nil {
		t.Fatal(err)
	}
	profiles, err := loadProfiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles["work"].ConfigDir == "" || profiles["personal"].ConfigDir == "" {
		t.Fatalf("adding a second profile must not drop the first: %+v", profiles)
	}
}

func TestAddProfileRejectsAnEmptyProfile(t *testing.T) {
	if err := addProfile(t.TempDir(), "empty", config.WorkspaceConfig{}); err == nil {
		t.Error("a profile that sets neither config-dir nor bin must be rejected")
	}
}

func TestAddProfileRejectsABinaryPath(t *testing.T) {
	if err := addProfile(t.TempDir(), "bad", config.WorkspaceConfig{Bin: "/usr/local/bin/claude"}); err == nil {
		t.Error("a bin that is a path must be rejected — a profile must not choose an arbitrary program")
	}
}

func TestRemoveProfile(t *testing.T) {
	dir := t.TempDir()
	_ = addProfile(dir, "work", config.WorkspaceConfig{ConfigDir: "~/.claude-work"})

	removed, err := removeProfile(dir, "work")
	if err != nil || !removed {
		t.Fatalf("removeProfile = %v, %v; want removed", removed, err)
	}
	profiles, _ := loadProfiles(dir)
	if _, ok := profiles["work"]; ok {
		t.Error("the profile must be gone after rm")
	}

	if removed, _ := removeProfile(dir, "work"); removed {
		t.Error("removing a missing profile must report not-removed, not a phantom success")
	}
}

func TestAddedProfileIsSelectableByTheResolver(t *testing.T) {
	// End-to-end: a profile added by the command must be applyable at start time.
	dir := t.TempDir()
	stack := stackWithAgentConfig(t, "version: 1\nworkspace:\n  id: acme\n")
	registerStack(t, dir, "acme", stack)
	if err := addProfile(dir, "skp", config.WorkspaceConfig{ConfigDir: "~/.claude-skp"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := remoteResolver(dir, false)("acme", "skp")
	if err != nil {
		t.Fatalf("a profile added by the command must resolve at start, got %v", err)
	}
	if cfg.ConfigDir != "~/.claude-skp" {
		t.Errorf("configDir = %q, want the profile's", cfg.ConfigDir)
	}
}

func TestAddProfileRejectsABadPermissionMode(t *testing.T) {
	if err := addProfile(t.TempDir(), "work", config.WorkspaceConfig{
		ConfigDir: "~/.claude-work", PermissionMode: "yolo",
	}); err == nil {
		t.Error("an unknown permissionMode must be rejected at add time, not deferred to session start")
	}
	if err := addProfile(t.TempDir(), "work", config.WorkspaceConfig{
		ConfigDir: "~/.claude-work", PermissionMode: "bypassPermissions",
	}); err == nil {
		t.Error("a forbidden permissionMode must be rejected")
	}
	if err := addProfile(t.TempDir(), "ok", config.WorkspaceConfig{
		ConfigDir: "~/.claude-work", PermissionMode: "acceptEdits",
	}); err != nil {
		t.Errorf("a valid permissionMode must be accepted, got %v", err)
	}
}

func TestSortedProfileNames(t *testing.T) {
	got := sortedProfileNames(map[string]config.WorkspaceConfig{"zed": {}, "alpha": {}, "mid": {}})
	if len(got) != 3 || got[0] != "alpha" || got[2] != "zed" {
		t.Errorf("sortedProfileNames = %v, want sorted", got)
	}
	if len(sortedProfileNames(nil)) != 0 {
		t.Error("nil profiles must sort to empty")
	}
}

func TestLoadProfilesErrorsOnAGroupReadableConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A world/group-readable config names credential dirs; LoadUser refuses it,
	// and loadProfiles must surface that rather than silently returning nothing.
	if err := os.WriteFile(agentUserConfigPath(dir), []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProfiles(dir); err == nil {
		t.Error("a group-readable config must be an error, not silently empty")
	}
}
