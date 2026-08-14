package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Service files live in ~/Library/LaunchAgents and ~/.config/systemd/user.
// Both are world-readable and land in backups, so nothing secret may be
// written into them — the daemon reads its own config at start instead.
func TestServiceFilesCarryNoSecrets(t *testing.T) {
	// The literal templates, as installLaunchd and installSystemd render them.
	rendered := []string{
		renderedLaunchdPlist("/usr/local/bin/corgi", "/tmp/log", "/tmp/err", "/usr/bin"),
		renderedSystemdUnit("/usr/local/bin/corgi", "/usr/bin"),
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

// corgi decides for itself when a workspace should stay down — an auth failure
// or repeated crashes. A service manager configured to restart on any non-zero
// exit would undo exactly those decisions in a loop.
func TestServiceFilesDoNotFightTheSupervisorsOwnPolicy(t *testing.T) {
	// Assert on the effective directives, not the prose: the comments in these
	// templates deliberately name the settings they avoid.
	plist := stripXMLComments(renderedLaunchdPlist("/usr/local/bin/corgi", "/tmp/o", "/tmp/e", "/usr/bin"))
	if strings.Contains(plist, "<key>SuccessfulExit</key>") {
		t.Error("KeepAlive/SuccessfulExit=false restarts on every error exit, which is precisely the deliberate ones")
	}
	if !strings.Contains(plist, "<key>Crashed</key>") {
		t.Error("the plist should still restart after a genuine crash")
	}

	unit := stripHashComments(renderedSystemdUnit("/usr/local/bin/corgi", "/usr/bin"))
	if strings.Contains(unit, "Restart=always") {
		t.Error("restarting on any exit turns a deliberate stop into a loop")
	}
	if !strings.Contains(unit, "Restart=on-abnormal") {
		t.Error("the unit should still restart after an abnormal end")
	}
}

func stripXMLComments(s string) string {
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start:], "-->")
		if end < 0 {
			return s[:start]
		}
		s = s[:start] + s[start+end+3:]
	}
}

func stripHashComments(s string) string {
	var kept []string
	for _, line := range strings.Split(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func TestPlistEscapesPathsThatWouldBreakTheXML(t *testing.T) {
	plist := renderedLaunchdPlist(`/opt/A & B/<corgi>`, "/tmp/o", "/tmp/e", "/usr/bin")

	if strings.Contains(plist, "A & B") {
		t.Error("a raw & makes the plist invalid and launchctl bootstrap fails opaquely")
	}
	if !strings.Contains(plist, "A &amp; B") || !strings.Contains(plist, "&lt;corgi&gt;") {
		t.Errorf("path was not escaped: %s", plist)
	}
}

// launchd and systemd start services with a minimal PATH. Without an explicit
// one the daemon cannot find `claude`, fails to start five times, disables the
// workspace — and `corgi agent doctor` in the user's shell passes throughout.
func TestServiceFilesSetAPATH(t *testing.T) {
	plist := renderedLaunchdPlist("/usr/local/bin/corgi", "/tmp/o", "/tmp/e", "/opt/homebrew/bin:/usr/bin")
	if !strings.Contains(plist, "<key>PATH</key>") || !strings.Contains(plist, "/opt/homebrew/bin") {
		t.Errorf("plist does not set PATH: %s", plist)
	}

	unit := renderedSystemdUnit("/usr/local/bin/corgi", "/opt/homebrew/bin:/usr/bin")
	if !strings.Contains(unit, "Environment=PATH=/opt/homebrew/bin:/usr/bin") {
		t.Errorf("unit does not set PATH: %s", unit)
	}
}

func TestServicePATHIncludesTheUsualClaudeLocations(t *testing.T) {
	got := servicePATH()
	for _, want := range []string{"/usr/local/bin", "/usr/bin"} {
		if !strings.Contains(got, want) {
			t.Errorf("servicePATH() = %q, missing %q", got, want)
		}
	}
	// The installing shell's PATH is where the user verified their setup.
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir != "" && !strings.Contains(got, dir) {
			t.Errorf("servicePATH() dropped %q from the installing shell", dir)
			break
		}
	}
}

func TestServicePATHHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(servicePATH()) {
		if seen[dir] {
			t.Errorf("duplicate entry %q", dir)
		}
		seen[dir] = true
	}
}
