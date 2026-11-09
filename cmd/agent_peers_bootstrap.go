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

type peerPack struct {
	From       string          `json:"from"`
	At         time.Time       `json:"at"`
	TZ         string          `json:"tz,omitempty"`
	GitName    string          `json:"gitName,omitempty"`
	GitEmail   string          `json:"gitEmail,omitempty"`
	Workspaces []packWorkspace `json:"workspaces"`
	Secrets    *watch.Secrets  `json:"secrets,omitempty"`
}

type packWorkspace struct {
	ID      string                  `json:"id"`
	Remote  string                  `json:"remote"`
	Compose bool                    `json:"compose,omitempty"`
	Aliases []string                `json:"aliases,omitempty"`
	Config  *config.WorkspaceConfig `json:"config,omitempty"`
	Secrets *watch.Secrets          `json:"secrets,omitempty"`
}

func (p peerPack) hasTokens() bool {
	if p.Secrets != nil && !p.Secrets.IsZero() {
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

func buildPeerPack(dir string, only []string, withTokens bool) (peerPack, error) {
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return peerPack{}, err
	}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil {
		return peerPack{}, err
	}
	pack := peerPack{From: peers.Me(), At: time.Now(), TZ: os.Getenv("TZ"),
		GitName: gitOutput(".", "config", "--global", "user.name"), GitEmail: gitOutput(".", "config", "--global", "user.email")}
	if pack.TZ == "" {
		if zone, err := os.Readlink("/etc/localtime"); err == nil {
			if i := strings.Index(zone, "zoneinfo/"); i >= 0 {
				pack.TZ = zone[i+len("zoneinfo/"):]
			}
		}
	}
	for _, ws := range registry.Workspaces {
		if len(only) > 0 && !containsFold(only, ws.ID) {
			continue
		}
		remote := gitOutput(ws.AbsPath, "remote", "get-url", "origin")
		if remote == "" {
			if len(only) > 0 {
				return peerPack{}, fmt.Errorf("%s has no git remote to clone from", ws.ID)
			}
			continue
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
		pack.Workspaces = append(pack.Workspaces, pw)
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

type importOptions struct {
	base      string
	skipPerms bool
	clone     func(remote, target string) error
	stackInit func(target string) error
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

func applyPeerPack(dir string, pack peerPack, opt importOptions) ([]string, error) {
	if opt.clone == nil {
		opt.clone = defaultClone
	}
	if opt.stackInit == nil {
		opt.stackInit = defaultStackInit
	}
	if opt.base == "" {
		home, _ := os.UserHomeDir()
		opt.base = filepath.Join(home, "dev")
	}
	var done []string
	if pack.GitName != "" && gitOutput(".", "config", "--global", "user.name") == "" {
		_ = exec.Command("git", "config", "--global", "user.name", pack.GitName).Run()
		_ = exec.Command("git", "config", "--global", "user.email", pack.GitEmail).Run()
		done = append(done, "git identity: "+pack.GitName)
	}
	if pack.Secrets != nil && watch.LoadSecrets(dir).IsZero() {
		if err := watch.SaveSecrets(dir, *pack.Secrets); err != nil {
			return done, err
		}
		done = append(done, "machine tokens")
	}
	for _, w := range pack.Workspaces {
		target := filepath.Join(opt.base, w.ID)
		if _, err := os.Stat(filepath.Join(target, ".git")); err != nil {
			if err := os.MkdirAll(opt.base, 0o755); err != nil {
				return done, err
			}
			if err := opt.clone(w.Remote, target); err != nil {
				return done, fmt.Errorf("%s: clone %s: %w", w.ID, w.Remote, err)
			}
			done = append(done, w.ID+": cloned")
		}
		if w.Compose {
			if _, err := os.Stat(filepath.Join(target, ".corgi", "corgi_services")); err != nil {
				if err := opt.stackInit(target); err != nil {
					return done, fmt.Errorf("%s: corgi init: %w", w.ID, err)
				}
				done = append(done, w.ID+": services cloned")
			}
		}
		agents := []string(nil)
		if w.Config != nil {
			agents = w.Config.Agents
		}
		if _, err := registerWorkspace(target, w.ID, w.Aliases, "", false, opt.skipPerms, agents); err != nil {
			return done, fmt.Errorf("%s: %w", w.ID, err)
		}
		if w.Config != nil {
			path := agentUserConfigPath(dir)
			user, err := config.LoadUser(path)
			if err != nil {
				return done, err
			}
			entry := user.Workspaces[w.ID]
			entry.Kind, entry.Capacity, entry.Watch, entry.Models, entry.Routines = w.Config.Kind, w.Config.Capacity, w.Config.Watch, w.Config.Models, w.Config.Routines
			user.Workspaces[w.ID] = entry
			if err := writeUserConfig(path, user); err != nil {
				return done, err
			}
		}
		if w.Secrets != nil {
			if err := watch.SaveWorkspaceSecrets(dir, w.ID, *w.Secrets); err != nil {
				return done, err
			}
		}
		done = append(done, w.ID+": registered"+map[bool]string{true: " with its watch", false: ""}[w.Config != nil && w.Config.Watch != nil])
	}
	return done, nil
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
	importCmd := "corgi agent peers import --dir " + shellQuote(opt.base)
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
		{name: "daemon at login", script: remotePath + up + " >/dev/null 2>&1; corgi agent status --json | head -c 4000"},
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
	pack, err := buildPeerPack(dir, opt.only, opt.withTokens)
	if err != nil {
		return err
	}
	packJSON, _ := json.Marshal(pack)
	say := func(format string, a ...any) { fmt.Fprintf(opt.out, format+"\n", a...) }
	if opt.dryRun {
		say("would send %d workspace(s) to %s (tokens: %v):", len(pack.Workspaces), opt.host, pack.hasTokens())
		for _, w := range pack.Workspaces {
			say("  %-16s %s", w.ID, w.Remote)
		}
		for _, s := range bootstrapSteps(pack, packJSON, opt) {
			say("  step %s: ssh %s %s", s.name, opt.host, firstLineOf(s.script))
		}
		if opt.pair {
			say("  step pair: corgi agent peers invite here, then ssh %s corgi agent peers join <url> <code>", opt.host)
		}
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
		if _, err := opt.run(ctx, s.script, s.stdin); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}
	if opt.pair {
		say("→ pairing as peers")
		url, code, err := opt.invite()
		if err != nil {
			return fmt.Errorf("pair window here: %w", err)
		}
		if _, err := opt.run(ctx, remotePath+"corgi agent peers join "+shellQuote(url)+" "+shellQuote(code), nil); err != nil {
			return fmt.Errorf("peers join from %s: %w (the other side needs a URL this machine can reach: a tunnel, or --lan on one network)", opt.host, err)
		}
		if opt.lead {
			if _, err := opt.run(ctx, remotePath+"corgi agent peers lead", nil); err != nil {
				return fmt.Errorf("peers lead: %w", err)
			}
			say("%s leads the shared trackers", opt.host)
		}
	}
	say("")
	say("left to do on %s, by hand:", opt.host)
	say("  ssh %s claude auth login        # prints a URL to open anywhere and a code to paste back", opt.host)
	say("  ssh %s gh auth login             # or glab auth login --token …; a run opens pull requests with it", opt.host)
	if !pack.hasTokens() {
		say("  ssh %s corgi agent watch auth <tracker> --token … --workspace <id>   # or run again with --with-tokens", opt.host)
	}
	say("  a stable URL: corgi agent tunnel setup <host> there (the quick tunnel changes on restart and un-pairs)")
	say("  ssh %s corgi agent doctor        # the rest, named", opt.host)
	return nil
}

var agentPeersExportCmd = &cobra.Command{
	Use:   "export",
	Short: "This machine's workspaces as a pack another machine imports",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		only, _ := cmd.Flags().GetStringSlice("workspace")
		withTokens, _ := cmd.Flags().GetBool("with-tokens")
		pack, err := buildPeerPack(dir, only, withTokens)
		if err != nil {
			exitWithError("agent_peers", err, 2)
			return
		}
		utils.PrintJSON(pack)
	},
}

var agentPeersImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Clone and register the workspaces of a pack read from stdin",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		base, _ := cmd.Flags().GetString("dir")
		skipPerms, _ := cmd.Flags().GetBool("dangerously-skip-permissions")
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			exitWithError("agent_peers", err, 1)
			return
		}
		var pack peerPack
		if err := json.Unmarshal(raw, &pack); err != nil || len(pack.Workspaces) == 0 {
			exitWithError("agent_peers", errors.New("stdin is not a pack: corgi agent peers export --json | ssh <host> corgi agent peers import"), 2)
			return
		}
		done, err := applyPeerPack(dir, pack, importOptions{base: base, skipPerms: skipPerms})
		if utils.JSONOutput {
			out := map[string]any{"done": done, "from": pack.From}
			if err != nil {
				out["error"] = err.Error()
			}
			utils.PrintJSON(out)
		} else {
			for _, d := range done {
				fmt.Println(d)
			}
		}
		if err != nil {
			exitWithError("agent_peers", err, 1)
		}
	},
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
		withTokens, _ := cmd.Flags().GetBool("with-tokens")
		skipPerms, _ := cmd.Flags().GetBool("dangerously-skip-permissions")
		lead, _ := cmd.Flags().GetBool("lead")
		lan, _ := cmd.Flags().GetBool("lan")
		noPair, _ := cmd.Flags().GetBool("no-pair")
		dry, _ := cmd.Flags().GetBool(watchFlagDryRun)
		opt := bootstrapOptions{host: args[0], base: base, only: only, withTokens: withTokens, skipPerms: skipPerms, lead: lead, lan: lan, pair: !noPair, dryRun: dry,
			run: sshRunner(args[0]), invite: func() (string, string, error) {
				ans, err := requestPairWindowFor(dir, pairing.RolePeer, 6*time.Second)
				if err != nil {
					return "", "", err
				}
				return firstNonEmpty(ans.PublicURL, ans.LocalURL), ans.Code, nil
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
	agentPeersExportCmd.Flags().Bool("with-tokens", false, "Include the tracker tokens")
	agentPeersImportCmd.Flags().String("dir", "", "Where the checkouts go (default ~/dev)")
	agentPeersImportCmd.Flags().Bool("dangerously-skip-permissions", false, "Register every workspace with permission prompts off")
	for _, f := range []*cobra.Command{agentPeersBootstrapCmd} {
		f.Flags().StringSlice("workspace", nil, "Only these workspace ids")
		f.Flags().String("dir", "~/dev", "Where the checkouts go on the other machine")
		f.Flags().Bool("with-tokens", false, "Copy this machine's tracker tokens over ssh")
		f.Flags().Bool("dangerously-skip-permissions", false, "Register the workspaces there with permission prompts off (a server)")
		f.Flags().Bool("lead", false, "The other machine leads every shared tracker")
		f.Flags().Bool("lan", false, "Serve on 0.0.0.0:8765 there instead of a tunnel")
		f.Flags().Bool("no-pair", false, "Set it up without pairing as peers")
		f.Flags().Bool(watchFlagDryRun, false, "Print the steps and send nothing")
	}
	agentPeersCmd.AddCommand(agentPeersExportCmd, agentPeersImportCmd, agentPeersBootstrapCmd)
}
