package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SpawnConfig is one workspace's agent launch settings, resolved from the
// trusted user config.
type SpawnConfig struct {
	WorkspaceID string
	Dir         string
	// Kind selects which agent CLI this workspace runs. Empty means
	// DefaultKind, so a config written before kinds existed keeps working.
	Kind string
	// Bin overrides the kind's default command. Must be a bare command name.
	Bin string
	// Args is the full argv for KindCustom, after the binary name. Ignored by
	// every built-in kind, which builds its own from the settings below.
	Args []string
	// ConfigDirEnv and CredentialEnv describe a custom kind's environment: the
	// variable that scopes it to one account, and the ambient credentials to
	// strip. Built-in kinds carry their own and reject these.
	ConfigDirEnv  string
	CredentialEnv []string
	Spawn         string // same-dir | worktree | session
	Capacity      int
	// PermissionMode is passed to remote control for spawned sessions.
	// bypassPermissions is rejected — see ValidateSpawnConfig.
	PermissionMode string
	// ConfigDir sets the kind's config-directory variable so this workspace runs
	// under its own account, memory, skills, and MCP servers.
	ConfigDir string
	// InheritAPIKey opts this workspace in to an ambient ANTHROPIC_API_KEY.
	// Off by default: remote control refuses to run with one set, and an
	// inherited key silently bills the API instead of a subscription.
	InheritAPIKey bool
	// InheritOAuthToken does the same for CLAUDE_CODE_OAUTH_TOKEN.
	InheritOAuthToken bool
	// SkipPermissions runs the session with permission prompts disarmed,
	// emitted as --permission-mode bypassPermissions. This is the one sanctioned
	// route around the bypass block, so ValidateSpawnConfig allows it — unlike a
	// forbidden permissionMode string or a smuggled --dangerously arg. It comes
	// only from trusted config, and the supervisor warns when it is on, because
	// it removes the gate a person answers from their phone.
	SkipPermissions bool
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
	// Origin says who asked for this workspace to run: OriginAutostart from
	// the daemon's startup set, OriginRemote from a spool command.
	Origin string
	// Profile is the trusted-config profile overlaid onto this workspace's
	// settings, when one was requested.
	Profile string
	// OnSessionURL is runtime wiring, not configuration: the runner installs
	// it so the exec layer can report the claude.ai session URL it spots in
	// the process output. Best-effort — may never fire.
	OnSessionURL func(url string)
	// OnActivity is runtime wiring too: the exec layer calls it on every chunk
	// of process output, which the idle wake lock uses as a "still working"
	// signal. Must be cheap — it is on the output path.
	OnActivity func()
}

// Origin values for SpawnConfig.Origin / RunState.Origin.
const (
	OriginAutostart = "autostart"
	OriginRemote    = "remote"
)

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
	if err := validateSpawnIdentity(c); err != nil {
		return err
	}
	kind, err := KindFor(c)
	if err != nil {
		return fmt.Errorf("workspace %s: %w", c.WorkspaceID, err)
	}
	if err := validatePermissionMode(c, kind); err != nil {
		return err
	}
	if err := validateSpawnMode(c, kind); err != nil {
		return err
	}
	if c.Capacity < 0 {
		return fmt.Errorf("workspace %s: capacity cannot be negative", c.WorkspaceID)
	}
	if _, err := ResolveBin(c); err != nil {
		return fmt.Errorf("workspace %s: %w", c.WorkspaceID, err)
	}
	// Build the argv now so a bad one fails at startup with a clear message,
	// rather than at the first restart hours later.
	if _, err := kind.Args(c); err != nil {
		return fmt.Errorf("workspace %s: %w", c.WorkspaceID, err)
	}
	if err := validateKindOwnedSettings(c, kind); err != nil {
		return err
	}
	if c.ConfigDir != "" && kind.ConfigDirEnv == "" {
		return fmt.Errorf(
			"workspace %s: kind %q has no config-directory variable, so configDir would be ignored — "+
				"set configDirEnv to the variable this CLI reads",
			c.WorkspaceID, kind.Name)
	}
	if c.WakeLock != "" && !ValidWakeLockMode(c.WakeLock) {
		return fmt.Errorf("workspace %s: unknown wakeLock %q (want always, off, session, idle)",
			c.WorkspaceID, c.WakeLock)
	}
	return nil
}

func validateSpawnIdentity(c SpawnConfig) error {
	if c.WorkspaceID == "" {
		return fmt.Errorf("workspace id is required")
	}
	if c.Dir == "" {
		return fmt.Errorf("workspace %s: directory is required", c.WorkspaceID)
	}
	if !filepath.IsAbs(c.Dir) {
		return fmt.Errorf("workspace %s: directory must be absolute, got %q", c.WorkspaceID, c.Dir)
	}
	return nil
}

// ValidPermissionMode reports whether mode is one a supervised session accepts.
// Empty is valid (the CLI's default). Exposed so a bad value can be rejected
// where it is written — e.g. `corgi agent profile add --permission-mode` — not
// only when a session is launched hours later.
func ValidPermissionMode(mode string) bool {
	m := normalize(mode)
	if m == "" {
		return true
	}
	return !forbiddenPermissionModes[m] && validPermissionModes[m]
}

// PermissionModeHint lists the accepted modes for an error message.
func PermissionModeHint() string { return sortedKeys(validPermissionModes) }

func validatePermissionMode(c SpawnConfig, kind Kind) error {
	if c.SkipPermissions {
		// The sanctioned bypass. Still has to be a kind that understands a
		// permission mode, and must not also carry a different one.
		if !kind.SupportsPermissionMode {
			return fmt.Errorf(
				"workspace %s: kind %q takes no permission mode, so dangerouslySkipPermissions has nothing to disarm — put the flag in args: instead",
				c.WorkspaceID, kind.Name)
		}
		if m := normalize(c.PermissionMode); m != "" && m != "bypasspermissions" {
			return fmt.Errorf(
				"workspace %s: set either permissionMode or dangerouslySkipPermissions, not both",
				c.WorkspaceID)
		}
		return nil
	}
	mode := normalize(c.PermissionMode)
	if mode == "" {
		return nil
	}
	if forbiddenPermissionModes[mode] {
		return fmt.Errorf(
			"workspace %s: permissionMode %q is not allowed for a supervised session — "+
				"permission prompts are what you answer from your phone",
			c.WorkspaceID, c.PermissionMode)
	}
	if !kind.SupportsPermissionMode {
		return fmt.Errorf(
			"workspace %s: kind %q does not take permissionMode — put the flag in args: instead, "+
				"so the setting is the one this CLI actually understands",
			c.WorkspaceID, kind.Name)
	}
	if !validPermissionModes[mode] {
		return fmt.Errorf("workspace %s: unknown permissionMode %q (want %s)",
			c.WorkspaceID, c.PermissionMode, sortedKeys(validPermissionModes))
	}
	return nil
}

func validateSpawnMode(c SpawnConfig, kind Kind) error {
	s := strings.ToLower(strings.TrimSpace(c.Spawn))
	if s == "" {
		return nil
	}
	if !kind.SupportsSpawn {
		return fmt.Errorf(
			"workspace %s: kind %q does not take spawn — put the flag in args: instead",
			c.WorkspaceID, kind.Name)
	}
	if !validSpawnModes[s] {
		return fmt.Errorf("workspace %s: unknown spawn mode %q (want %s)",
			c.WorkspaceID, c.Spawn, sortedKeys(validSpawnModes))
	}
	return nil
}

// validateKindOwnedSettings rejects settings a kind builds itself: a
// `capacity: 4` that quietly does nothing reads as a limit being applied.
func validateKindOwnedSettings(c SpawnConfig, kind Kind) error {
	if !kind.BuildsArgvFromSettings {
		if c.Capacity > 0 {
			return fmt.Errorf(
				"workspace %s: kind %q does not take capacity — put the flag in args: instead",
				c.WorkspaceID, kind.Name)
		}
		return nil
	}
	if c.ConfigDirEnv != "" || len(c.CredentialEnv) > 0 {
		return fmt.Errorf(
			"workspace %s: configDirEnv and credentialEnv are only for kind %q — "+
				"kind %q already knows its own",
			c.WorkspaceID, KindCustom, kind.Name)
	}
	if len(c.Args) > 0 {
		return fmt.Errorf(
			"workspace %s: args is only for kind %q — kind %q builds its own argv from "+
				"spawn, capacity and permissionMode",
			c.WorkspaceID, KindCustom, kind.Name)
	}
	return nil
}

// ResolveBin returns the command to run for a workspace: its `bin:` if set,
// otherwise the kind's default.
func ResolveBin(c SpawnConfig) (string, error) {
	bin, err := SanitizeBin(c.Bin)
	if err != nil {
		return "", err
	}
	if bin != "" {
		return bin, nil
	}
	kind, err := KindFor(c)
	if err != nil {
		return "", err
	}
	if kind.DefaultBin == "" {
		return "", fmt.Errorf("kind %q has no default command — set bin: to the command to run", kind.Name)
	}
	return kind.DefaultBin, nil
}

// SanitizeBin rejects a binary name that is a path, so no config file can
// point the supervisor at an arbitrary executable. Only a bare command name
// resolved through PATH is allowed. An empty name is left empty for ResolveBin
// to fill from the kind.
func SanitizeBin(bin string) (string, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return "", nil
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

// BuildArgs returns the argv for a workspace's agent process, after the binary
// name. It never emits a flag that disarms permission prompts, whatever the
// caller's shell aliases or config say.
func BuildArgs(c SpawnConfig) ([]string, error) {
	kind, err := KindFor(c)
	if err != nil {
		return nil, err
	}
	return kind.Args(c)
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
	kind, err := KindFor(c)
	if err != nil {
		// Unreachable through the daemon, which validates first. Returning the
		// parent unchanged would hand the child every ambient credential, so
		// return an empty environment instead: a process with none fails loudly.
		//
		// Non-nil deliberately. exec.Cmd treats a nil Env as "inherit the whole
		// parent environment", which is the exact opposite of what this line is
		// for, so returning nil here would turn the safe fallback into total
		// credential inheritance.
		return []string{}
	}
	configVar := kind.ConfigDirEnv

	keep := make([]string, 0, len(parentEnv))
	for _, entry := range parentEnv {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if isStrippedCredential(kind, key, c) {
			continue
		}
		if configVar != "" && key == configVar && c.ConfigDir != "" {
			continue // replaced below
		}
		keep = append(keep, entry)
	}
	if c.ConfigDir != "" && configVar != "" {
		keep = append(keep, configVar+"="+expandHome(c.ConfigDir))
	}
	return keep
}

// isStrippedCredential reports whether a variable is one of the kind's ambient
// credentials and the workspace has not opted in to keeping it.
//
// The two opt-ins are split because they fail differently: an API key bills the
// API instead of a subscription, while an OAuth token points at another
// account. A name containing OAUTH is treated as the token case, which is the
// convention every CLI here follows.
func isStrippedCredential(kind Kind, key string, c SpawnConfig) bool {
	for _, name := range kind.CredentialEnv {
		if key != name {
			continue
		}
		if strings.Contains(strings.ToUpper(name), "OAUTH") {
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
	kind, err := KindFor(c)
	if err != nil {
		return nil
	}
	var stripped []string
	for _, entry := range parentEnv {
		key, _, ok := strings.Cut(entry, "=")
		if ok && isStrippedCredential(kind, key, c) {
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
