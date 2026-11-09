package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

func gitRepoWithRemote(t *testing.T, dir, remote string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", remote}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestAPackCarriesTheWorkspacesTheirWatchAndOnlyAskedForTokens(t *testing.T) {
	dir, ws := watchFixture(t, nil)
	gitRepoWithRemote(t, ws, "git@example.com:acme/stack.git")
	bare := filepath.Join(filepath.Dir(ws), "notes")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	registry, _ := workspace.Load(agentRegistryPath(dir))
	registry.Upsert(workspace.Workspace{ID: "notes", AbsPath: bare, Status: workspace.StatusOK})
	if err := workspace.Save(agentRegistryPath(dir), registry); err != nil {
		t.Fatal(err)
	}
	path := agentUserConfigPath(dir)
	user, _ := config.LoadUser(path)
	user.Workspaces["acme-stack"] = config.WorkspaceConfig{ConfigDir: "/home/me/.claude-work", Bin: "/usr/bin/claude", Agents: []string{"claude", "codex"},
		Watch: &config.WatchConfig{Enabled: true, Tracker: "linear", Project: "ENG", Action: "fix", Isolate: true}}
	if err := writeUserConfig(path, user); err != nil {
		t.Fatal(err)
	}
	_ = watch.SaveWorkspaceSecrets(dir, "acme-stack", watch.Secrets{Linear: "lin_secret"})
	_ = watch.SaveSecrets(dir, watch.Secrets{GitHub: "gh_secret", HookSecret: "hook"})

	pack, err := buildPeerPack(dir, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Workspaces) != 1 || pack.Workspaces[0].ID != "acme-stack" || pack.Workspaces[0].Remote != "git@example.com:acme/stack.git" {
		t.Fatalf("one workspace with a remote; one without is left out: %+v", pack.Workspaces)
	}
	w := pack.Workspaces[0]
	if w.Config == nil || w.Config.Watch == nil || w.Config.Watch.Project != "ENG" || len(w.Config.Agents) != 2 {
		t.Fatalf("the watch and the agents travel: %+v", w.Config)
	}
	if w.Config.ConfigDir != "" || w.Config.Bin != "" {
		t.Fatalf("paths of this machine do not travel: %+v", w.Config)
	}
	if w.Secrets != nil || pack.Secrets != nil || pack.hasTokens() {
		t.Fatal("no tokens unless asked")
	}
	raw, _ := json.Marshal(pack)
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatalf("a token in the pack: %s", raw)
	}

	pack, err = buildPeerPack(dir, []string{"acme-stack"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !pack.hasTokens() || pack.Workspaces[0].Secrets.Linear != "lin_secret" || pack.Secrets.GitHub != "gh_secret" || pack.Secrets.HookSecret != "" {
		t.Fatalf("asked for: the workspace's and the machine's tokens, never the hook secret: %+v %+v", pack.Workspaces[0].Secrets, pack.Secrets)
	}
	if _, err := buildPeerPack(dir, []string{"notes"}, false); err == nil {
		t.Fatal("a named workspace with no remote is an error, not a silent skip")
	}
}

func TestImportingAPackClonesRegistersAndKeepsTheWatch(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	base := filepath.Join(t.TempDir(), "dev")
	pack := peerPack{From: "home", GitName: "corgi on build-1", GitEmail: "me@example.com", Workspaces: []packWorkspace{
		{ID: "api", Remote: "git@example.com:acme/api.git", Aliases: []string{"backend"},
			Config: &config.WorkspaceConfig{Agents: []string{"claude", "codex"}, Watch: &config.WatchConfig{Enabled: true, Tracker: "linear", Project: "ENG", Action: "fix"}},
			Secrets: &watch.Secrets{Linear: "lin_secret"}},
		{ID: "stack", Remote: "git@example.com:acme/stack.git", Compose: true},
	}, Secrets: &watch.Secrets{GitHub: "gh_secret"}}
	var cloned, inited []string
	clone := func(remote, target string) error {
		cloned = append(cloned, remote)
		if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
			return err
		}
		if strings.HasSuffix(remote, "stack.git") {
			return os.WriteFile(filepath.Join(target, "corgi-compose.yml"), []byte("services: {}\n"), 0o644)
		}
		return nil
	}
	stackInit := func(target string) error {
		inited = append(inited, filepath.Base(target))
		return os.MkdirAll(filepath.Join(target, ".corgi", "corgi_services"), 0o755)
	}
	done, err := applyPeerPack(dir, pack, importOptions{base: base, skipPerms: true, clone: clone, stackInit: stackInit})
	if err != nil {
		t.Fatalf("%v (done %v)", err, done)
	}
	if len(cloned) != 2 || len(inited) != 1 || inited[0] != "stack" {
		t.Fatalf("both cloned, only the compose one gets corgi init: %v %v", cloned, inited)
	}
	registry, _ := workspace.Load(agentRegistryPath(dir))
	api, ok := registry.Find("api")
	if !ok || api.AbsPath != filepath.Join(base, "api") || len(api.Aliases) != 1 {
		t.Fatalf("registered under its own name where it was cloned: %+v", api)
	}
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	entry := user.Workspaces["api"]
	if entry.Watch == nil || entry.Watch.Project != "ENG" || !entry.DangerouslySkipPermissions || len(entry.Agents) != 2 || entry.Autostart == nil || !*entry.Autostart {
		t.Fatalf("the watch, the agents, the permission choice and autostart are set: %+v", entry)
	}
	if watch.WorkspaceSecrets(dir, "api").Linear != "lin_secret" || watch.LoadSecrets(dir).GitHub != "gh_secret" {
		t.Fatal("the tokens landed")
	}
	done, err = applyPeerPack(dir, pack, importOptions{base: base, clone: clone, stackInit: stackInit})
	if err != nil || len(cloned) != 2 || len(inited) != 1 {
		t.Fatalf("a second import clones nothing again: %v %v %v", err, cloned, inited)
	}
	for _, d := range done {
		if strings.Contains(d, "cloned") {
			t.Fatalf("nothing cloned the second time: %v", done)
		}
	}
}

func TestBootstrapDrivesTheOtherMachineOverSSH(t *testing.T) {
	dir, ws := watchFixture(t, nil)
	gitRepoWithRemote(t, ws, "git@example.com:acme/stack.git")
	_ = watch.SaveWorkspaceSecrets(dir, "acme-stack", watch.Secrets{Linear: "lin_secret"})
	var scripts []string
	var stdins [][]byte
	run := func(_ context.Context, script string, stdin []byte) (string, error) {
		scripts = append(scripts, script)
		stdins = append(stdins, stdin)
		if script == "uname -s" {
			return "Linux\n", nil
		}
		return "", nil
	}
	var out bytes.Buffer
	opt := bootstrapOptions{host: "me@build-1", base: "~/dev", withTokens: true, skipPerms: true, lead: true, pair: true, run: run, out: &out,
		invite: func() (string, string, error) { return "https://home.example.com", "123456", nil }}
	if err := runBootstrap(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(scripts, "\n")
	for _, want := range []string{"uname -s", "command -v corgi", "command -v claude", "corgi agent peers import --dir ~/dev --dangerously-skip-permissions", "corgi agent up --at-login", "loginctl enable-linger", "corgi agent peers join https://home.example.com 123456", "corgi agent peers lead"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing step %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "lin_secret") {
		t.Fatal("a token on a command line")
	}
	var packs int
	for _, in := range stdins {
		if in != nil {
			packs++
			if !bytes.Contains(in, []byte("lin_secret")) || !bytes.Contains(in, []byte("acme/stack.git")) {
				t.Fatalf("the pack goes over stdin, tokens inside: %s", in)
			}
		}
	}
	if packs != 1 {
		t.Fatalf("exactly one step reads stdin: %d", packs)
	}
	if !strings.Contains(out.String(), "claude auth login") || !strings.Contains(out.String(), "leads the shared trackers") {
		t.Fatalf("the hand-over names the logins: %s", out.String())
	}

	scripts, out = nil, bytes.Buffer{}
	opt.dryRun, opt.withTokens = true, false
	if err := runBootstrap(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 0 || !strings.Contains(out.String(), "would send 1 workspace(s)") || !strings.Contains(out.String(), "tokens: false") || !strings.Contains(out.String(), "step pair") {
		t.Fatalf("a dry run sends nothing and says the plan: %d %s", len(scripts), out.String())
	}

	opt.dryRun = false
	opt.run = func(_ context.Context, script string, _ []byte) (string, error) {
		if script == "uname -s" {
			return "Darwin\n", nil
		}
		scripts = append(scripts, script)
		return "", nil
	}
	opt.lead, opt.pair = false, false
	if err := runBootstrap(context.Background(), dir, opt); err != nil {
		t.Fatal(err)
	}
	if j := strings.Join(scripts, "\n"); strings.Contains(j, "loginctl") || strings.Contains(j, "peers join") {
		t.Fatalf("a Mac gets no linger; --no-pair pairs nothing:\n%s", j)
	}
}
