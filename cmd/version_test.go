package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestVersionCmdIsRegistered(t *testing.T) {
	var found bool
	for _, c := range rootCmd.Commands() {
		if c.Name() == "version" {
			found = true
		}
	}
	if !found {
		t.Fatal("version subcommand is not registered on root")
	}
}

func TestVersionCmdPlainOutput(t *testing.T) {
	prev := utils.JSONOutput
	utils.JSONOutput = false
	t.Cleanup(func() { utils.JSONOutput = prev })

	out := captureStdout(t, func() { versionCmd.Run(versionCmd, nil) })
	if !strings.Contains(out, "corgi version "+APP_VERSION) {
		t.Errorf("expected the version, got %q", out)
	}
	if !strings.Contains(out, "releases/tag/v"+APP_VERSION) {
		t.Errorf("expected a changelog link, got %q", out)
	}
}

func TestVersionCmdJSONOutput(t *testing.T) {
	prev := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = prev })

	out := captureStdout(t, func() { versionCmd.Run(versionCmd, nil) })

	var got struct {
		Version   string `json:"version"`
		Changelog string `json:"changelog"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json must emit parseable JSON, got %q: %v", out, err)
	}
	if got.Version != APP_VERSION {
		t.Errorf("version = %q, want %q", got.Version, APP_VERSION)
	}
	if !strings.HasSuffix(got.Changelog, "v"+APP_VERSION) {
		t.Errorf("changelog = %q", got.Changelog)
	}
}
