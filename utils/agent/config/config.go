// Package config resolves agent-mode settings from two files with different
// trust levels: `.corgi/agent.yml` is committed and written by whoever wrote
// the repo, so it is UNTRUSTED; the user-level file in the corgi data directory
// is written by the machine's owner and is TRUSTED.
//
// The rule that keeps this safe: untrusted config may restrict, never relax.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// RepoConfig is `.corgi/agent.yml` — committed, untrusted.
//
// Deliberately tiny. Every field here either identifies the workspace or
// restricts what may be done with it. Adding a field that grants capability
// would make cloning a repository a way to run code on someone's machine.
type RepoConfig struct {
	Version   int           `yaml:"version"`
	Workspace RepoWorkspace `yaml:"workspace"`
}

// RepoWorkspace is the identity half of a workspace.
type RepoWorkspace struct {
	ID      string   `yaml:"id"`
	Aliases []string `yaml:"aliases"`
	// Sensitive refuses public tunnels for this workspace. Honoured from the
	// repo file because it only ever takes capability away.
	Sensitive bool `yaml:"sensitive"`
}

// UserConfig is the user-level file — never committed, trusted.
type UserConfig struct {
	Version    int                        `yaml:"version"`
	Workspaces map[string]WorkspaceConfig `yaml:"workspaces"`
	Defaults   WorkspaceConfig            `yaml:"defaults"`
	// NotifyUrl gets a POST per daemon notification; trusted config only.
	NotifyUrl string `yaml:"notifyUrl"`
	// Profiles are named setting bundles pickable at session-start time —
	// "work", "personal" — for running one workspace under different Claude
	// accounts. Trusted like everything else here: a remote caller sends only
	// a profile NAME; what it selects is defined in this file.
	Profiles map[string]WorkspaceConfig `yaml:"profiles"`
}

// WorkspaceConfig is everything that grants capability. Trusted sources only.
type WorkspaceConfig struct {
	Autostart *bool `yaml:"autostart"`
	// Kind selects which agent CLI to supervise. Empty keeps the default, so a
	// config written before this existed behaves exactly as it did.
	Kind string `yaml:"kind"`
	Bin  string `yaml:"bin"`
	// Args is the argv for kind: custom, after the binary name.
	//
	// Trusted config only, like everything else here — an argv is a choice of
	// what code runs, so a committed repo file must never reach it.
	Args []string `yaml:"args"`
	// ConfigDirEnv and CredentialEnv describe a custom kind's environment.
	ConfigDirEnv   string   `yaml:"configDirEnv"`
	CredentialEnv  []string `yaml:"credentialEnv"`
	Spawn          string   `yaml:"spawn"`
	Capacity       int      `yaml:"capacity"`
	PermissionMode string   `yaml:"permissionMode"`
	ConfigDir      string   `yaml:"configDir"`
	WakeLock       string   `yaml:"wakeLock"`
	// InheritAPIKey lets this workspace keep an ambient ANTHROPIC_API_KEY.
	// Off unless the machine's owner asks for it: remote control refuses to
	// run with one set, and an inherited key bills the API instead of a
	// subscription.
	InheritAPIKey     bool `yaml:"inheritApiKey"`
	InheritOAuthToken bool `yaml:"inheritOauthToken"`
	// DangerouslySkipPermissions runs the session with permission prompts off,
	// removing the main defence against it acting on instructions injected into
	// a file it read. Trusted config only by construction — RepoConfig has no
	// such field, so a cloned repository can never turn it on.
	DangerouslySkipPermissions bool `yaml:"dangerouslySkipPermissions"`
}

// LoadRepo reads `.corgi/agent.yml` from a workspace directory. A missing file
// is not an error — agent mode is opt-in.
func LoadRepo(dir string) (*RepoConfig, error) {
	path := filepath.Join(dir, ".corgi", "agent.yml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c RepoConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

// LoadUser reads the trusted user-level config.
//
// The file must not be group- or world-readable: it names the config
// directories holding Claude credentials, and on a shared machine that is a
// map to someone else's account.
func LoadUser(path string) (*UserConfig, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return &UserConfig{Workspaces: map[string]WorkspaceConfig{}}, nil
	}
	if err != nil {
		return nil, err
	}
	// Skipped on Windows: Go reports 0666 for every file there, so this would
	// reject a config the user cannot fix, with advice that does not apply.
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode&0o077 != 0 {
			return nil, fmt.Errorf(
				"%s is readable by other users (mode %04o) — run: chmod 600 %s",
				path, mode, path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c UserConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// Bypass must be a deliberate per-workspace (or per-profile) choice. Under
	// defaults: it would skip prompts for every workspace — including ones a
	// later `agent scan` adds — and the OR overlay means no workspace could turn
	// it back off. Too dangerous to allow as a blanket default.
	if c.Defaults.DangerouslySkipPermissions {
		return nil, fmt.Errorf(
			"%s: dangerouslySkipPermissions cannot be set under defaults: — set it per-workspace or per-profile, so each bypass is a deliberate opt-in with an opt-out",
			path)
	}
	if c.Workspaces == nil {
		c.Workspaces = map[string]WorkspaceConfig{}
	}
	return &c, nil
}

// Resolved is the merged settings for one workspace.
type Resolved struct {
	// ID is the registry's identity for this workspace. Trusted.
	ID string
	// RepoDeclaredID is whatever the committed file called itself. Untrusted:
	// useful to `corgi agent init`, never used to look up settings.
	RepoDeclaredID string
	Aliases        []string
	Sensitive      bool
	WorkspaceConfig
}

// Resolve merges the two files under the restrict-never-relax rule.
//
// id comes from the local registry and the repo file may not change it: the id
// keys into the trusted per-workspace settings, so a clone declaring `id: work`
// would inherit that workspace's configDir, bin and permissionMode.
// RepoDeclaredID is reported separately for `corgi agent init` to adopt.
//
// repo may be nil. Capability-granting fields come only from user.
func Resolve(id string, repo *RepoConfig, user *UserConfig) Resolved {
	out := Resolved{ID: id}

	if repo != nil {
		out.RepoDeclaredID = repo.Workspace.ID
		out.Aliases = repo.Workspace.Aliases
		out.Sensitive = repo.Workspace.Sensitive
	}

	if user == nil {
		return out
	}
	out.WorkspaceConfig = user.Defaults
	if specific, ok := user.Workspaces[out.ID]; ok {
		out.WorkspaceConfig = overlay(out.WorkspaceConfig, specific)
	}
	return out
}

// overlay applies the non-empty fields of over onto base.
func overlay(base, over WorkspaceConfig) WorkspaceConfig {
	if over.Autostart != nil {
		base.Autostart = over.Autostart
	}
	if over.Kind != "" {
		base.Kind = over.Kind
	}
	if over.Bin != "" {
		base.Bin = over.Bin
	}
	// Replaced wholesale rather than appended: an argv is one command, and
	// concatenating a default's flags onto a workspace's own would produce a
	// command line neither file asked for.
	if len(over.Args) > 0 {
		base.Args = over.Args
	}
	if over.ConfigDirEnv != "" {
		base.ConfigDirEnv = over.ConfigDirEnv
	}
	if len(over.CredentialEnv) > 0 {
		base.CredentialEnv = over.CredentialEnv
	}
	if over.Spawn != "" {
		base.Spawn = over.Spawn
	}
	if over.Capacity != 0 {
		base.Capacity = over.Capacity
	}
	if over.PermissionMode != "" {
		base.PermissionMode = over.PermissionMode
	}
	if over.ConfigDir != "" {
		base.ConfigDir = over.ConfigDir
	}
	if over.WakeLock != "" {
		base.WakeLock = over.WakeLock
	}
	// Booleans that grant capability are OR-ed rather than overwritten, so a
	// per-workspace entry cannot silently turn off a default the user set.
	base.InheritAPIKey = base.InheritAPIKey || over.InheritAPIKey
	base.InheritOAuthToken = base.InheritOAuthToken || over.InheritOAuthToken
	base.DangerouslySkipPermissions = base.DangerouslySkipPermissions || over.DangerouslySkipPermissions
	return base
}

// ApplyProfile overlays a named profile onto already-resolved settings. The
// name may come from an untrusted caller (a phone); everything it selects is
// defined in the trusted file, so remote picks from the menu, never cooks.
func ApplyProfile(r Resolved, user *UserConfig, name string) (Resolved, error) {
	if name == "" {
		return r, nil
	}
	if user == nil || len(user.Profiles) == 0 {
		return r, fmt.Errorf("no profiles defined — add a profiles: section to the agent config")
	}
	p, ok := user.Profiles[name]
	if !ok {
		names := make([]string, 0, len(user.Profiles))
		for n := range user.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
		return r, fmt.Errorf("unknown profile %q (defined: %s)", name, strings.Join(names, ", "))
	}
	r.WorkspaceConfig = overlay(r.WorkspaceConfig, p)
	return r, nil
}

// AutostartEnabled reports whether the workspace should be supervised. Opt-in:
// `agent scan ~/projects` can register a dozen stacks, and defaulting to on
// would spawn a process for each on the next daemon start. `agent init` writes
// the trusted config that enables it; `scan` deliberately does not.
func (r Resolved) AutostartEnabled() bool {
	return r.Autostart != nil && *r.Autostart
}
