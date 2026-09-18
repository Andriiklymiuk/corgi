package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceFilesCarryNoSecrets(t *testing.T) {
	rendered := []string{
		renderedLaunchdPlist("/usr/local/bin/corgi", "/tmp/log", "/tmp/err", map[string]string{"PATH": "/usr/bin"}),
		renderedSystemdUnit("/usr/local/bin/corgi", map[string]string{"PATH": "/usr/bin"}),
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

func TestServiceFilesDoNotFightTheSupervisorsOwnPolicy(t *testing.T) {
	plist := stripXMLComments(renderedLaunchdPlist("/usr/local/bin/corgi", "/tmp/o", "/tmp/e", map[string]string{"PATH": "/usr/bin"}))
	if strings.Contains(plist, "<key>SuccessfulExit</key>") {
		t.Error("KeepAlive/SuccessfulExit=false restarts on every error exit, which is precisely the deliberate ones")
	}
	if !strings.Contains(plist, "<key>Crashed</key>") {
		t.Error("the plist should still restart after a genuine crash")
	}

	unit := stripHashComments(renderedSystemdUnit("/usr/local/bin/corgi", map[string]string{"PATH": "/usr/bin"}))
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
	plist := renderedLaunchdPlist(`/opt/A & B/<corgi>`, "/tmp/o", "/tmp/e", map[string]string{"PATH": "/usr/bin"})

	if strings.Contains(plist, "A & B") {
		t.Error("a raw & makes the plist invalid and launchctl bootstrap fails opaquely")
	}
	if !strings.Contains(plist, "A &amp; B") || !strings.Contains(plist, "&lt;corgi&gt;") {
		t.Errorf("path was not escaped: %s", plist)
	}
}

func TestServiceFilesSetAPATH(t *testing.T) {
	plist := renderedLaunchdPlist("/usr/local/bin/corgi", "/tmp/o", "/tmp/e", map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin"})
	if !strings.Contains(plist, "<key>PATH</key>") || !strings.Contains(plist, "/opt/homebrew/bin") {
		t.Errorf("plist does not set PATH: %s", plist)
	}

	unit := renderedSystemdUnit("/usr/local/bin/corgi", map[string]string{"PATH": "/opt/homebrew/bin:/usr/bin"})
	if !strings.Contains(unit, `Environment="PATH=/opt/homebrew/bin:/usr/bin"`) {
		t.Errorf("unit does not set PATH: %s", unit)
	}
}

func TestSystemdPATHIsQuoted(t *testing.T) {
	unit := renderedSystemdUnit("/usr/local/bin/corgi", map[string]string{"PATH": "/opt/My Tools/bin:/usr/bin"})

	if !strings.Contains(unit, `Environment="PATH=/opt/My Tools/bin:/usr/bin"`) {
		t.Errorf("a PATH with a space must be quoted, got: %s", unit)
	}
}

func TestServicePATHIncludesTheUsualClaudeLocations(t *testing.T) {
	got := servicePATH()
	for _, want := range []string{"/usr/local/bin", "/usr/bin"} {
		if !strings.Contains(got, want) {
			t.Errorf("servicePATH() = %q, missing %q", got, want)
		}
	}
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

func TestServiceFilesCarryTheDataDirEnvironment(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", "/custom/corgi")
	t.Setenv("HOMEBREW_PREFIX", "/opt/brew")

	env := serviceEnv()
	if env["CORGI_DATA_DIR"] != "/custom/corgi" || env["HOMEBREW_PREFIX"] != "/opt/brew" {
		t.Fatalf("serviceEnv() = %v", env)
	}

	plist := renderedLaunchdPlist("/usr/local/bin/corgi", "/tmp/o", "/tmp/e", env)
	if !strings.Contains(plist, "<key>CORGI_DATA_DIR</key>") || !strings.Contains(plist, "/custom/corgi") {
		t.Errorf("plist does not carry CORGI_DATA_DIR: %s", plist)
	}
	unit := renderedSystemdUnit("/usr/local/bin/corgi", env)
	if !strings.Contains(unit, `Environment="CORGI_DATA_DIR=/custom/corgi"`) {
		t.Errorf("unit does not carry CORGI_DATA_DIR: %s", unit)
	}
}

func TestServiceEnvOmitsUnsetKeys(t *testing.T) {
	t.Setenv("CORGI_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")

	env := serviceEnv()
	for _, key := range []string{"CORGI_DATA_DIR", "XDG_DATA_HOME"} {
		if _, ok := env[key]; ok {
			t.Errorf("%s should be omitted when unset, not exported empty", key)
		}
	}
}

func TestServiceEnvCapturesHomeForTheDataDir(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	t.Setenv("CORGI_DATA_DIR", "")
	env := serviceEnv()
	if env["HOME"] != "/home/tester" {
		t.Errorf("HOME = %q; the daemon's agent dir now resolves from HOME, so it must be captured", env["HOME"])
	}
}

func TestLingerIsReadFromLogindsAnswer(t *testing.T) {
	if !lingerFromOutput([]byte("yes\n")) {
		t.Fatal("logind said yes and corgi read no")
	}
	for _, out := range []string{"no\n", "", "stub"} {
		if lingerFromOutput([]byte(out)) {
			t.Fatalf("%q read as linger on", out)
		}
	}
}
