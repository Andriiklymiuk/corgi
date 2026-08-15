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
	// A config written before kinds existed must launch exactly as it did, or
	// upgrading corgi silently changes what every supervised workspace runs.
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
	// The message has to name the alternatives: this fails inside a daemon,
	// where nobody is watching a terminal to go and look them up.
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
	// The returned slice reaches exec.Command. Sharing the backing array with
	// config would let one launch's mutation change the next one's argv.
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
	// There is no sensible default command for an agent corgi knows nothing
	// about, so the failure has to be explicit rather than a PATH lookup for "".
	c := customConfig()
	c.Bin = ""

	if err := ValidateSpawnConfig(c); err == nil {
		t.Fatal("kind custom with no bin must fail at startup")
	}
}

func TestCustomArgsCannotDisarmPermissionPrompts(t *testing.T) {
	// This is the one route around the bypassPermissions rejection: a custom
	// argv is written by hand, so the same rule has to apply to it.
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
	// Another agent's variable is not this kind's business and must survive.
	if !slices.Contains(env, "CLAUDE_CONFIG_DIR=/home/u/.claude") {
		t.Errorf("env = %v, want an unrelated agent's variable left alone", env)
	}
}

func TestConfigDirWithNoVariableToSetIsRejected(t *testing.T) {
	// Silently ignoring it would leave the workspace running under the default
	// account, which looks exactly like running under the right one.
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
	// The two failures differ: a key bills the wrong meter, a token points at
	// another account. Opting in to one must not quietly opt in to the other.
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
	// For a built-in, these variable names are a property of the CLI. Letting
	// config change them would mean a workspace could point the strip list at
	// nothing and hand the child every ambient credential.
	c := baseConfig()
	c.CredentialEnv = []string{"NOTHING_AT_ALL"}

	if err := ValidateSpawnConfig(c); err == nil {
		t.Fatal("credentialEnv on a built-in kind must be rejected")
	}
}

func TestKindWithoutSpawnSupportRejectsSpawnAndPermissionMode(t *testing.T) {
	// Dropping an unsupported setting silently would leave someone believing a
	// session is isolated, or prompting, when it is neither.
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
	// BuildEnv cannot report an error, and returning the parent unchanged would
	// hand a child every ambient credential. Empty fails loudly instead.
	c := baseConfig()
	c.Kind = "nonesuch"

	if env := BuildEnv(c, []string{"ANTHROPIC_API_KEY=sk-live", "PATH=/usr/bin"}); len(env) != 0 {
		t.Errorf("BuildEnv() = %v, want empty for an unresolvable kind", env)
	}
}
