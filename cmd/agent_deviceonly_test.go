package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/supervisor"
)

func writeAgentUserConfig(t *testing.T, agentDir, body string) {
	t.Helper()
	if err := os.WriteFile(agentUserConfigPath(agentDir), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAutostartWorkspacesRunAsDevicesByDefault(t *testing.T) {
	agentDir := t.TempDir()
	stack := stackWithAgentConfig(t, "version: 1\nworkspace:\n  id: acme\n")
	registerStack(t, agentDir, "acme", stack)
	writeAgentUserConfig(t, agentDir, "version: 1\nworkspaces:\n  acme:\n    autostart: true\n")

	configs, err := loadSpawnConfigs(agentDir, false)
	if err != nil || len(configs) != 1 {
		t.Fatalf("configs = %v, %v", configs, err)
	}
	c := configs[0]
	if !c.DeviceOnly {
		t.Error("a server the daemon starts by itself must not pre-create a session — that row is the one nobody opened")
	}
	if c.SessionNamePrefix != "acme" {
		t.Errorf("on-demand sessions are named after the workspace, got prefix %q", c.SessionNamePrefix)
	}
	if err := supervisor.ValidateSpawnConfig(c); err != nil {
		t.Errorf("the startup config must validate: %v", err)
	}
}

func TestAutostartSessionRestoresThePreCreatedSession(t *testing.T) {
	agentDir := t.TempDir()
	stack := stackWithAgentConfig(t, "version: 1\nworkspace:\n  id: acme\n")
	registerStack(t, agentDir, "acme", stack)
	writeAgentUserConfig(t, agentDir, "version: 1\nworkspaces:\n  acme:\n    autostart: true\n    autostartSession: true\n")

	configs, err := loadSpawnConfigs(agentDir, false)
	if err != nil || len(configs) != 1 {
		t.Fatalf("configs = %v, %v", configs, err)
	}
	if configs[0].DeviceOnly {
		t.Error("autostartSession: true is the opt back in to a session waiting in the list")
	}
}

func TestRemoteStartAlwaysOpensASession(t *testing.T) {
	agentDir := t.TempDir()
	stack := stackWithAgentConfig(t, "version: 1\nworkspace:\n  id: acme\n")
	registerStack(t, agentDir, "acme", stack)

	cfg, err := remoteResolver(agentDir, false)("acme", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeviceOnly {
		t.Error("a start from the phone IS the request for a session")
	}
	if cfg.SessionNamePrefix != "acme" {
		t.Errorf("prefix = %q", cfg.SessionNamePrefix)
	}
}

func TestSessionNamePrefixReadsAsOneWord(t *testing.T) {
	for _, tc := range []struct{ id, profile, want string }{
		{"corgi", "", "corgi"},
		{"corgi", "work", "corgi-work"},
		{"my stack.v2", "", "my-stack-v2"},
		{"  api  ", "personal", "api-personal"},
	} {
		if got := sessionNamePrefix(tc.id, tc.profile); got != tc.want {
			t.Errorf("sessionNamePrefix(%q, %q) = %q, want %q", tc.id, tc.profile, got, tc.want)
		}
	}
}

func TestTitleHookAcceptsAGeneratedOnDemandName(t *testing.T) {
	if !titleIsStillCorgis("corgi-brave-otter", "corgi") {
		t.Error("the adjective-animal pair Claude Code generates under corgi's prefix is nobody's choice")
	}
	for _, chosen := range []string{"corgi-fix-the-login-redirect", "corgi-Brave-Otter", "corgi-brave otter", "corgi-brave"} {
		if titleIsStillCorgis(chosen, "corgi") {
			t.Errorf("%q does not look generated and must be left alone", chosen)
		}
	}
}

func TestLaunchStateCallsAnIdleDeviceReady(t *testing.T) {
	now := time.Now()
	fresh := launchWorkspace{Running: true, DeviceOnly: true, StartedAt: now.Add(-2 * time.Second).UnixMilli()}
	if got := launchStateAt(fresh, now); got != "starting" {
		t.Errorf("a device that came up two seconds ago is still starting, got %q", got)
	}
	settled := launchWorkspace{Running: true, DeviceOnly: true, StartedAt: now.Add(-time.Minute).UnixMilli()}
	if got := launchStateAt(settled, now); got != "ready" {
		t.Errorf("a device up for a minute with no session is ready, not starting: %q", got)
	}
	settled.Live = 1
	if got := launchStateAt(settled, now); got != "live" {
		t.Errorf("a session on it is live, got %q", got)
	}
	withSession := launchWorkspace{Running: true, StartedAt: now.Add(-time.Minute).UnixMilli()}
	if got := launchStateAt(withSession, now); got != "starting" {
		t.Errorf("a server that should have a session and has none is still starting, got %q", got)
	}
}

func TestStatusCallsAnIdleDeviceOnline(t *testing.T) {
	if got := workspaceState(supervisor.RunState{Running: true, DeviceOnly: true}); got != "online" {
		t.Errorf("state = %q, want online", got)
	}
	if got := workspaceState(supervisor.RunState{Running: true, DeviceOnly: true, SessionsThisRun: 1}); got != "running" {
		t.Errorf("a device serving a session is running, got %q", got)
	}
	if got := workspaceState(supervisor.RunState{Running: true}); got != "running" {
		t.Errorf("state = %q, want running", got)
	}
}

func TestAutostartSessionOverlaysLikeACapability(t *testing.T) {
	on := true
	user := &config.UserConfig{
		Defaults:   config.WorkspaceConfig{AutostartSession: true},
		Workspaces: map[string]config.WorkspaceConfig{"acme": {Autostart: &on}},
	}
	if r := config.Resolve("acme", nil, user); !r.AutostartSession {
		t.Error("a default a workspace entry does not mention must survive the overlay")
	}
	_ = filepath.Join // keep the import honest if helpers move
}
