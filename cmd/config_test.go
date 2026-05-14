package cmd

import (
	"andriiklymiuk/corgi/utils"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunConfigShow_PrintsPathVersionAndState(t *testing.T) {
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
