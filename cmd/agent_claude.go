package cmd

import (
	"andriiklymiuk/corgi/utils/agent/daemon"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/usage"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// claudeLaunch is the command line `corgi agent claude` runs: the binary,
// its arguments and the variables that pick the account.
type claudeLaunch struct {
	Workspace string
	Bin       string
	Args      []string
	Env       map[string]string
}

var agentClaudeCmd = &cobra.Command{
	Use:   "claude [-- claude args]",
	Short: "Run Claude Code the way this folder's workspace is configured",
	Long: `Starts Claude Code for the workspace the current directory belongs to, under
that workspace's account (configDir), binary and permission mode from the
corgi agent config — the same settings a remote session gets. Outside every
workspace it is plain claude. So one command replaces per-account aliases:
the corgi VS Code extension's "+" key runs it in a new terminal.

  corgi agent claude                 # this folder's workspace
  corgi agent claude --workspace api # that workspace, whatever folder you are in
  corgi agent claude --profile work  # under a corgi profile
  corgi agent claude --profile auto  # the listed account with most budget left
  corgi agent claude --model auto    # the workspace's models: policy, else opusplan
  corgi agent claude --show          # print the command instead of running it
  corgi agent claude -- --resume     # arguments after -- go to claude`,
	Run: func(cmd *cobra.Command, args []string) {
		profile, _ := cmd.Flags().GetString("profile")
		show, _ := cmd.Flags().GetBool("show")
		model, _ := cmd.Flags().GetString("model")
		promptID, _ := cmd.Flags().GetString("prompt-id")
		ticket, _ := cmd.Flags().GetString("ticket")
		ticketKey, _ := cmd.Flags().GetString("ticket-key")
		wanted, _ := cmd.Flags().GetString("workspace")
		isolate, _ := cmd.Flags().GetBool("isolate")
		cwd, err := os.Getwd()
		if err != nil {
			exitWithError("agent_claude", err, 1)
		}
		// A caller with no cwd of its own — the phone, a menu bar — names the
		// workspace instead. Without this the session lands in whichever
		// checkout the terminal happened to be in, under that one's account.
		if id := strings.TrimSpace(wanted); id != "" {
			root, err := workspaceRoot(id)
			if err != nil {
				exitWithError("agent_claude", err, 2)
			}
			if err := os.Chdir(root); err != nil {
				exitWithError("agent_claude", err, 1)
			}
			cwd = root
		}
		// Its own worktree: one session, one branch, one checkout nobody
		// else is in. The ticket names the branch; without one, the minute
		// does. Every repository of the stack gets one, and the session
		// starts in the workspace's own.
		var isolation string
		if isolate {
			branch := daemon.FixBranch(isolationRef(ticket, time.Now()))
			trees, start, err := isolateWorkspace(cwd, branch)
			if err != nil {
				exitWithError("agent_claude", fmt.Errorf("could not isolate: %v", err), 2)
			}
			if err := os.Chdir(start); err != nil {
				exitWithError("agent_claude", err, 1)
			}
			cwd = start
			isolation = daemon.IsolationNote(branch, trees)
			utils.Info(fmt.Sprintf("corgi: isolated on %s in %s", branch, start))
		}
		if strings.EqualFold(strings.TrimSpace(model), "auto") {
			model = autoModelFor(cwd)
		}
		if model != "" {
			if !validModel(model) {
				exitWithError("agent_claude", fmt.Errorf("model %q: letters, digits, dots and dashes only", model), 2)
			}
			args = append([]string{"--model", model}, args...)
		}
		if promptID != "" {
			dir, err := agentDir()
			if err != nil {
				exitWithError("agent_claude", err, 1)
			}
			text, err := takePrompt(dir, promptID)
			if err != nil {
				exitWithError("agent_claude", err, 2)
			}
			args = append(args, text+isolation)
		}
		launch, err := resolveClaudeLaunch(cwd, profile, args)
		if err != nil {
			exitWithError("agent_claude", err, 2)
		}
		if show {
			fmt.Println(launch.String())
			return
		}
		if launch.Workspace != "" {
			utils.Info(fmt.Sprintf("corgi: claude for %s%s", launch.Workspace, launch.accountSuffix()))
		}
		env := os.Environ()
		for k, v := range launch.Env {
			env = append(env, k+"="+v)
		}
		// The ticket rides in the environment: the tracking hook, a child of
		// claude, reads it and the board shows a session on the ticket from
		// its first event — before any branch is named after it.
		if t := strings.TrimSpace(ticket); t != "" {
			env = append(env, "CORGI_TICKET="+t)
			if k := strings.TrimSpace(ticketKey); k != "" {
				env = append(env, "CORGI_TICKET_KEY="+k)
			}
		}
		if isolate {
			env = append(env, "CORGI_ISOLATED=1")
		}
		if err := runClaudeInPlace(launch.Bin, launch.Args, env); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				os.Exit(exit.ExitCode())
			}
			exitWithError("agent_claude", err, 1)
		}
	},
}

// execProcess is syscall.Exec, swappable so tests can watch the exec path.
var execProcess = syscall.Exec

// runClaudeInPlace replaces this process with claude where the OS allows,
// so claude's parent is the shell, not corgi. The tracking hook skips any
// claude with a corgi ancestor — that is how the daemon's own remote-control
// sessions stay off the board — and a claude run as corgi's child would be
// skipped the same way: started from the "+" key, never on a key. Where exec
// is unavailable it runs as a child and returns when claude exits.
func runClaudeInPlace(bin string, args []string, env []string) error {
	path, err := exec.LookPath(bin)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := execProcess(path, append([]string{bin}, args...), env); err == nil {
			return nil
		}
		// Exec refused (a script without a shebang, say): fall through.
	}
	c := exec.Command(path, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	c.Env = env
	return c.Run()
}

// resolveClaudeLaunch picks the workspace whose path contains dir (the
// deepest one when they nest) and turns its resolved config into a command.
func resolveClaudeLaunch(dir, profile string, extra []string) (claudeLaunch, error) {
	launch := claudeLaunch{Bin: "claude", Args: append([]string(nil), extra...), Env: map[string]string{}}
	registry, _, err := agentRegistry()
	if err != nil {
		return launch, nil
	}
	agentD, err := agentDir()
	if err != nil {
		return launch, nil
	}
	dir = cleanPath(dir)
	var id, best string
	for _, ws := range registry.Sorted() {
		root := cleanPath(ws.AbsPath)
		if root == "" || (dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator))) {
			continue
		}
		if len(root) > len(best) {
			id, best = ws.ID, root
		}
	}
	if id == "" {
		return launch, nil
	}
	launch.Workspace = id
	user, err := config.LoadUser(agentUserConfigPath(agentD))
	if err != nil {
		return launch, err
	}
	repo, _ := config.LoadRepo(best)
	resolved := config.Resolve(id, repo, user)
	if strings.EqualFold(strings.TrimSpace(profile), "auto") {
		profile = pickAccountProfileSticky(agentD, user, resolved)
	}
	if p := strings.TrimSpace(profile); p != "" {
		withProfile, err := config.ApplyProfile(resolved, user, p)
		if err != nil {
			return launch, err
		}
		resolved = withProfile
	}
	kind, err := supervisor.KindFor(supervisor.SpawnConfig{Kind: resolved.Kind, ConfigDirEnv: resolved.ConfigDirEnv, CredentialEnv: resolved.CredentialEnv})
	if err != nil {
		return launch, err
	}
	if bin := strings.TrimSpace(resolved.Bin); bin != "" {
		launch.Bin = expandTilde(bin)
	} else if kind.DefaultBin != "" {
		launch.Bin = kind.DefaultBin
	}
	if kind.Name == supervisor.KindCustom {
		launch.Args = append(append([]string(nil), resolved.Args...), extra...)
	} else if mode := strings.TrimSpace(resolved.PermissionMode); mode != "" {
		launch.Args = append([]string{"--permission-mode", mode}, extra...)
	}
	if cfg := strings.TrimSpace(resolved.ConfigDir); cfg != "" && kind.ConfigDirEnv != "" {
		launch.Env[kind.ConfigDirEnv] = expandTilde(cfg)
	}
	return launch, nil
}

// workspaceRoot is a registered workspace's checkout, by id.
func workspaceRoot(id string) (string, error) {
	registry, _, err := agentRegistry()
	if err != nil {
		return "", err
	}
	ws, ok := registry.Find(id)
	if !ok {
		return "", fmt.Errorf("%q is not a registered workspace — `corgi agent init` there first", id)
	}
	root := expandTilde(strings.TrimSpace(ws.AbsPath))
	if root == "" {
		return "", fmt.Errorf("workspace %q has no path on this machine", id)
	}
	if _, err := os.Stat(root); err != nil {
		return "", fmt.Errorf("workspace %q is registered at %s, which is not there", id, root)
	}
	return root, nil
}

func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(expandTilde(p)); err == nil {
		p = abs
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Clean(p)
}

func (l claudeLaunch) accountSuffix() string {
	for k, v := range l.Env {
		return " under " + v + " (" + k + ")"
	}
	return ""
}

// String is the shell form, for --show and for a terminal to run.
func (l claudeLaunch) String() string {
	parts := make([]string, 0, len(l.Env)+1+len(l.Args))
	for k, v := range l.Env {
		parts = append(parts, k+"="+shellQuote(v))
	}
	parts = append(parts, shellQuote(l.Bin))
	for _, a := range l.Args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"$`\\!*?[]{}()<>|;&") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func init() {
	agentClaudeCmd.Flags().String("workspace", "", "Start in this registered workspace's checkout, under its account, whatever folder you are in")
	agentClaudeCmd.Flags().String("profile", "", "Run under this corgi profile's account and settings")
	agentClaudeCmd.Flags().String("model", "", "Pass --model to claude (opus, sonnet, haiku, or a model id)")
	agentClaudeCmd.Flags().String("prompt-id", "", "Start with the prompt saved under this id by the phone launcher; the file is read once and removed")
	agentClaudeCmd.Flags().String("ticket", "", "The tracker ref(s) this session works on (ABC-1 or ABC-1,ABC-2): the board shows it on the ticket")
	agentClaudeCmd.Flags().String("ticket-key", "", "The inbox key of that ticket, with --ticket")
	agentClaudeCmd.Flags().Bool("isolate", false, "Start in a worktree of its own on a corgi/<ticket> branch — every repository of the stack gets one — so this session never touches your checkout")
	agentClaudeCmd.Flags().Bool("show", false, "Print the resolved command and exit")
	agentCmd.AddCommand(agentClaudeCmd)
}

// pickAccountProfile is --profile auto: among the profiles the workspace's
// accounts: list allows, the one whose 5-hour window has the most room, by
// the numbers Claude Code last fetched. "" keeps the workspace's own account
// — a workspace that lists nothing never switches — and a profile with no
// snapshot yet counts as full, so an account nobody has used is tried last.
func pickAccountProfile(user *config.UserConfig, resolved config.Resolved) string {
	if user == nil || len(resolved.Accounts) == 0 {
		return ""
	}
	best, bestLeft := "", -1
	for _, name := range resolved.Accounts {
		name = strings.TrimSpace(name)
		p, ok := user.Profiles[name]
		if !ok && name != "default" {
			continue
		}
		left := 0
		if l, ok := usage.ReadLimits(expandTilde(p.ConfigDir)); ok {
			left = 100 - l.FiveHour.Percent
			if l.FiveHour.Percent >= 100 {
				left = 0
			}
		}
		if left > bestLeft {
			best, bestLeft = name, left
		}
	}
	if best == "default" {
		return ""
	}
	return best
}

// autoModelFor is --model auto: the workspace's policy, else opusplan —
// Opus to plan, Sonnet to execute, in one session.
func autoModelFor(cwd string) string {
	dir := agentDirOrEmpty()
	if dir == "" {
		return config.ModelAutoDefault
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return config.ModelAutoDefault
	}
	id, _ := workspaceLabel(registry, cwd)
	resolved, err := resolveWorkspaceConfig(dir, id)
	if err != nil {
		return config.ModelAutoDefault
	}
	return resolved.Models.ForAuto()
}

// autoSwitchAt is the reading past which --profile auto leaves the account
// it has been using. Not sooner: a session pinned to an account with room
// left should stay there, and bouncing between accounts on every small
// difference is how two windows end up limited at once.
const autoSwitchAt = 98

func autoProfilePath(agentDir string) string { return filepath.Join(agentDir, "auto-profile") }

// pickAccountProfileSticky is --profile auto with a memory: the account it
// picked last time stays picked until its window is nearly spent, then the
// one with the most room takes over and stays even when the first recovers.
func pickAccountProfileSticky(agentDir string, user *config.UserConfig, resolved config.Resolved) string {
	if user == nil || len(resolved.Accounts) == 0 {
		return ""
	}
	if last, err := os.ReadFile(autoProfilePath(agentDir)); err == nil {
		name := strings.TrimSpace(string(last))
		if name != "" && accountAllowed(resolved.Accounts, name) {
			if p, ok := user.Profiles[name]; ok || name == "default" {
				if l, ok := usage.ReadLimits(expandTilde(p.ConfigDir)); !ok || l.FiveHour.Percent < autoSwitchAt {
					if name == "default" {
						return ""
					}
					return name
				}
			}
		}
	}
	pick := pickAccountProfile(user, resolved)
	remember := pick
	if remember == "" {
		remember = "default"
	}
	_ = os.WriteFile(autoProfilePath(agentDir), []byte(remember+"\n"), 0o600)
	return pick
}

// A prompt typed on the phone reaches the new terminal by id, never inside
// the shell line: the launcher writes it under prompts/ with 0600, the new
// session reads it once and removes it. Ids are random and short-lived.

var (
	promptIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	modelPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
)

const promptMaxAge = 10 * time.Minute

func validModel(model string) bool { return modelPattern.MatchString(model) }

func promptsDir(agentDir string) string { return filepath.Join(agentDir, "prompts") }

// savePrompt stores text for a new session and returns its id. Files older
// than promptMaxAge are removed on the way, so a prompt nobody picked up
// does not linger.
func savePrompt(agentDir, text string) (string, error) {
	dir := promptsDir(agentDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > promptMaxAge {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(raw[:])
	if err := os.WriteFile(filepath.Join(dir, id), []byte(text), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

// takePrompt reads a saved prompt and removes it.
func takePrompt(agentDir, id string) (string, error) {
	if !promptIDPattern.MatchString(id) {
		return "", fmt.Errorf("prompt id %q is not one the launcher makes", id)
	}
	path := filepath.Join(promptsDir(agentDir), id)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("no saved prompt %s: it was already used or expired", id)
	}
	if time.Since(info.ModTime()) > promptMaxAge {
		_ = os.Remove(path)
		return "", fmt.Errorf("saved prompt %s expired", id)
	}
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// isolationRef is what names an isolated session's branch: the first
// ticket it works on, else the minute it started.
func isolationRef(ticket string, now time.Time) string {
	if first := strings.TrimSpace(strings.Split(ticket, ",")[0]); first != "" {
		return first
	}
	return "session-" + now.Format("0102-1504")
}

// isolateWorkspace gives the workspace at root a worktree per repository on
// branch, and picks the one to start in: the repository root itself when it
// is one, else the first. A workspace with a corgi-compose.yml gets one per
// service repository; a bare checkout gets its own.
func isolateWorkspace(root, branch string) (trees []string, start string, err error) {
	hasCompose := false
	for _, name := range []string{utils.CorgiComposeDefaultName, "corgi-compose.yaml"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			hasCompose = true
			break
		}
	}
	if hasCompose {
		trees, err = isolateFixWorktrees(root, branch)
		if err == nil && len(trees) > 0 {
			// The stack's own repository, when the compose file sits in one,
			// is where the session should start; a service's otherwise.
			start = trees[0]
			if repo, ok := utils.RepoRootOf(root); ok && repo != "" {
				prefix := utils.WorktreeDirPrefix(repo) + "@"
				for _, t := range trees {
					if strings.HasPrefix(filepath.Base(t), prefix) {
						start = t
						break
					}
				}
			}
		}
		return trees, start, err
	}
	dir, err := utils.MaterializeBranchInRepo(root, branch)
	if err != nil {
		return nil, "", err
	}
	return []string{dir}, dir, nil
}
