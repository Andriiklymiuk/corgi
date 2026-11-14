package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestACarryFileOpensOnlyWithItsPassphrase(t *testing.T) {
	pass, err := newCarryPassphrase()
	if err != nil || len(pass) != 24 {
		t.Fatalf("a 20-letter passphrase in groups of four: %q %v", pass, err)
	}
	sealed, err := sealCarry([]byte(`{"workspaces":[{"id":"api"}]}`), pass)
	if err != nil {
		t.Fatal(err)
	}
	if !isCarryFile(sealed) || strings.Contains(string(sealed), "api") {
		t.Fatal("sealed and marked, nothing readable inside")
	}
	plain, err := openCarry(sealed, pass)
	if err != nil || string(plain) != `{"workspaces":[{"id":"api"}]}` {
		t.Fatalf("opens back: %s %v", plain, err)
	}
	if _, err := openCarry(sealed, pass+"x"); err == nil {
		t.Fatal("a wrong passphrase opens nothing")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := openCarry(sealed, pass); err == nil {
		t.Fatal("a changed byte is refused")
	}
}

func TestEnvFilesAreTheIgnoredOnesNearTheTop(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	writeTestFile(t, filepath.Join(root, ".env"), "A=1")
	writeTestFile(t, filepath.Join(root, "api", ".env.local"), "B=2")
	writeTestFile(t, filepath.Join(root, "api", ".env.example"), "B=")
	writeTestFile(t, filepath.Join(root, "web", ".npmrc"), "//registry/:_authToken=x")
	writeTestFile(t, filepath.Join(root, "node_modules", "x", ".env"), "no")
	writeTestFile(t, filepath.Join(root, "a", "b", "c", ".env"), "too deep")
	writeTestFile(t, filepath.Join(root, ".yarnrc.yml"), "tracked")
	if out, err := exec.Command("git", "-C", root, "add", "-f", ".yarnrc.yml").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	files, _, err := collectEnvFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != ".env,api/.env.local,web/.npmrc" {
		t.Fatalf("ignored env files only, not samples, tracked, deep or vendored: %v", got)
	}
}

func TestACarryLandsOnAnotherUserWithTheMemoriesAndMCPMoved(t *testing.T) {
	dir, ws := watchFixture(t, nil)
	oldHome, _ := os.UserHomeDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	gitRepoWithRemote(t, ws, "git@github.com:acme/stack.git")
	writeTestFile(t, filepath.Join(ws, ".env"), "SECRET=1")
	claude := filepath.Join(oldHome, ".claude")
	writeTestFile(t, filepath.Join(claude, "settings.json"), `{"statusLine":{"command":"`+oldHome+`/.claude/line.sh"}}`)
	writeTestFile(t, filepath.Join(claude, "CLAUDE.md"), "be terse")
	writeTestFile(t, filepath.Join(claude, "rules", "go.md"), "no comments")
	writeTestFile(t, filepath.Join(claude, "history.jsonl"), "private")
	key := claudeProjectKey(ws)
	writeTestFile(t, filepath.Join(claude, "projects", key, "memory", "MEMORY.md"), "- a fact")
	writeTestFile(t, filepath.Join(claude, "projects", key, "chat.jsonl"), "transcript")
	writeTestFile(t, filepath.Join(claude, "plugins", "known_marketplaces.json"), `{"corgi":{"source":{"source":"github","repo":"Andriiklymiuk/corgi"}}}`)
	writeTestFile(t, filepath.Join(claude, "plugins", "installed_plugins.json"), `{"version":2,"plugins":{"corgi@corgi":[]}}`)
	writeTestFile(t, filepath.Join(oldHome, ".claude.json"), `{"userID":"me","mcpServers":{"docs":{"url":"https://x"}},"projects":{"`+ws+`":{"mcpServers":{"db":{"command":"pg"}}}}}`)

	pack, err := buildCarryPack(dir, carryOptions{env: true, claude: true})
	if err != nil {
		t.Fatal(err)
	}
	if pack.Home != oldHome || pack.Workspaces[0].Path != "~/dev/acme-stack" || len(pack.Workspaces[0].Files) != 1 || len(pack.Claude) != 1 {
		t.Fatalf("home, the path under it, the env file and one account: %+v", pack)
	}
	raw, _ := json.Marshal(pack)
	for _, never := range []string{"private", "transcript", "userID"} {
		if strings.Contains(string(raw), never) {
			t.Fatalf("%s travelled", never)
		}
	}

	newHome := filepath.Join(t.TempDir(), "other-user")
	t.Setenv("HOME", newHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(newHome, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(newHome, ".local", "share"))
	newDir := mustAgentDir()
	writeTestFile(t, filepath.Join(newHome, ".claude", "CLAUDE.md"), "mine")
	writeTestFile(t, filepath.Join(newHome, ".claude.json"), `{"userID":"them","mcpServers":{"docs":{"url":"https://mine"}}}`)
	clone := func(remote, target string) error {
		if !strings.HasPrefix(remote, "https://") {
			return os.ErrPermission
		}
		return os.MkdirAll(filepath.Join(target, ".git"), 0o755)
	}
	var claudeCalls []string
	runClaude := func(d string, args ...string) error {
		claudeCalls = append(claudeCalls, d+" "+strings.Join(args, " "))
		return nil
	}
	done, err := applyPeerPack(newDir, pack, importOptions{clone: clone, runClaude: runClaude})
	if err != nil {
		t.Fatalf("%v %v", err, done)
	}
	target := filepath.Join(newHome, "dev", "acme-stack")
	registry, _ := workspace.Load(agentRegistryPath(newDir))
	if w, ok := registry.Find("acme-stack"); !ok || w.AbsPath != target {
		t.Fatalf("cloned over https when ssh is refused, at the same place under the new home: %+v", w)
	}
	if b, _ := os.ReadFile(filepath.Join(target, ".env")); string(b) != "SECRET=1" {
		t.Fatalf("env file: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(newHome, ".claude", "settings.json")); !strings.Contains(string(b), newHome+"/.claude/line.sh") || strings.Contains(string(b), oldHome) {
		t.Fatalf("paths in settings follow the home: %s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(newHome, ".claude", "CLAUDE.md")); string(b) != "mine" {
		t.Fatal("a file already here is kept")
	}
	if b, _ := os.ReadFile(filepath.Join(newHome, ".claude", "projects", claudeProjectKey(target), "memory", "MEMORY.md")); string(b) != "- a fact" {
		t.Fatalf("the memory moves to the new project key: %q", b)
	}
	var state map[string]any
	b, _ := os.ReadFile(filepath.Join(newHome, ".claude.json"))
	_ = json.Unmarshal(b, &state)
	if state["userID"] != "them" || state["mcpServers"].(map[string]any)["docs"].(map[string]any)["url"] != "https://mine" {
		t.Fatalf("the state file keeps its own values: %s", b)
	}
	projects := state["projects"].(map[string]any)
	if _, ok := projects[target].(map[string]any)["mcpServers"].(map[string]any)["db"]; !ok {
		t.Fatalf("project MCP servers land on the new path: %s", b)
	}
	if strings.Join(claudeCalls, "|") != filepath.Join(newHome, ".claude")+" plugin marketplace add Andriiklymiuk/corgi|"+filepath.Join(newHome, ".claude")+" plugin install corgi@corgi" {
		t.Fatalf("the marketplace and the plugin: %v", claudeCalls)
	}
}

func TestAWorkspaceThatFailsDoesNotStopTheRest(t *testing.T) {
	dir, _ := watchFixture(t, nil)
	pack := peerPack{Workspaces: []packWorkspace{{ID: "gone", Remote: "https://x/gone.git"}, {ID: "fine", Remote: "https://x/fine.git"}}}
	clone := func(remote, target string) error {
		if strings.Contains(remote, "gone") {
			return os.ErrNotExist
		}
		return os.MkdirAll(filepath.Join(target, ".git"), 0o755)
	}
	_, err := applyPeerPack(dir, pack, importOptions{base: t.TempDir(), clone: clone})
	if err == nil || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("the failure is named: %v", err)
	}
	registry, _ := workspace.Load(agentRegistryPath(dir))
	if _, ok := registry.Find("fine"); !ok {
		t.Fatal("the next workspace still lands")
	}
}

func TestTheSetupWindowGoesToOnePeerThatPairedInsideIt(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	early := pairing.Device{Name: "old", Role: pairing.RolePeer, CreatedAt: now.Add(-time.Hour)}
	fresh := pairing.Device{Name: "new", Role: pairing.RolePeer, CreatedAt: now.Add(time.Minute)}
	if _, err := takeShareWindow(dir, fresh, now); err == nil {
		t.Fatal("no window, nothing")
	}
	if err := openShareWindow(dir, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := takeShareWindow(dir, early, now.Add(time.Minute)); err == nil {
		t.Fatal("a peer paired before the window does not take it")
	}
	win, err := takeShareWindow(dir, fresh, now.Add(2*time.Minute))
	if err != nil || !win.Secrets {
		t.Fatalf("the new one takes it: %+v %v", win, err)
	}
	if _, err := takeShareWindow(dir, fresh, now.Add(3*time.Minute)); err == nil {
		t.Fatal("once")
	}
	_ = openShareWindow(dir, false, now)
	if _, err := takeShareWindow(dir, fresh, now.Add(shareWindowFor+time.Minute)); err == nil {
		t.Fatal("an expired window is closed")
	}
	if !peerMay("POST", "/launch/peers/pack") || peerMay("GET", "/launch/peers/pack") {
		t.Fatal("a peer may POST for the pack, nothing else")
	}
}

func TestRemotesAndHosts(t *testing.T) {
	cases := map[string][2]string{
		"git@github.com:acme/api.git":         {"https://github.com/acme/api.git", "github.com"},
		"https://gitlab.acme.io/team/web.git": {"", "gitlab.acme.io"},
		"ssh://git@gitlab.com:22/a/b.git":     {"", "gitlab.com"},
	}
	for remote, want := range cases {
		if got := httpsRemote(remote); got != want[0] {
			t.Errorf("httpsRemote(%s) = %q", remote, got)
		}
		if got := remoteHost(remote); got != want[1] {
			t.Errorf("remoteHost(%s) = %q", remote, got)
		}
	}
	user := &config.UserConfig{}
	if dirs := claudeConfigDirs(user, t.TempDir()); len(dirs) != 0 {
		t.Fatalf("no dir on disk, none listed: %v", dirs)
	}
}

func TestAgentUpListensOnTheLANOnlyWhenAskedByName(t *testing.T) {
	for addr, want := range map[string]bool{"0.0.0.0:8765": true, "192.168.1.4:8765": true, "127.0.0.1:8765": false, "localhost:8765": false, ":8765": false, "[::1]:8765": false} {
		if got := asksForNonLoopback(addr); got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}
