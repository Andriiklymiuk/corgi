package cmd

import (
	"strings"
	"testing"
)

// Service files live in ~/Library/LaunchAgents and ~/.config/systemd/user.
// Both are world-readable and land in backups, so nothing secret may be
// written into them — the daemon reads its own config at start instead.
func TestServiceFilesCarryNoSecrets(t *testing.T) {
	// The literal templates, as installLaunchd and installSystemd render them.
	rendered := []string{
		renderedLaunchdPlist("/usr/local/bin/corgi", "/tmp/log", "/tmp/err"),
		renderedSystemdUnit("/usr/local/bin/corgi"),
	}

	forbidden := []string{
		"ANTHROPIC_API_KEY",
		"CLAUDE_CODE_OAUTH_TOKEN",
		"ANTHROPIC_AUTH_TOKEN",
		"bearerToken",
		"botToken",
		"password",
	}

	for _, body := range rendered {
		for _, bad := range forbidden {
			if strings.Contains(body, bad) {
				t.Errorf("service file references %q; these files are world-readable and land in backups", bad)
			}
		}
		if strings.Contains(body, "dangerously-skip-permissions") {
			t.Error("a service file must never install a permission bypass")
		}
		if !strings.Contains(body, "agent") || !strings.Contains(body, "serve") {
			t.Error("the service file should invoke `corgi agent serve`")
		}
	}
}

func TestInstallMechanismIsNamedHonestly(t *testing.T) {
	if installSupported() && installMechanism() == "unsupported" {
		t.Error("a supported platform must name its mechanism")
	}
	if !installSupported() && installMechanism() != "unsupported" {
		t.Error("an unsupported platform must say so rather than naming a mechanism it cannot use")
	}
}
