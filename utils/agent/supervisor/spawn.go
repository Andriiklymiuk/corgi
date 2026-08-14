package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// SpawnConfig is one workspace's remote-control launch settings, resolved from
// .corgi/agent.yml.
type SpawnConfig struct {
	WorkspaceID string
	Dir         string
	Bin         string // defaults to "claude"
	Spawn       string // same-dir | worktree | session
	Capacity    int
	// PermissionMode is passed to remote control for spawned sessions.
	// bypassPermissions is rejected — see ValidateSpawnConfig.
	PermissionMode string
	// ConfigDir sets CLAUDE_CONFIG_DIR so this workspace runs under its own
	// Claude account, memory, skills, and MCP servers.
	ConfigDir string
	// InheritAPIKey opts this workspace in to an ambient ANTHROPIC_API_KEY.
	// Off by default: remote control refuses to run with one set, and an
	// inherited key silently bills the API instead of a subscription.
	InheritAPIKey bool
	// InheritOAuthToken does the same for CLAUDE_CODE_OAUTH_TOKEN.
	InheritOAuthToken bool
	// Name is the remote-control session name shown in claude.ai/code.
	Name string
	// WakeLock controls whether the machine is kept awake while this
	// workspace's session runs. Empty means WakeLockSession.
	WakeLock WakeLockMode
	// MirrorOutput echoes the supervised process's output to corgi's stderr.
	//
	// Off by default. Remote control's output can contain anything the session
	// printed — env values, tokens, file contents — and in `serve` mode corgi's
	// stderr is a log file on disk. Only `--foreground`, where a person is
	// watching the terminal, turns this on.
	MirrorOutput bool
}

// credentialEnvVars are stripped from the child environment unless the
// workspace explicitly opts in. Leaving one in place routes work to the wrong
// account without any visible error.
var credentialEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_OAUTH_TOKEN",
}

// forbiddenPermissionModes never reach a supervised process. A daemon running
// unattended must not be able to skip permission prompts — those prompts are
// what a person answers from their phone, and they are the main defence
// against a prompt-injected session acting on its own.
var forbiddenPermissionModes = map[string]bool{
	"bypasspermissions": true,
}

// validPermissionModes mirrors `claude remote-control --permission-mode`.
var validPermissionModes = map[string]bool{
	"acceptedits": true,
	"auto":        true,
	"default":     true,
	"dontask":     true,
	"plan":        true,
}

// validSpawnModes mirrors `claude remote-control --spawn`.
var validSpawnModes = map[string]bool{
	"same-dir": true,
	"worktree": true,
	"session":  true,
}

// ValidateSpawnConfig rejects a configuration before anything is launched, so a
// bad setting fails at startup with a clear message instead of on first use.
func ValidateSpawnConfig(c SpawnConfig) error {
	if c.WorkspaceID == "" {
		return fmt.Errorf("workspace id is required")
	}
	if c.Dir == "" {
		return fmt.Errorf("workspace %s: directory is required", c.WorkspaceID)
	}
	if !filepath.IsAbs(c.Dir) {
		return fmt.Errorf("workspace %s: directory must be absolute, got %q", c.WorkspaceID, c.Dir)
	}
	if mode := normalize(c.PermissionMode); mode != "" {
		if forbiddenPermissionModes[mode] {
			return fmt.Errorf(
				"workspace %s: permissionMode %q is not allowed for a supervised session — "+
					"permission prompts are what you answer from your phone",
				c.WorkspaceID, c.PermissionMode)
		}
		if !validPermissionModes[mode] {
			return fmt.Errorf("workspace %s: unknown permissionMode %q (want %s)",
				c.WorkspaceID, c.PermissionMode, sortedKeys(validPermissionModes))
		}
	}
	if s := strings.ToLower(strings.TrimSpace(c.Spawn)); s != "" && !validSpawnModes[s] {
		return fmt.Errorf("workspace %s: unknown spawn mode %q (want %s)",
			c.WorkspaceID, c.Spawn, sortedKeys(validSpawnModes))
	}
	if c.Capacity < 0 {
		return fmt.Errorf("workspace %s: capacity cannot be negative", c.WorkspaceID)
	}
	if _, err := SanitizeBin(c.Bin); err != nil {
		return fmt.Errorf("workspace %s: %w", c.WorkspaceID, err)
	}
	if c.WakeLock != "" && !ValidWakeLockMode(c.WakeLock) {
		return fmt.Errorf("workspace %s: unknown wakeLock %q (want always, off, session)",
			c.WorkspaceID, c.WakeLock)
	}
	return nil
}

// SanitizeBin rejects a binary name that is a path, so no config file can
// point the supervisor at an arbitrary executable. Only a bare command name
// resolved through PATH is allowed.
func SanitizeBin(bin string) (string, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return "claude", nil
	}
	if strings.ContainsAny(bin, `/\`) {
		return "", fmt.Errorf(
			"bin %q must be a command name found on PATH, not a path — "+
				"a path here would let a config file choose which program the daemon runs", bin)
	}
	if strings.HasPrefix(bin, "-") {
		return "", fmt.Errorf("bin %q cannot start with a dash", bin)
	}
	return bin, nil
}

// BuildArgs returns the argv for a workspace's remote-control process.
// It never emits --dangerously-skip-permissions, whatever the caller's shell
// aliases do.
func BuildArgs(c SpawnConfig) []string {
	args := []string{"remote-control"}
	if s := strings.ToLower(strings.TrimSpace(c.Spawn)); s != "" {
		args = append(args, "--spawn", s)
	}
	if c.Capacity > 0 {
		args = append(args, "--capacity", strconv.Itoa(c.Capacity))
	}
	if mode := strings.TrimSpace(c.PermissionMode); mode != "" && !forbiddenPermissionModes[normalize(mode)] {
		args = append(args, "--permission-mode", mode)
	}
	if name := strings.TrimSpace(c.Name); name != "" {
		args = append(args, "--name", name)
	}
	return args
}

// BuildEnv constructs the child environment explicitly rather than handing over
// the daemon's own.
//
// This exists because launchd and systemd never source a shell rc file, so the
// CLAUDE_CONFIG_DIR aliases people use to separate accounts are simply absent.
// Without setting it here every workspace would silently run under the default
// account — no error, correct-looking output, wrong account.
//
// parentEnv is the environment to derive from, in "KEY=value" form.
func BuildEnv(c SpawnConfig, parentEnv []string) []string {
	keep := make([]string, 0, len(parentEnv))
	for _, entry := range parentEnv {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if isStrippedCredential(key, c) {
			continue
		}
		if key == "CLAUDE_CONFIG_DIR" && c.ConfigDir != "" {
			continue // replaced below
		}
		keep = append(keep, entry)
	}
	if c.ConfigDir != "" {
		keep = append(keep, "CLAUDE_CONFIG_DIR="+expandHome(c.ConfigDir))
	}
	return keep
}

func isStrippedCredential(key string, c SpawnConfig) bool {
	for _, name := range credentialEnvVars {
		if key != name {
			continue
		}
		if name == "CLAUDE_CODE_OAUTH_TOKEN" {
			return !c.InheritOAuthToken
		}
		return !c.InheritAPIKey
	}
	return false
}

// StrippedCredentials reports which credential variables were removed, so the
// supervisor can say so in its startup diagnostic instead of leaving the user
// to wonder which account a task ran under.
func StrippedCredentials(c SpawnConfig, parentEnv []string) []string {
	var stripped []string
	for _, entry := range parentEnv {
		key, _, ok := strings.Cut(entry, "=")
		if ok && isStrippedCredential(key, c) {
			stripped = append(stripped, key)
		}
	}
	sort.Strings(stripped)
	return stripped
}

// expandHome resolves a leading ~ so config files can use the short form.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func sortedKeys(m map[string]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
