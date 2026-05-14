package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetUserConfigDir(t *testing.T) {
	dir, err := GetUserConfigDir()
	if err != nil {
		t.Fatalf("GetUserConfigDir returned error: %v", err)
	}
	if dir == "" {
		t.Fatal("GetUserConfigDir returned empty string")
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".corgi")
	if dir != expected {
		t.Errorf("expected %q, got %q", expected, dir)
	}
}

func TestLoadUserConfig_NotExist(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir) // Windows compat

	cfg, err := LoadUserConfig()
	if err != nil {
		t.Fatalf("expected no error for missing config, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Notifications != false {
		t.Error("expected Notifications=false by default")
	}
}

func TestSaveAndLoadUserConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	want := &UserConfig{Notifications: true}
	if err := SaveUserConfig(want); err != nil {
		t.Fatalf("SaveUserConfig failed: %v", err)
	}

	got, err := LoadUserConfig()
	if err != nil {
		t.Fatalf("LoadUserConfig failed: %v", err)
	}
	if got.Notifications != want.Notifications {
		t.Errorf("Notifications: want %v, got %v", want.Notifications, got.Notifications)
	}
}

func TestSaveUserConfig_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	cfg := &UserConfig{Notifications: false}
	if err := SaveUserConfig(cfg); err != nil {
		t.Fatalf("SaveUserConfig failed: %v", err)
	}

	configPath := filepath.Join(dir, ".corgi", "config.yml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("expected config file at %s to exist", configPath)
	}
}

func TestSaveUserConfig_StampsCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	cfg := &UserConfig{Version: 0, Notifications: true}
	if err := SaveUserConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Version != UserConfigSchemaVersion {
		t.Errorf("Save should bump Version to %d, got %d", UserConfigSchemaVersion, cfg.Version)
	}
	got, _ := LoadUserConfig()
	if got.Version != UserConfigSchemaVersion {
		t.Errorf("on-disk Version = %d, want %d", got.Version, UserConfigSchemaVersion)
	}
}

func TestLoadUserConfig_MigratesUnversionedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".corgi"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".corgi", "config.yml")
	if err := os.WriteFile(path, []byte("notifications: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadUserConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != UserConfigSchemaVersion {
		t.Errorf("expected migrated Version=%d, got %d", UserConfigSchemaVersion, cfg.Version)
	}
	if !cfg.Notifications {
		t.Error("Notifications value lost during migration")
	}
}
