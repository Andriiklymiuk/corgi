package supervisor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// A Kind is one agent CLI the supervisor knows how to keep alive. Only launch
// details differ between agents, so they live here and the rest of the package
// never asks which agent it has.
//
// Adding one is a map entry, deliberately not a registration hook: a kind
// decides the argv of an unattended process, so the set is fixed at compile time.
type Kind struct {
	// Name is what `kind:` in the trusted user config selects.
	Name string
	// DefaultBin is the command looked up on PATH when no `bin:` is set.
	DefaultBin string
	// ConfigDirEnv points the CLI at a per-workspace config directory, which is
	// how one machine runs two accounts. Empty when the CLI has no such
	// variable, in which case a workspace's configDir is rejected at validation
	// rather than silently having no effect — a session running under the wrong
	// account looks exactly like one running under the right one.
	ConfigDirEnv string
	// CredentialEnv are ambient credentials stripped from the child unless the
	// workspace opts in. An inherited key routes work to another account with no
	// visible error, so removing them is the default.
	CredentialEnv []string
	// Args builds argv after the binary name.
	Args func(SpawnConfig) ([]string, error)
	// SupportsSpawn and SupportsPermissionMode say whether this CLI understands
	// those settings. A setting that cannot take effect is an error, not a
	// silent no-op.
	SupportsSpawn          bool
	SupportsPermissionMode bool
	// BuildsArgvFromSettings is true for a kind that assembles its own command
	// line from the typed settings, false for one handed a complete argv. It
	// decides which half of the config is meaningful so the other is rejected,
	// not silently dropped — a `capacity: 4` that does nothing reads as applied.
	BuildsArgvFromSettings bool
}

// Kind names. KindCustom launches an argv the machine's owner wrote out in
// full, which is how an agent corgi has no built-in entry for is supervised
// without corgi guessing at flags it cannot verify.
const (
	KindClaude = "claude"
	KindCustom = "custom"
)

// DefaultKind is what a workspace with no `kind:` gets, so every existing
// config keeps its current behaviour.
const DefaultKind = KindClaude

var kinds = map[string]Kind{
	KindClaude: {
		Name:         KindClaude,
		DefaultBin:   "claude",
		ConfigDirEnv: "CLAUDE_CONFIG_DIR",
		CredentialEnv: []string{
			"ANTHROPIC_API_KEY",
			"ANTHROPIC_AUTH_TOKEN",
			"CLAUDE_CODE_OAUTH_TOKEN",
		},
		Args:                   claudeArgs,
		SupportsSpawn:          true,
		SupportsPermissionMode: true,
		BuildsArgvFromSettings: true,
	},
	KindCustom: {
		Name: KindCustom,
		// No default: a custom kind must name its binary, because there is
		// nothing sensible to fall back to.
		DefaultBin: "",
		// Both are configurable per workspace — see SpawnConfig.ConfigDirEnv
		// and CredentialEnv, which KindFor folds in.
		Args: customArgs,
		// The supervisor cannot know this CLI's flag names, so it refuses to
		// invent them. Put them in `args:` instead.
		SupportsSpawn:          false,
		SupportsPermissionMode: false,
		BuildsArgvFromSettings: false,
	},
}

// KindFor resolves a workspace's kind, folding in the per-workspace overrides
// that only a custom kind may set.
func KindFor(c SpawnConfig) (Kind, error) {
	name := strings.ToLower(strings.TrimSpace(c.Kind))
	if name == "" {
		name = DefaultKind
	}
	k, ok := kinds[name]
	if !ok {
		return Kind{}, fmt.Errorf("unknown kind %q (want %s)", c.Kind, strings.Join(KindNames(), ", "))
	}
	if k.Name != KindCustom {
		return k, nil
	}
	// Only a custom kind takes these from config: for a built-in, the variable
	// names are a property of the CLI, not a choice.
	if env := strings.TrimSpace(c.ConfigDirEnv); env != "" {
		k.ConfigDirEnv = env
	}
	if len(c.CredentialEnv) > 0 {
		k.CredentialEnv = append([]string(nil), c.CredentialEnv...)
	}
	return k, nil
}

// KindNames lists the registered kinds for help text and error messages.
func KindNames() []string {
	out := make([]string, 0, len(kinds))
	for name := range kinds {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// claudeArgs builds `claude remote-control ...`.
func claudeArgs(c SpawnConfig) ([]string, error) {
	args := []string{"remote-control"}
	if s := strings.ToLower(strings.TrimSpace(c.Spawn)); s != "" {
		args = append(args, "--spawn", s)
	}
	if c.Capacity > 0 {
		args = append(args, "--capacity", strconv.Itoa(c.Capacity))
	}
	switch {
	case c.SkipPermissions:
		// The one sanctioned bypass: emitted only for a workspace whose trusted
		// config set dangerouslySkipPermissions. bypassPermissions is a real
		// remote-control mode that corgi otherwise refuses.
		args = append(args, "--permission-mode", "bypassPermissions")
	case strings.TrimSpace(c.PermissionMode) != "" && !forbiddenPermissionModes[normalize(c.PermissionMode)]:
		args = append(args, "--permission-mode", strings.TrimSpace(c.PermissionMode))
	}
	if name := strings.TrimSpace(c.Name); name != "" {
		args = append(args, "--name", name)
	}
	return args, nil
}

// customArgs returns the argv the machine's owner wrote, after checking it does
// not disarm the permission prompts.
func customArgs(c SpawnConfig) ([]string, error) {
	if len(c.Args) == 0 {
		return nil, fmt.Errorf("kind %q requires args: the argv to run after the binary name", KindCustom)
	}
	for _, a := range c.Args {
		if err := checkArg(a); err != nil {
			return nil, err
		}
	}
	return append([]string(nil), c.Args...), nil
}

// forbiddenArgPrefixes never reach a supervised process, whoever wrote them.
// Permission prompts are the main defence against a session acting on
// instructions it read out of a repository, so an unattended daemon must not
// skip them — the same rule that rejects permissionMode: bypassPermissions.
var forbiddenArgPrefixes = []string{
	"--dangerously",
	"--yolo",
}

func checkArg(a string) error {
	whole := normalize(a)
	flag, value, hasValue := strings.Cut(whole, "=")
	if !hasValue {
		flag = whole
	}
	for _, prefix := range forbiddenArgPrefixes {
		if strings.HasPrefix(flag, prefix) {
			return fmt.Errorf(
				"arg %q is not allowed for a supervised session — "+
					"permission prompts are what you answer from your phone", a)
		}
	}
	// A forbidden permission mode can also arrive as a plain value, either as
	// its own arg after the flag or as --permission-mode=bypassPermissions.
	// Without this, `kind: custom, bin: claude, args: [remote-control,
	// --permission-mode, bypassPermissions]` walks straight around the
	// rejection that ValidateSpawnConfig applies to the typed setting.
	if forbiddenPermissionModes[whole] || (hasValue && forbiddenPermissionModes[value]) {
		return fmt.Errorf(
			"arg %q is not allowed for a supervised session — "+
				"permission prompts are what you answer from your phone", a)
	}
	return nil
}
