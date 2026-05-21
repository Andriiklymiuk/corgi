package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfigJSONShape(t *testing.T) {
	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, configView{Version: 1, Notifications: true})
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := got["notifications"]; !ok {
		t.Errorf("notifications field missing: %v", got)
	}
	if _, ok := got["version"]; !ok {
		t.Errorf("version field missing: %v", got)
	}
}

// withHumanOutput forces the non-JSON output path and restores the global
// afterward, so these tests stay deterministic even if an earlier test left
// utils.JSONOutput set (test-isolation, not production behavior).
func withHumanOutput(t *testing.T) {
	t.Helper()
	prev := utils.JSONOutput
	utils.JSONOutput = false
	t.Cleanup(func() { utils.JSONOutput = prev })
}

func TestRunConfigShow_PrintsPathVersionAndState(t *testing.T) {
	withHumanOutput(t)
	dir := withTempHome(t)
	if err := utils.SaveUserConfig(&utils.UserConfig{Notifications: true}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { runConfigShow(&cobra.Command{}, nil) })

	wantPath := filepath.Join(dir, ".corgi", "config.yml")
	for _, want := range []string{wantPath, "schema version:", "notifications:  on"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %q", want, out)
		}
	}
}

func TestRunConfigShow_DefaultsToOff(t *testing.T) {
	withHumanOutput(t)
	withTempHome(t)
	out := captureStdout(t, func() { runConfigShow(&cobra.Command{}, nil) })
	if !strings.Contains(out, "notifications:  off") {
		t.Errorf("expected 'notifications:  off' in fresh-config output, got: %q", out)
	}
}

func TestConfigPathSubcommand_PrintsPath(t *testing.T) {
	dir := withTempHome(t)
	out := captureStdout(t, func() { configPathCmd.Run(configPathCmd, nil) })
	want := filepath.Join(dir, ".corgi", "config.yml")
	if !strings.Contains(out, want) {
		t.Errorf("expected path %q in output, got: %q", want, out)
	}
}
