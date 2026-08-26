package supervisor

import (
	"slices"
	"strings"
	"testing"
)

func baseConfig() SpawnConfig {
	return SpawnConfig{WorkspaceID: "acme-stack", Dir: "/tmp/acme"}
}

func TestValidateSpawnConfigRejectsBypassPermissions(t *testing.T) {
	c := baseConfig()
	c.PermissionMode = "bypassPermissions"

	err := ValidateSpawnConfig(c)
	if err == nil {
		t.Fatal("bypassPermissions must be rejected: an unattended daemon that can skip permission prompts removes the only gate a person answers from their phone")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error should say the mode is not allowed, got %q", err)
	}
}

func TestValidateSpawnConfigRejectsBypassPermissionsRegardlessOfCase(t *testing.T) {
	for _, mode := range []string{"BYPASSPERMISSIONS", "BypassPermissions", " bypasspermissions "} {
		c := baseConfig()
		c.PermissionMode = mode
		if err := ValidateSpawnConfig(c); err == nil {
			t.Errorf("permissionMode %q must be rejected", mode)
		}
	}
}

func TestValidateSpawnConfig(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*SpawnConfig)
		wantErr bool
	}{
		{"defaults are valid", func(*SpawnConfig) {}, false},
		{"accepted permission mode", func(c *SpawnConfig) { c.PermissionMode = "acceptEdits" }, false},
		{"unknown permission mode", func(c *SpawnConfig) { c.PermissionMode = "yolo" }, true},
		{"valid spawn mode", func(c *SpawnConfig) { c.Spawn = "worktree" }, false},
		{"unknown spawn mode", func(c *SpawnConfig) { c.Spawn = "everywhere" }, true},
		{"relative dir", func(c *SpawnConfig) { c.Dir = "relative/path" }, true},
		{"missing dir", func(c *SpawnConfig) { c.Dir = "" }, true},
		{"missing id", func(c *SpawnConfig) { c.WorkspaceID = "" }, true},
		{"negative capacity", func(c *SpawnConfig) { c.Capacity = -1 }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := baseConfig()
			tt.mutate(&c)
			err := ValidateSpawnConfig(c)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSpawnConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBuildArgsNeverSkipsPermissions(t *testing.T) {
	c := baseConfig()
	c.Spawn = "worktree"
	c.Capacity = 4
	c.PermissionMode = "bypassPermissions"

	args, err := BuildArgs(c)
	if err != nil {
		t.Fatalf("BuildArgs() error = %v", err)
	}

	for _, arg := range args {
		if strings.Contains(arg, "dangerously-skip-permissions") {
			t.Fatal("the supervisor must never pass --dangerously-skip-permissions, whatever a shell alias does")
		}
		if strings.EqualFold(arg, "bypassPermissions") {
			t.Fatal("a rejected permission mode must not reach argv even if validation is bypassed")
		}
	}
}

func TestSkipPermissionsIsTheSanctionedBypass(t *testing.T) {
	c := baseConfig()
	c.SkipPermissions = true

	if err := ValidateSpawnConfig(c); err != nil {
		t.Fatalf("dangerouslySkipPermissions is the one allowed bypass and must validate: %v", err)
	}
	args, err := BuildArgs(c)
	if err != nil {
		t.Fatalf("BuildArgs() error = %v", err)
	}
	// It rides on --permission-mode bypassPermissions, the mode remote control
	// understands — not the plain-claude --dangerously-skip-permissions flag.
	i := slices.Index(args, "--permission-mode")
	if i < 0 || i+1 >= len(args) || args[i+1] != "bypassPermissions" {
		t.Fatalf("SkipPermissions must emit --permission-mode bypassPermissions, got %v", args)
	}
}

func TestSkipPermissionsRejectsAConflictingMode(t *testing.T) {
	c := baseConfig()
	c.SkipPermissions = true
	c.PermissionMode = "acceptEdits"
	if err := ValidateSpawnConfig(c); err == nil {
		t.Fatal("both dangerouslySkipPermissions and a different permissionMode is ambiguous and must be rejected")
	}
}

func TestBuildArgs(t *testing.T) {
	c := baseConfig()
	c.Spawn = "worktree"
	c.Capacity = 4
	c.PermissionMode = "acceptEdits"
	c.Name = "acme"

	args, err := BuildArgs(c)
	if err != nil {
		t.Fatalf("BuildArgs() error = %v", err)
	}

	if len(args) == 0 || args[0] != "remote-control" {
		t.Fatalf("argv must start with remote-control, got %v", args)
	}
	for _, want := range [][2]string{
		{"--spawn", "worktree"},
		{"--capacity", "4"},
		{"--permission-mode", "acceptEdits"},
		{"--name", "acme"},
	} {
		i := slices.Index(args, want[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != want[1] {
			t.Errorf("expected %s %s in %v", want[0], want[1], args)
		}
	}
}

func TestBuildEnvStripsAmbientCredentialsByDefault(t *testing.T) {
	parent := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=sk-secret",
		"CLAUDE_CODE_OAUTH_TOKEN=oauth-secret",
		"ANTHROPIC_AUTH_TOKEN=another-secret",
	}

	env := BuildEnv(baseConfig(), parent)

	joined := strings.Join(env, "\n")
	for _, leak := range []string{"sk-secret", "oauth-secret", "another-secret"} {
		if strings.Contains(joined, leak) {
			t.Errorf("ambient credential %q leaked into the child env; remote control refuses to run with one set and it silently bills the wrong account", leak)
		}
	}
	if !slices.Contains(env, "PATH=/usr/bin") {
		t.Error("unrelated variables must be preserved")
	}
}

func TestBuildEnvHonoursExplicitOptIn(t *testing.T) {
	parent := []string{"ANTHROPIC_API_KEY=sk-secret", "CLAUDE_CODE_OAUTH_TOKEN=oauth-secret"}

	c := baseConfig()
	c.InheritAPIKey = true
	env := BuildEnv(c, parent)

	if !slices.Contains(env, "ANTHROPIC_API_KEY=sk-secret") {
		t.Error("an explicit opt-in must keep the api key")
	}
	if slices.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=oauth-secret") {
		t.Error("opting in to the api key must not also keep the oauth token")
	}
}

func TestBuildEnvSetsConfigDirAndReplacesInherited(t *testing.T) {
	parent := []string{"CLAUDE_CONFIG_DIR=/inherited/from/daemon", "PATH=/usr/bin"}

	c := baseConfig()
	c.ConfigDir = "/workspace/specific"
	env := BuildEnv(c, parent)

	if !slices.Contains(env, "CLAUDE_CONFIG_DIR=/workspace/specific") {
		t.Error("the workspace's config dir must be set")
	}
	if slices.Contains(env, "CLAUDE_CONFIG_DIR=/inherited/from/daemon") {
		t.Error("the inherited config dir must be replaced, not duplicated — otherwise which one wins depends on env ordering")
	}
}

func TestBuildEnvWithoutConfigDirLeavesInheritedAlone(t *testing.T) {
	parent := []string{"CLAUDE_CONFIG_DIR=/inherited", "PATH=/usr/bin"}

	env := BuildEnv(baseConfig(), parent)

	if !slices.Contains(env, "CLAUDE_CONFIG_DIR=/inherited") {
		t.Error("with no workspace config dir the inherited value should survive")
	}
}

func TestStrippedCredentialsReportsWhatWasRemoved(t *testing.T) {
	parent := []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=x", "CLAUDE_CODE_OAUTH_TOKEN=y"}

	got := StrippedCredentials(baseConfig(), parent)

	want := []string{"ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"}
	if !slices.Equal(got, want) {
		t.Errorf("StrippedCredentials() = %v, want %v — status output depends on this to explain which account ran", got, want)
	}
}

func TestBuildEnvIgnoresMalformedEntries(t *testing.T) {
	env := BuildEnv(baseConfig(), []string{"NOT_AN_ASSIGNMENT", "PATH=/usr/bin"})

	if slices.Contains(env, "NOT_AN_ASSIGNMENT") {
		t.Error("malformed entries should be dropped rather than passed through")
	}
}

func TestValidPermissionMode(t *testing.T) {
	for _, m := range []string{"", "default", "acceptEdits", "plan", "auto", "dontask", "  Default  "} {
		if !ValidPermissionMode(m) {
			t.Errorf("%q should be valid", m)
		}
	}
	for _, m := range []string{"bypassPermissions", "yolo", "nope"} {
		if ValidPermissionMode(m) {
			t.Errorf("%q must be rejected", m)
		}
	}
	if PermissionModeHint() == "" {
		t.Error("the hint must list the accepted modes")
	}
}

func TestActivityWriterReportsEachWrite(t *testing.T) {
	n := 0
	a := activityWriter{report: func() { n++ }}
	if _, err := a.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Write([]byte("more")); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("report called %d times, want one per write", n)
	}
}
