// Package config resolves agent-mode settings from two files with different
// trust levels.
//
// `.corgi/agent.yml` lives in the repository and is committed, so it travels
// with a clone and is written by whoever wrote the repo — which may not be the
// person running the daemon. It is therefore UNTRUSTED.
//
// The user-level file lives in the corgi data directory, is never committed,
// and is written by the machine's owner. It is TRUSTED.
//
// The rule that keeps this safe: **untrusted config may restrict, never
// relax.** A cloned repository can mark itself sensitive, which only ever
// removes capability. It cannot choose which binary runs, which credentials
// are used, or which permission mode applies.
package config

import (
	"fmt"
	"os"
	"path/filepath"

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
}

// WorkspaceConfig is everything that grants capability. Trusted sources only.
type WorkspaceConfig struct {
	Autostart      *bool  `yaml:"autostart"`
	Bin            string `yaml:"bin"`
	Spawn          string `yaml:"spawn"`
	Capacity       int    `yaml:"capacity"`
	PermissionMode string `yaml:"permissionMode"`
	ConfigDir      string `yaml:"configDir"`
	WakeLock       string `yaml:"wakeLock"`
	// InheritAPIKey lets this workspace keep an ambient ANTHROPIC_API_KEY.
	// Off unless the machine's owner asks for it: remote control refuses to
	// run with one set, and an inherited key bills the API instead of a
	// subscription.
	InheritAPIKey     bool `yaml:"inheritApiKey"`
	InheritOAuthToken bool `yaml:"inheritOauthToken"`
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
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		return nil, fmt.Errorf(
			"%s is readable by other users (mode %04o) — run: chmod 600 %s",
			path, mode, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c UserConfig
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Workspaces == nil {
		c.Workspaces = map[string]WorkspaceConfig{}
	}
	return &c, nil
}

// Resolved is the merged settings for one workspace.
type Resolved struct {
	ID        string
	Aliases   []string
	Sensitive bool
	WorkspaceConfig
}

// Resolve merges the two files under the restrict-never-relax rule.
//
// repo may be nil (no committed config). Capability-granting fields come only
// from user, whose per-workspace entry overrides its defaults.
func Resolve(id string, repo *RepoConfig, user *UserConfig) Resolved {
	out := Resolved{ID: id}

	if repo != nil {
		if repo.Workspace.ID != "" {
			out.ID = repo.Workspace.ID
		}
		out.Aliases = repo.Workspace.Aliases
		out.Sensitive = repo.Workspace.Sensitive
	}
	if out.ID == "" {
		out.ID = id
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
	if over.Bin != "" {
		base.Bin = over.Bin
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
	return base
}

// AutostartEnabled reports whether the workspace should be supervised.
// Absent means yes: someone who wrote a user-level entry for a workspace meant
// it to run.
func (r Resolved) AutostartEnabled() bool {
	return r.Autostart == nil || *r.Autostart
}
