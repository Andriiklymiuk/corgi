package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/agent/peers"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"github.com/spf13/cobra"
)

const (
	gitGlobalFlag       = "--global"
	skipPermissionsFlag = "dangerously-skip-permissions"
	gitUserNameKey      = "user.name"
	withTokensFlag      = "with-tokens"
	withEnvFlag         = "with-env"
	withClaudeFlag      = "with-claude"
)

type peerPack struct {
	From       string          `json:"from"`
	At         time.Time       `json:"at"`
	TZ         string          `json:"tz,omitempty"`
	GitName    string          `json:"gitName,omitempty"`
	GitEmail   string          `json:"gitEmail,omitempty"`
	Home       string          `json:"home,omitempty"`
	Notify     string          `json:"notify,omitempty"`
	Workspaces []packWorkspace `json:"workspaces"`
	Secrets    *watch.Secrets  `json:"secrets,omitempty"`
	Claude     []packClaude    `json:"claude,omitempty"`
}

type packWorkspace struct {
	ID        string                  `json:"id"`
	Remote    string                  `json:"remote"`
	Compose   bool                    `json:"compose,omitempty"`
	Aliases   []string                `json:"aliases,omitempty"`
	Path      string                  `json:"path,omitempty"`
	ConfigDir string                  `json:"configDir,omitempty"`
	Config    *config.WorkspaceConfig `json:"config,omitempty"`
	Secrets   *watch.Secrets          `json:"secrets,omitempty"`
	Files     []packFile              `json:"files,omitempty"`
}

func (p peerPack) hasTokens() bool {
	if p.Notify != "" || (p.Secrets != nil && !p.Secrets.IsZero()) {
		return true
	}
	for _, w := range p.Workspaces {
		if w.Secrets != nil && !w.Secrets.IsZero() {
			return true
		}
	}
	return false
}

var gitOutput = func(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func localTimeZone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	zone, err := os.Readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	if i := strings.Index(zone, "zoneinfo/"); i >= 0 {
		return zone[i+len("zoneinfo/"):]
	}
	return ""
}

func buildPeerPack(dir string, only []string, withTokens bool) (peerPack, error) {
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return peerPack{}, err
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return peerPack{}, err
	}
	pack := peerPack{From: peers.Me(), At: time.Now(), TZ: localTimeZone(),
		GitName: gitOutput(".", "config", gitGlobalFlag, gitUserNameKey), GitEmail: gitOutput(".", "config", gitGlobalFlag, "user.email")}
	for _, ws := range registry.Workspaces {
		if len(only) > 0 && !containsFold(only, ws.ID) {
			continue
		}
		pw, ok := packWorkspaceFor(dir, ws, user, withTokens)
		if !ok && len(only) > 0 {
			return peerPack{}, fmt.Errorf("%s has no git remote to clone from", ws.ID)
		}
		if ok {
			pack.Workspaces = append(pack.Workspaces, pw)
		}
	}
	if withTokens {
		if s := watch.LoadSecrets(dir); !s.IsZero() {
			s.HookSecret = ""
			pack.Secrets = &s
		}
	}
	if len(pack.Workspaces) == 0 {
		return peerPack{}, errors.New("no workspace with a git remote to send; corgi agent init in a checkout first")
	}
	return pack, nil
}

func packWorkspaceFor(dir string, ws workspace.Workspace, user *config.UserConfig, withTokens bool) (packWorkspace, bool) {
	remote := gitOutput(ws.AbsPath, "remote", "get-url", "origin")
	if remote == "" {
		return packWorkspace{}, false
	}
	pw := packWorkspace{ID: ws.ID, Remote: remote, Compose: ws.ComposeFile != "", Aliases: ws.Aliases}
	if wc, ok := user.Workspaces[ws.ID]; ok {
		pw.Config = &config.WorkspaceConfig{Kind: wc.Kind, Agents: wc.Agents, Capacity: wc.Capacity, Watch: wc.Watch, Models: wc.Models, Routines: wc.Routines}
	}
	if withTokens {
		if s := watch.WorkspaceSecrets(dir, ws.ID); !s.IsZero() {
			pw.Secrets = &s
		}
	}
	return pw, true
}

type importOptions struct {
	base      string
	skipPerms bool
	clone     func(remote, target string) error
	stackInit func(target string) error
	runClaude func(dir string, args ...string) error
}

var defaultClone = func(remote, target string) error {
	cmd := exec.Command("git", "clone", remote, target)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

var defaultStackInit = func(target string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "init", "--silent")
	cmd.Dir = target
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

func packTarget(w packWorkspace, opt importOptions, home string) string {
	if opt.base != "" {
		return filepath.Join(expandTilde(opt.base), w.ID)
	}
	if strings.HasPrefix(w.Path, "~/") {
		return underHome(w.Path, home)
	}
	return filepath.Join(home, "dev", w.ID)
}

func newCarryApply(pack peerPack, opt importOptions, home string) *carryApply {
	c := &carryApply{home: home, oldHome: pack.Home}
	old := pack.Home
	if old == "" {
		old = home
	}
	for _, w := range pack.Workspaces {
		if w.Path == "" {
			continue
		}
		from, to := underHome(w.Path, old), packTarget(w, opt, home)
		if from != to {
			c.moves = append(c.moves, [2]string{from, to})
		}
	}
	sort.Slice(c.moves, func(i, j int) bool { return len(c.moves[i][0]) > len(c.moves[j][0]) })
	if old != home {
		c.moves = append(c.moves, [2]string{old, home})
	}
	return c
}

func applyPeerPack(dir string, pack peerPack, opt importOptions) ([]string, error) {
	if opt.clone == nil {
		opt.clone = defaultClone
	}
	if opt.stackInit == nil {
		opt.stackInit = defaultStackInit
	}
	home, _ := os.UserHomeDir()
	c := newCarryApply(pack, opt, home)
	if pack.GitName != "" && gitOutput(".", "config", gitGlobalFlag, gitUserNameKey) == "" {
		_ = exec.Command("git", "config", gitGlobalFlag, gitUserNameKey, pack.GitName).Run()
		_ = exec.Command("git", "config", gitGlobalFlag, "user.email", pack.GitEmail).Run()
		c.done = append(c.done, "git identity: "+pack.GitName)
	}
	if pack.Secrets != nil && watch.LoadSecrets(dir).IsZero() {
		if err := watch.SaveSecrets(dir, *pack.Secrets); err != nil {
			return c.done, err
		}
		c.done = append(c.done, "machine tokens")
	}
	if err := applyPackNotify(dir, pack.Notify); err != nil {
		return c.done, err
	}
	if pack.Notify != "" {
		c.done = append(c.done, "notifications")
	}
	var failed []string
	for _, pc := range pack.Claude {
		if err := c.claudeDir(pc, opt.runClaude); err != nil {
			failed = append(failed, err.Error())
		}
	}
	for _, w := range pack.Workspaces {
		target := packTarget(w, opt, home)
		steps, err := applyPackWorkspace(dir, w, target, opt)
		c.done = append(c.done, steps...)
		if err == nil {
			err = c.envFiles(target, w)
		}
		if err != nil {
			failed = append(failed, err.Error())
		}
	}
	if c.kept > 0 {
		c.done = append(c.done, fmt.Sprintf("%d file(s) already here, kept as they are", c.kept))
	}
	if len(failed) > 0 {
		return c.done, errors.New(strings.Join(failed, "; "))
	}
	return c.done, nil
}

func applyPackNotify(dir, notify string) error {
	if notify == "" {
		return nil
	}
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil || user.NotifyUrl != "" {
		return err
	}
	user.NotifyUrl = notify
	return writeUserConfig(path, user)
}

func httpsRemote(remote string) string {
	if !strings.HasPrefix(remote, "git@") {
		return ""
	}
	host, path, ok := strings.Cut(strings.TrimPrefix(remote, "git@"), ":")
	if !ok {
		return ""
	}
	return "https://" + host + "/" + path
}

func checkoutPackWorkspace(w packWorkspace, target string, opt importOptions) ([]string, error) {
	var done []string
	if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return done, err
		}
		if err := opt.clone(w.Remote, target); err != nil {
			alt := httpsRemote(w.Remote)
			if alt == "" || opt.clone(alt, target) != nil {
				return done, fmt.Errorf("%s: clone %s: %w", w.ID, w.Remote, err)
			}
		}
		done = append(done, w.ID+": cloned")
	}
	if !w.Compose {
		return done, nil
	}
	if _, err := os.Stat(filepath.Join(target, ".corgi", "corgi_services")); err != nil {
		if err := opt.stackInit(target); err != nil {
			return done, fmt.Errorf("%s: corgi init: %w", w.ID, err)
		}
		done = append(done, w.ID+": services cloned")
	}
	return done, nil
}

func applyPackWorkspace(dir string, w packWorkspace, target string, opt importOptions) ([]string, error) {
	done, err := checkoutPackWorkspace(w, target, opt)
	if err != nil {
		return done, err
	}
	agents := []string(nil)
	if w.Config != nil {
		agents = w.Config.Agents
	}
	configDir := ""
	if w.ConfigDir != "" {
		home, _ := os.UserHomeDir()
		configDir = underHome(w.ConfigDir, home)
	}
	if _, err := registerWorkspace(target, w.ID, w.Aliases, configDir, false, opt.skipPerms, agents); err != nil {
		return done, fmt.Errorf("%s: %w", w.ID, err)
	}
	if w.Config != nil {
		if err := savePackWorkspaceConfig(dir, w); err != nil {
			return done, err
		}
	}
	if w.Secrets != nil {
		if err := watch.SaveWorkspaceSecrets(dir, w.ID, *w.Secrets); err != nil {
			return done, err
		}
	}
	return append(done, w.ID+": registered"+map[bool]string{true: " with its watch", false: ""}[w.Config != nil && w.Config.Watch != nil]), nil
}

func savePackWorkspaceConfig(dir string, w packWorkspace) error {
	path := agentUserConfigPath(dir)
	user, err := config.LoadUser(path)
	if err != nil {
		return err
	}
	entry := user.Workspaces[w.ID]
	entry.Kind, entry.Capacity, entry.Watch, entry.Models, entry.Routines = w.Config.Kind, w.Config.Capacity, w.Config.Watch, w.Config.Models, w.Config.Routines
	user.Workspaces[w.ID] = entry
	return writeUserConfig(path, user)
}

type remoteRunner func(ctx context.Context, script string, stdin []byte) (string, error)

func sshRunner(host string) remoteRunner {
	return func(ctx context.Context, script string, stdin []byte) (string, error) {
		cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", host, "bash -lc "+shellQuote(script))
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = io.MultiWriter(&out, os.Stderr), os.Stderr
		err := cmd.Run()
		return out.String(), err
	}
}

const remotePath = `export PATH="$HOME/.local/bin:$HOME/bin:/opt/homebrew/bin:/usr/local/bin:$PATH"; `

type bootstrapOptions struct {
	host       string
	base       string
	only       []string
	withTokens bool
	withEnv    bool
	withClaude bool
	skipPerms  bool
	lead       bool
	lan        bool
	pair       bool
	dryRun     bool
	run        remoteRunner
	invite     func() (url, code string, err error)
	out        io.Writer
}

type bootstrapStep struct {
	name   string
	script string
	stdin  []byte
	when   func(os string) bool
}

func bootstrapSteps(pack peerPack, packJSON []byte, opt bootstrapOptions) []bootstrapStep {
	linux := func(os string) bool { return os == "Linux" }
	importCmd := "corgi agent peers import"
	if opt.base != "" {
		importCmd += " --dir " + shellQuote(opt.base)
	}
	if opt.skipPerms {
		importCmd += " --dangerously-skip-permissions"
	}
	up := "corgi agent up --at-login"
	if opt.lan {
		up += " --http 0.0.0.0:8765"
	}
	steps := []bootstrapStep{
		{name: "corgi", script: remotePath + `command -v corgi >/dev/null || { [ "$(uname -s)" = Darwin ] && brew install andriiklymiuk/homebrew-tools/corgi || curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | sh; }; corgi --version`},
		{name: "claude", script: remotePath + `command -v claude >/dev/null || curl -fsSL https://claude.ai/install.sh | bash; claude --version`},
		{name: fmt.Sprintf("import %d workspace(s)", len(pack.Workspaces)), script: remotePath + importCmd, stdin: packJSON},
		{name: "daemon at login", script: remotePath + up + " >/dev/null 2>&1; corgi agent status | head -3"},
		{name: "linger", script: `loginctl enable-linger "$USER" 2>/dev/null || true`, when: linux},
	}
	if pack.TZ != "" {
		steps = append(steps, bootstrapStep{name: "timezone " + pack.TZ, script: `grep -q '^export TZ=' ~/.profile 2>/dev/null || echo 'export TZ=` + pack.TZ + `' >> ~/.profile`, when: linux})
	}
	return steps
}

func runBootstrap(ctx context.Context, dir string, opt bootstrapOptions) error {
	if opt.out == nil {
		opt.out = os.Stdout
	}
	pack, err := buildCarryPack(dir, carryOptions{only: opt.only, tokens: opt.withTokens, env: opt.withEnv, claude: opt.withClaude})
	if err != nil {
		return err
	}
	packJSON, _ := json.Marshal(pack)
	say := func(format string, a ...any) { fmt.Fprintf(opt.out, format+"\n", a...) }
	if opt.dryRun {
		sayBootstrapPlan(say, pack, packJSON, opt)
		return nil
	}
	remoteOS, err := opt.run(ctx, "uname -s", nil)
	if err != nil {
		return fmt.Errorf("ssh %s: %w (keys in place? BatchMode asks nothing)", opt.host, err)
	}
	remoteOS = strings.TrimSpace(remoteOS)
	say("%s runs %s", opt.host, remoteOS)
	for _, s := range bootstrapSteps(pack, packJSON, opt) {
		if s.when != nil && !s.when(remoteOS) {
			continue
		}
		say("→ %s", s.name)
		out, err := opt.run(ctx, s.script, s.stdin)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		if s.name == "corgi" {
			warnOlderRemote(say, opt.host, out, pack)
		}
	}
	if opt.pair {
		if err := pairBootstrapped(ctx, say, opt); err != nil {
			return err
		}
	}
	sayBootstrapLeft(say, pack, opt)
	return nil
}

func warnOlderRemote(say func(string, ...any), host, out string, pack peerPack) {
	fields := strings.Fields(lastLine(out))
	if len(fields) == 0 {
		return
	}
	there := strings.TrimPrefix(fields[len(fields)-1], "v")
	carries := len(pack.Claude) > 0
	for _, w := range pack.Workspaces {
		carries = carries || len(w.Files) > 0
	}
	if carries && versionLess(there, APP_VERSION) {
		say("  %s runs corgi %s, older than %s here: it skips the env files and Claude settings. ssh %s corgi upd, then run this again", host, there, APP_VERSION, host)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func sayBootstrapPlan(say func(string, ...any), pack peerPack, packJSON []byte, opt bootstrapOptions) {
	say("would send %d workspace(s) to %s (tokens: %v):", len(pack.Workspaces), opt.host, pack.hasTokens())
	for _, w := range pack.Workspaces {
		say("  %-16s %s%s", w.ID, w.Remote, packFilesNote(len(w.Files), "env file"))
	}
	for _, pc := range pack.Claude {
		say("  claude %-9s %d file(s), %d plugin(s)", pc.Dir, len(pc.Files), len(pc.Plugins))
	}
	for _, s := range bootstrapSteps(pack, packJSON, opt) {
		say("  step %s: ssh %s %s", s.name, opt.host, firstLineOf(s.script))
	}
	if opt.pair {
		say("  step pair: corgi agent peers invite here, then ssh %s corgi agent peers join <url> <code>", opt.host)
	}
}

func pairBootstrapped(ctx context.Context, say func(string, ...any), opt bootstrapOptions) error {
	say("→ pairing as peers")
	url, code, err := opt.invite()
	if err != nil {
		return fmt.Errorf("pair window here: %w", err)
	}
	if _, err := opt.run(ctx, remotePath+"corgi agent peers join "+shellQuote(url)+" "+shellQuote(code), nil); err != nil {
		return fmt.Errorf("peers join from %s: %w (the other side needs a URL this machine can reach: a tunnel, or --lan on one network)", opt.host, err)
	}
	if !opt.lead {
		return nil
	}
	if _, err := opt.run(ctx, remotePath+"corgi agent peers lead", nil); err != nil {
		return fmt.Errorf("peers lead: %w", err)
	}
	say("%s leads the shared trackers", opt.host)
	return nil
}

func sayBootstrapLeft(say func(string, ...any), pack peerPack, opt bootstrapOptions) {
	say("")
	say("left to do on %s, by hand:", opt.host)
	say("  ssh %s claude auth login        # prints a URL to open anywhere and a code to paste back", opt.host)
	say("  ssh %s gh auth login             # or glab auth login --token …; a run opens pull requests with it", opt.host)
	if !pack.hasTokens() {
		say("  ssh %s corgi agent watch auth <tracker> --token … --workspace <id>   # or run again with --with-tokens", opt.host)
	}
	say("  a stable URL: corgi agent tunnel setup <host> there (the quick tunnel changes on restart and un-pairs)")
	say("  ssh %s corgi agent doctor        # the rest, named", opt.host)
}

var agentPeersExportCmd = &cobra.Command{
	Use:   "export",
	Short: "This machine's workspaces as a pack another machine imports",
	Long: `Prints the pack as JSON, or with --out seals it into one file for a machine
with no ssh to this one (AirDrop, a USB stick).

  corgi agent peers export --out ~/Desktop/me.corgi --with-tokens --with-env --with-claude
  corgi agent peers import --from me.corgi          # on the other machine

--with-env takes the git-ignored .env, .npmrc and .yarnrc.yml files of each
workspace. --with-claude takes every Claude account's settings, rules, skills,
memories, MCP servers and the plugin list (never the login). The file is
sealed with a passphrase printed once (or ` + carryPassEnv + `).`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		only, _ := cmd.Flags().GetStringSlice("workspace")
		withTokens, _ := cmd.Flags().GetBool(withTokensFlag)
		withEnv, _ := cmd.Flags().GetBool(withEnvFlag)
		withClaude, _ := cmd.Flags().GetBool(withClaudeFlag)
		out, _ := cmd.Flags().GetString("out")
		pack, err := buildCarryPack(dir, carryOptions{only: only, tokens: withTokens, env: withEnv, claude: withClaude})
		if err != nil {
			exitWithError("agent_peers", err, 2)
			return
		}
		if out == "" {
			utils.PrintJSON(pack)
			return
		}
		if err := writeCarryFile(expandTilde(out), pack); err != nil {
			exitWithError("agent_peers", err, 1)
		}
	},
}

func writeCarryFile(path string, pack peerPack) error {
	pass := os.Getenv(carryPassEnv)
	minted := pass == ""
	if minted {
		var err error
		if pass, err = newCarryPassphrase(); err != nil {
			return err
		}
	}
	plain, err := json.Marshal(pack)
	if err != nil {
		return err
	}
	sealed, err := sealCarry(plain, pass)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return err
	}
	if utils.JSONOutput {
		ans := map[string]any{"file": path, "bytes": len(sealed), "workspaces": len(pack.Workspaces), "claude": len(pack.Claude)}
		if minted {
			ans["passphrase"] = pass
		}
		utils.PrintJSON(ans)
		return nil
	}
	fmt.Printf("sealed %d workspace(s), %d Claude account(s) into %s (%d KB)\n", len(pack.Workspaces), len(pack.Claude), path, len(sealed)>>10)
	if minted {
		fmt.Printf("passphrase: %s\n", pass)
		fmt.Println("  shown once, stored nowhere. Type it on the other machine; do not send it with the file.")
	}
	fmt.Printf("there: corgi agent peers import --from %s\n", filepath.Base(path))
	return nil
}

func readPackInput(from string) (peerPack, error) {
	var raw []byte
	var err error
	if from != "" {
		raw, err = os.ReadFile(expandTilde(from))
	} else {
		raw, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		return peerPack{}, err
	}
	if isCarryFile(raw) {
		pass, err := askCarryPassphrase()
		if err != nil {
			return peerPack{}, err
		}
		if raw, err = openCarry(raw, pass); err != nil {
			return peerPack{}, err
		}
	}
	var pack peerPack
	if err := json.Unmarshal(raw, &pack); err != nil || len(pack.Workspaces) == 0 {
		return peerPack{}, errors.New("not a pack: corgi agent peers export --out me.corgi here, then import --from me.corgi there")
	}
	return pack, nil
}

var agentPeersImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Clone and register the workspaces of a pack (stdin or --from a sealed file)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		base, _ := cmd.Flags().GetString("dir")
		skipPerms, _ := cmd.Flags().GetBool(skipPermissionsFlag)
		from, _ := cmd.Flags().GetString("from")
		pack, err := readPackInput(from)
		if err != nil {
			exitWithError("agent_peers", err, 2)
			return
		}
		done, err := applyPeerPack(dir, pack, importOptions{base: base, skipPerms: skipPerms, runClaude: runClaudeCommand})
		reportPackImport(pack, done, err)
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
	},
}

func reportPackImport(pack peerPack, done []string, err error) {
	left := packLeftToDo(pack)
	if utils.JSONOutput {
		out := map[string]any{"done": done, "from": pack.From, "left": left}
		if err != nil {
			out["error"] = err.Error()
		}
		utils.PrintJSON(out)
		return
	}
	for _, d := range done {
		fmt.Println(d)
	}
	if len(left) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("left to do here, by hand:")
	for _, l := range left {
		fmt.Println("  " + l)
	}
}

var commandSucceeds = func(env []string, name string, args ...string) bool {
	if _, err := exec.LookPath(name); err != nil {
		return false
	}
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run() == nil
}

func packLeftToDo(pack peerPack) []string {
	home, _ := os.UserHomeDir()
	var left []string
	dirs := []string{"~/.claude"}
	for _, pc := range pack.Claude {
		if pc.Dir != "~/.claude" {
			dirs = append(dirs, pc.Dir)
		}
	}
	for _, d := range dirs {
		var env []string
		prefix := ""
		if d != "~/.claude" {
			env = []string{"CLAUDE_CONFIG_DIR=" + underHome(d, home)}
			prefix = "CLAUDE_CONFIG_DIR=" + d + " "
		}
		if !commandSucceeds(env, "claude", "auth", "status") {
			left = append(left, prefix+"claude auth login        # once per account; no browser here prints a URL")
		}
	}
	hosts := map[string]bool{}
	compose := false
	for _, w := range pack.Workspaces {
		hosts[remoteHost(w.Remote)] = true
		compose = compose || w.Compose
	}
	if hosts["github.com"] && !commandSucceeds(nil, "gh", "auth", "status") {
		left = append(left, "gh auth login --git-protocol ssh   # pull requests, and ssh keys for the clones")
	}
	for h := range hosts {
		if strings.Contains(h, "gitlab") && !commandSucceeds(nil, "glab", "auth", "status") {
			left = append(left, "glab auth login --hostname "+h)
			break
		}
	}
	left = append(left, "corgi agent up --at-login            # the daemon, the endpoint and a pairing QR")
	left = append(left, "corgi agent tunnel setup <hostname>  # a URL that survives restarts (one per machine)")
	if compose {
		left = append(left, "databases: corgi db snapshot on the old machine, copy the file, corgi db restore <file> here")
	}
	return append(left, "corgi agent doctor                   # whatever is still missing, named")
}

func remoteHost(remote string) string {
	if alt := httpsRemote(remote); alt != "" {
		remote = alt
	}
	rest := remote
	if _, after, ok := strings.Cut(remote, "://"); ok {
		rest = after
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	host, _, _ := strings.Cut(rest, "/")
	host, _, _ = strings.Cut(host, ":")
	return strings.ToLower(host)
}

func packFilesNote(n int, what string) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (+%d %s(s))", n, what)
}

var agentPeersBootstrapCmd = &cobra.Command{
	Use:   "bootstrap <user@host>",
	Short: "Set another machine up over ssh and pair it as a peer",
	Long: `A new laptop or a server joins this one over ssh: corgi and claude are
installed, this machine's workspaces are cloned and registered under the same
names with their watch, models and routines, the daemon starts at login, and
the two pair as peers so one leads each shared tracker. Logins stay by hand
(claude auth login, gh auth login) and are printed at the end.

  corgi agent peers bootstrap me@build-1 --dry-run
  corgi agent peers bootstrap me@build-1 --workspace api --with-tokens --lead
  corgi agent peers bootstrap me@laptop-2 --lan        # same network, no tunnel

--with-tokens copies this machine's tracker tokens over the ssh session
(never on a command line). --lead makes the other machine do the unattended
work. --dangerously-skip-permissions is what a server needs for runs nobody
answers; pair it with corgi agent harden there.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		only, _ := cmd.Flags().GetStringSlice("workspace")
		base, _ := cmd.Flags().GetString("dir")
		withTokens, _ := cmd.Flags().GetBool(withTokensFlag)
		withEnv, _ := cmd.Flags().GetBool(withEnvFlag)
		withClaude, _ := cmd.Flags().GetBool(withClaudeFlag)
		skipPerms, _ := cmd.Flags().GetBool(skipPermissionsFlag)
		lead, _ := cmd.Flags().GetBool("lead")
		lan, _ := cmd.Flags().GetBool("lan")
		noPair, _ := cmd.Flags().GetBool("no-pair")
		dry, _ := cmd.Flags().GetBool(watchFlagDryRun)
		opt := bootstrapOptions{host: args[0], base: base, only: only, withTokens: withTokens, withEnv: withEnv, withClaude: withClaude, skipPerms: skipPerms, lead: lead, lan: lan, pair: !noPair, dryRun: dry,
			run: sshRunner(args[0]), invite: func() (string, string, error) {
				ans, err := requestPairWindowFor(dir, pairing.RolePeer, 6*time.Second)
				if err != nil {
					return "", "", err
				}
				return peerURL(dir, ans), ans.Code, nil
			}}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := runBootstrap(ctx, dir, opt); err != nil {
			exitWithError("agent_peers", err, 1)
		}
	},
}

func init() {
	agentPeersExportCmd.Flags().StringSlice("workspace", nil, "Only these workspace ids")
	agentPeersExportCmd.Flags().Bool(withTokensFlag, false, "Include the tracker tokens and the notification URL")
	agentPeersExportCmd.Flags().Bool(withEnvFlag, false, "Include each workspace's git-ignored .env, .npmrc and .yarnrc.yml files")
	agentPeersExportCmd.Flags().Bool(withClaudeFlag, false, "Include every Claude account's settings, rules, skills, memories, MCP servers and plugin list")
	agentPeersExportCmd.Flags().String("out", "", "Seal the pack into this file instead of printing JSON")
	agentPeersImportCmd.Flags().String("dir", "", "Where the checkouts go (default: the same place under home as there, else ~/dev)")
	agentPeersImportCmd.Flags().String("from", "", "A sealed file from export --out (default: stdin)")
	agentPeersImportCmd.Flags().Bool(skipPermissionsFlag, false, "Register every workspace with permission prompts off")
	for _, f := range []*cobra.Command{agentPeersBootstrapCmd} {
		f.Flags().StringSlice("workspace", nil, "Only these workspace ids")
		f.Flags().String("dir", "", "Where the checkouts go on the other machine (default: the same place under home as here)")
		f.Flags().Bool(withTokensFlag, false, "Copy this machine's tracker tokens and notification URL over ssh")
		f.Flags().Bool(withEnvFlag, false, "Copy each workspace's git-ignored .env files over ssh")
		f.Flags().Bool(withClaudeFlag, false, "Copy every Claude account's settings, skills, memories, MCP servers and plugins over ssh")
		f.Flags().Bool(skipPermissionsFlag, false, "Register the workspaces there with permission prompts off (a server)")
		f.Flags().Bool("lead", false, "The other machine leads every shared tracker")
		f.Flags().Bool("lan", false, "Serve on 0.0.0.0:8765 there instead of a tunnel")
		f.Flags().Bool("no-pair", false, "Set it up without pairing as peers")
		f.Flags().Bool(watchFlagDryRun, false, "Print the steps and send nothing")
	}
	agentPeersCmd.AddCommand(agentPeersExportCmd, agentPeersImportCmd, agentPeersBootstrapCmd)
}
