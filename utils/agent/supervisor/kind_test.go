package supervisor

import (
	"slices"
	"strings"
	"testing"
)

func customConfig() SpawnConfig {
	return SpawnConfig{
		WorkspaceID: "acme",
		Dir:         "/tmp/acme",
		Kind:        KindCustom,
		Bin:         "some-agent",
		Args:        []string{"serve", "--headless"},
	}
}

func TestDefaultKindIsUnchangedBehaviour(t *testing.T) {
	c := baseConfig()
	c.Kind = ""

	kind, err := KindFor(c)
	if err != nil {
		t.Fatalf("KindFor() error = %v", err)
	}
	if kind.Name != KindClaude {
		t.Fatalf("empty kind resolved to %q, want %q", kind.Name, KindClaude)
	}

	bin, err := ResolveBin(c)
	if err != nil {
		t.Fatalf("ResolveBin() error = %v", err)
	}
	if bin != "claude" {
		t.Errorf("ResolveBin() = %q, want claude", bin)
	}

	args, err := BuildArgs(c)
	if err != nil {
		t.Fatalf("BuildArgs() error = %v", err)
	}
	if len(args) == 0 || args[0] != "remote-control" {
		t.Errorf("argv = %v, want it to start with remote-control", args)
	}
}

func TestUnknownKindIsRejectedWithTheValidNames(t *testing.T) {
	c := baseConfig()
	c.Kind = "nonesuch"

	err := ValidateSpawnConfig(c)
	if err == nil {
		t.Fatal("an unknown kind must fail at startup, not launch something unexpected")
	}
	for _, name := range KindNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention valid kind %q", err, name)
		}
	}
}

func TestCustomKindRunsTheConfiguredArgv(t *testing.T) {
	c := customConfig()

	if err := ValidateSpawnConfig(c); err != nil {
		t.Fatalf("ValidateSpawnConfig() error = %v", err)
	}
	bin, err := ResolveBin(c)
	if err != nil {
		t.Fatalf("ResolveBin() error = %v", err)
	}
	if bin != "some-agent" {
		t.Errorf("ResolveBin() = %q, want some-agent", bin)
	}
	args, err := BuildArgs(c)
	if err != nil {
		t.Fatalf("BuildArgs() error = %v", err)
	}
	if !slices.Equal(args, []string{"serve", "--headless"}) {
		t.Errorf("argv = %v, want the configured args verbatim", args)
	}
}

func TestCustomKindArgvIsCopiedNotAliased(t *testing.T) {
	c := customConfig()
	args, err := BuildArgs(c)
	if err != nil {
		t.Fatalf("BuildArgs() error = %v", err)
	}
	args[0] = "mutated"

	again, err := BuildArgs(c)
	if err != nil {
		t.Fatalf("BuildArgs() error = %v", err)
	}
	if again[0] != "serve" {
		t.Errorf("argv[0] = %q after mutating a previous result, want serve", again[0])
	}
}

func TestCustomKindWithoutArgsIsRejected(t *testing.T) {
	c := customConfig()
	c.Args = nil

	if err := ValidateSpawnConfig(c); err == nil {
		t.Fatal("kind custom with no args has nothing to run and must fail at startup")
	}
}

func TestCustomKindWithoutBinIsRejected(t *testing.T) {
	c := customConfig()
	c.Bin = ""

	if err := ValidateSpawnConfig(c); err == nil {
		t.Fatal("kind custom with no bin must fail at startup")
	}
}

func TestCustomArgsCannotDisarmPermissionPrompts(t *testing.T) {
	for _, arg := range []string{
		"--dangerously-skip-permissions",
		"--DANGEROUSLY-SKIP-PERMISSIONS",
		"--dangerously-skip-permissions=true",
		"--yolo",
	} {
		c := customConfig()
		c.Args = []string{"serve", arg}

		if err := ValidateSpawnConfig(c); err == nil {
			t.Errorf("arg %q must be rejected — permission prompts are the defence a phone answers", arg)
		}
	}
}

func TestCustomKindUsesItsOwnConfigDirVariable(t *testing.T) {
	c := customConfig()
	c.ConfigDirEnv = "SOME_AGENT_HOME"
	c.ConfigDir = "/home/u/.some-agent-work"

	if err := ValidateSpawnConfig(c); err != nil {
		t.Fatalf("ValidateSpawnConfig() error = %v", err)
	}
	env := BuildEnv(c, []string{"PATH=/usr/bin", "CLAUDE_CONFIG_DIR=/home/u/.claude"})

	if !slices.Contains(env, "SOME_AGENT_HOME=/home/u/.some-agent-work") {
		t.Errorf("env = %v, want the custom kind's config-dir variable set", env)
	}
	if !slices.Contains(env, "CLAUDE_CONFIG_DIR=/home/u/.claude") {
		t.Errorf("env = %v, want an unrelated agent's variable left alone", env)
	}
}

func TestConfigDirWithNoVariableToSetIsRejected(t *testing.T) {
	c := customConfig()
	c.ConfigDir = "/home/u/.some-agent-work"

	err := ValidateSpawnConfig(c)
	if err == nil {
		t.Fatal("configDir with no configDirEnv must fail rather than be ignored")
	}
	if !strings.Contains(err.Error(), "configDirEnv") {
		t.Errorf("error %q should name the setting that fixes it", err)
	}
}

func TestCustomKindStripsItsOwnCredentials(t *testing.T) {
	c := customConfig()
	c.CredentialEnv = []string{"SOME_AGENT_API_KEY", "SOME_AGENT_OAUTH_TOKEN"}

	if err := ValidateSpawnConfig(c); err != nil {
		t.Fatalf("ValidateSpawnConfig() error = %v", err)
	}
	parent := []string{
		"PATH=/usr/bin",
		"SOME_AGENT_API_KEY=sk-live",
		"SOME_AGENT_OAUTH_TOKEN=oauth",
	}

	env := BuildEnv(c, parent)
	for _, unwanted := range []string{"SOME_AGENT_API_KEY=sk-live", "SOME_AGENT_OAUTH_TOKEN=oauth"} {
		if slices.Contains(env, unwanted) {
			t.Errorf("env still carries %q — an inherited credential bills the wrong account", unwanted)
		}
	}

	got := StrippedCredentials(c, parent)
	want := []string{"SOME_AGENT_API_KEY", "SOME_AGENT_OAUTH_TOKEN"}
	if !slices.Equal(got, want) {
		t.Errorf("StrippedCredentials() = %v, want %v — status output explains the account from this", got, want)
	}
}

func TestCustomKindCredentialOptInsAreSeparate(t *testing.T) {
	c := customConfig()
	c.CredentialEnv = []string{"SOME_AGENT_API_KEY", "SOME_AGENT_OAUTH_TOKEN"}
	c.InheritAPIKey = true

	env := BuildEnv(c, []string{"SOME_AGENT_API_KEY=sk-live", "SOME_AGENT_OAUTH_TOKEN=oauth"})

	if !slices.Contains(env, "SOME_AGENT_API_KEY=sk-live") {
		t.Error("inheritApiKey must keep the API key")
	}
	if slices.Contains(env, "SOME_AGENT_OAUTH_TOKEN=oauth") {
		t.Error("inheritApiKey must not also keep the OAuth token")
	}
}

func TestBuiltInKindRejectsEnvironmentOverrides(t *testing.T) {
	c := baseConfig()
	c.CredentialEnv = []string{"NOTHING_AT_ALL"}

	if err := ValidateSpawnConfig(c); err == nil {
		t.Fatal("credentialEnv on a built-in kind must be rejected")
	}
}

func TestKindWithoutSpawnSupportRejectsSpawnAndPermissionMode(t *testing.T) {
	for name, mutate := range map[string]func(*SpawnConfig){
		"spawn":          func(c *SpawnConfig) { c.Spawn = "worktree" },
		"permissionMode": func(c *SpawnConfig) { c.PermissionMode = "default" },
	} {
		c := customConfig()
		mutate(&c)
		if err := ValidateSpawnConfig(c); err == nil {
			t.Errorf("%s on kind custom must be rejected, not ignored", name)
		}
	}
}

func TestUnknownKindProducesNoEnvironmentAtAll(t *testing.T) {
	c := baseConfig()
	c.Kind = "nonesuch"

	if env := BuildEnv(c, []string{"ANTHROPIC_API_KEY=sk-live", "PATH=/usr/bin"}); len(env) != 0 {
		t.Errorf("BuildEnv() = %v, want empty for an unresolvable kind", env)
	}
}

func TestBuildEnvNeverReturnsNil(t *testing.T) {
	c := baseConfig()
	c.Kind = "nonesuch"

	env := BuildEnv(c, []string{"ANTHROPIC_API_KEY=sk-live", "PATH=/usr/bin"})

	if env == nil {
		t.Fatal("BuildEnv() = nil; a nil Env makes os/exec inherit everything")
	}
	if len(env) != 0 {
		t.Errorf("BuildEnv() = %v, want empty", env)
	}
}

func TestSettingsAKindCannotHonourAreRejected(t *testing.T) {
	tests := map[string]func(*SpawnConfig){
		"args on a built-in kind":          func(c *SpawnConfig) { c.Kind = KindClaude; c.Args = []string{"--flag"} },
		"configDirEnv on a built-in kind":  func(c *SpawnConfig) { c.Kind = KindClaude; c.ConfigDirEnv = "X_HOME" },
		"credentialEnv on a built-in kind": func(c *SpawnConfig) { c.Kind = KindClaude; c.CredentialEnv = []string{"X"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := baseConfig()
			mutate(&c)
			if err := ValidateSpawnConfig(c); err == nil {
				t.Error("must be rejected, not silently ignored")
			}
		})
	}

	c := customConfig()
	c.Capacity = 4
	if err := ValidateSpawnConfig(c); err == nil {
		t.Error("capacity on a custom kind must be rejected, not silently ignored")
	}
}

func TestCustomArgsCannotSmuggleAForbiddenPermissionMode(t *testing.T) {
	for _, args := range [][]string{
		{"remote-control", "--permission-mode", "bypassPermissions"},
		{"remote-control", "--permission-mode=bypassPermissions"},
		{"remote-control", "--permission-mode", "BYPASSPERMISSIONS"},
	} {
		c := customConfig()
		c.Bin = "claude"
		c.Args = args

		if err := ValidateSpawnConfig(c); err == nil {
			t.Errorf("args %v must be rejected — this is the invariant the docs promise", args)
		}
	}
}

func TestCustomArgsStillAllowLegitimatePermissionModes(t *testing.T) {
	c := customConfig()
	c.Bin = "claude"
	c.Args = []string{"remote-control", "--permission-mode", "acceptEdits"}

	if err := ValidateSpawnConfig(c); err != nil {
		t.Errorf("ValidateSpawnConfig() = %v, want a normal permission mode accepted", err)
	}
}
