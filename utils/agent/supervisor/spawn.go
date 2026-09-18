package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SpawnConfig struct {
	WorkspaceID       string
	Dir               string
	Kind              string
	Bin               string
	Args              []string
	ConfigDirEnv      string
	CredentialEnv     []string
	Spawn             string
	Capacity          int
	PermissionMode    string
	ConfigDir         string
	InheritAPIKey     bool
	InheritOAuthToken bool
	SkipPermissions   bool
	Name              string
	DeviceOnly        bool
	SessionNamePrefix string
	WakeLock          WakeLockMode
	MirrorOutput      bool
	Origin            string
	Profile           string
	OnSessionURL      func(url string)
	OnSessionLink     func(id string)
	OnActivity        func()
}

const (
	OriginAutostart = "autostart"
	OriginRemote    = "remote"
)

var forbiddenPermissionModes = map[string]bool{
	"bypasspermissions": true,
}

var validPermissionModes = map[string]bool{
	"acceptedits": true,
	"auto":        true,
	"default":     true,
	"dontask":     true,
	"plan":        true,
}

var validSpawnModes = map[string]bool{
	"same-dir": true,
	"worktree": true,
	"session":  true,
}

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
	if c.DeviceOnly && !kind.BuildsArgvFromSettings {
		return fmt.Errorf(
			"workspace %s: kind %q builds no argv of its own, so deviceOnly would be ignored — put this CLI's own flag in args: instead",
			c.WorkspaceID, kind.Name)
	}
	if c.Capacity < 0 {
		return fmt.Errorf("workspace %s: capacity cannot be negative", c.WorkspaceID)
	}
	if _, err := ResolveBin(c); err != nil {
		return fmt.Errorf("workspace %s: %w", c.WorkspaceID, err)
	}
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

func ValidPermissionMode(mode string) bool {
	m := normalize(mode)
	if m == "" {
		return true
	}
	return !forbiddenPermissionModes[m] && validPermissionModes[m]
}

func PermissionModeHint() string { return sortedKeys(validPermissionModes) }

func validatePermissionMode(c SpawnConfig, kind Kind) error {
	if c.SkipPermissions {
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

func BuildArgs(c SpawnConfig) ([]string, error) {
	kind, err := KindFor(c)
	if err != nil {
		return nil, err
	}
	return kind.Args(c)
}

func BuildEnv(c SpawnConfig, parentEnv []string) []string {
	kind, err := KindFor(c)
	if err != nil {
		return []string{}
	}
	configVar := kind.ConfigDirEnv
	prefixVar := kind.SessionPrefixEnv
	prefix := sanitizePrefix(c.SessionNamePrefix)

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
			continue
		}
		if prefixVar != "" && key == prefixVar && prefix != "" {
			continue
		}
		keep = append(keep, entry)
	}
	if c.ConfigDir != "" && configVar != "" {
		keep = append(keep, configVar+"="+expandHome(c.ConfigDir))
	}
	if prefix != "" && prefixVar != "" {
		keep = append(keep, prefixVar+"="+prefix)
	}
	return keep
}

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

func sanitizePrefix(p string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(p) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == '.', r == ' ':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

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
