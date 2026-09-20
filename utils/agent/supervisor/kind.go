package supervisor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Kind struct {
	Name                   string
	DefaultBin             string
	ConfigDirEnv           string
	CredentialEnv          []string
	SessionPrefixEnv       string
	Args                   func(SpawnConfig) ([]string, error)
	SupportsSpawn          bool
	SupportsPermissionMode bool
	BuildsArgvFromSettings bool
}

const (
	KindClaude = "claude"
	KindCodex  = "codex"
	KindCustom = "custom"
)

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
		SessionPrefixEnv:       "CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX",
		Args:                   claudeArgs,
		SupportsSpawn:          true,
		SupportsPermissionMode: true,
		BuildsArgvFromSettings: true,
	},
	// Codex has no remote-control: the daemon never spawns one, but a
	// workspace of this kind opens codex for a person and runs its fixes
	// and bots through codex exec.
	KindCodex: {
		Name:          KindCodex,
		DefaultBin:    "codex",
		ConfigDirEnv:  "CODEX_HOME",
		CredentialEnv: []string{"OPENAI_API_KEY", "CODEX_API_KEY"},
		Args:          codexArgs,
	},
	KindCustom: {
		Name:                   KindCustom,
		DefaultBin:             "",
		Args:                   customArgs,
		SupportsSpawn:          false,
		SupportsPermissionMode: false,
		BuildsArgvFromSettings: false,
	},
}

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
	if env := strings.TrimSpace(c.ConfigDirEnv); env != "" {
		k.ConfigDirEnv = env
	}
	if len(c.CredentialEnv) > 0 {
		k.CredentialEnv = append([]string(nil), c.CredentialEnv...)
	}
	return k, nil
}

func KindNames() []string {
	out := make([]string, 0, len(kinds))
	for name := range kinds {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

const DeviceOnlyFlag = "--no-create-session-in-dir"

func claudeArgs(c SpawnConfig) ([]string, error) {
	args := []string{"remote-control"}
	if s := strings.ToLower(strings.TrimSpace(c.Spawn)); s != "" {
		args = append(args, "--spawn", s)
	}
	if c.DeviceOnly {
		args = append(args, DeviceOnlyFlag)
	}
	if c.Capacity > 0 {
		args = append(args, "--capacity", strconv.Itoa(c.Capacity))
	}
	switch {
	case c.SkipPermissions:
		args = append(args, "--permission-mode", "bypassPermissions")
	case strings.TrimSpace(c.PermissionMode) != "" && !forbiddenPermissionModes[normalize(c.PermissionMode)]:
		args = append(args, "--permission-mode", strings.TrimSpace(c.PermissionMode))
	}
	if name := strings.TrimSpace(c.Name); name != "" {
		args = append(args, "--name", name)
	}
	return args, nil
}

func codexArgs(SpawnConfig) ([]string, error) {
	return nil, fmt.Errorf("kind %q has no remote-control session to supervise; corgi agent claude opens codex for a person, and watch fixes and bots run through codex exec", KindCodex)
}

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
	if forbiddenPermissionModes[whole] || (hasValue && forbiddenPermissionModes[value]) {
		return fmt.Errorf(
			"arg %q is not allowed for a supervised session — "+
				"permission prompts are what you answer from your phone", a)
	}
	return nil
}
