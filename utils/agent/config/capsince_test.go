package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestCapSinceRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	u := UserConfig{Workspaces: map[string]WorkspaceConfig{"w": {Watch: &WatchConfig{Enabled: true, MaxFixesTotal: 3, CapSince: at}}, "z": {Watch: &WatchConfig{Enabled: true}}}}
	data, err := yaml.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "capSince") != 1 {
		t.Fatalf("a zero capSince must be omitted:\n%s", data)
	}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := LoadUser(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Workspaces["w"].Watch; got.MaxFixesTotal != 3 || !got.CapSince.Equal(at) {
		t.Fatalf("got %+v", got)
	}
}
