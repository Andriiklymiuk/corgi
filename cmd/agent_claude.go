package cmd

import (
	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/harness"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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

type claudeLaunch struct {
	Workspace string
	Kind      string
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
		kindOverride, _ = cmd.Flags().GetString("kind")
		if kindOverride != "" && !harness.Known(kindOverride) {
			exitWithError("agent_claude", fmt.Errorf("--kind is one of %s", strings.Join(harness.Names(), ", ")), 2)
		}
		show, _ := cmd.Flags().GetBool("show")
		model, _ := cmd.Flags().GetString("model")
		promptID, _ := cmd.Flags().GetString("prompt-id")
		ticket, _ := cmd.Flags().GetString("ticket")
		ticketKey, _ := cmd.Flags().GetString("ticket-key")
		wanted, _ := cmd.Flags().GetString("workspace")
		isolate, _ := cmd.Flags().GetBool("isolate")
		attempt, _ := cmd.Flags().GetInt("attempt")
		if attempt > 0 {
			isolate = true
		}
		botName, _ := cmd.Flags().GetString("bot")
		cwd, err := os.Getwd()
		if err != nil {
			exitWithError("agent_claude", err, 1)
		}
		var bot *bots.Bot
		if botName != "" {
			b, err := loadBot(botName)
			if err != nil {
				exitWithError("agent_claude", err, 2)
			}
			bot = &b
			if wanted == "" {
				wanted = b.Workspace
			}
			if profile == "" {
				profile = b.Profile
			}
			if model == "" {
				model = b.Model
			}
			if !cmd.Flags().Changed("isolate") {
				isolate = b.Isolate
			}
		}
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
		var isolation string
		if isolate {
			ref := isolationRef(ticket, time.Now())
			if attempt > 0 {
				ref += "-" + strconv.Itoa(attempt)
			}
			branch := daemon.FixBranch(ref)
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
		if bot != nil {
			if soul := strings.TrimSpace(bot.Soul); soul != "" {
				args = append([]string{"--append-system-prompt", soul}, args...)
			}
		}
		launch, err := resolveClaudeLaunch(cwd, profile, args)
		if err != nil {
			exitWithError("agent_claude", err, 2)
		}
		if bot != nil && bot.LastSession != "" && !hasFlag(launch.Args, "--resume") && !hasFlag(launch.Args, "--continue") {
			if transcriptExists(launch.Env, cwd, bot.LastSession) {
				launch.Args = append([]string{"--resume", bot.LastSession}, launch.Args...)
				utils.Info(fmt.Sprintf("corgi: %s picks up where it left off", bot.Display()))
			}
		}
		if show {
			fmt.Println(launch.String())
			return
		}
		if launch.Workspace != "" {
			word := "claude"
			if launch.Kind == supervisor.KindCodex {
				word = "codex"
			}
			utils.Info(fmt.Sprintf("corgi: %s for %s%s", word, launch.Workspace, launch.accountSuffix()))
		}
		env := os.Environ()
		for k, v := range launch.Env {
			env = append(env, k+"="+v)
		}
		if t := strings.TrimSpace(ticket); t != "" {
			env = append(env, "CORGI_TICKET="+t)
			if k := strings.TrimSpace(ticketKey); k != "" {
				env = append(env, "CORGI_TICKET_KEY="+k)
			}
		}
		if isolate {
			env = append(env, "CORGI_ISOLATED=1")
		}
		if attempt > 0 {
			env = append(env, "CORGI_ATTEMPT="+isolationRef(ticket, time.Now())+"/"+strconv.Itoa(attempt))
		}
		if bot != nil {
			env = append(env, "CORGI_BOT="+bot.Name)
		}
		if err := runClaudeInPlace(launch.Bin, launch.Args, env); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				os.Exit(exit.ExitCode())
			}
			exitWithError("agent_claude", err, 1)
		}
	},
}

func loadBot(name string) (bots.Bot, error) {
	dir, err := agentDir()
	if err != nil {
		return bots.Bot{}, err
	}
	store, err := bots.Load(bots.Path(dir))
	if err != nil {
		return bots.Bot{}, err
	}
	b, ok := store.Find(name)
	if !ok {
		return bots.Bot{}, fmt.Errorf("no bot named %q — corgi agent bot list", name)
	}
	return b, nil
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func transcriptExists(env map[string]string, cwd, sessionID string) bool {
	path := usage.TranscriptPath(env["CLAUDE_CONFIG_DIR"], cwd, sessionID)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

var execProcess = syscall.Exec

func runClaudeInPlace(bin string, args []string, env []string) error {
	path, err := exec.LookPath(bin)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := execProcess(path, append([]string{bin}, args...), env); err == nil {
			return nil
		}
	}
	c := exec.Command(path, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	c.Env = env
	return c.Run()
}

// kindOverride is --kind on the command line: one session under another
// harness than the workspace's config names.
var kindOverride string

func resolveClaudeLaunch(dir, profile string, extra []string) (claudeLaunch, error) {
	launch := claudeLaunch{Bin: harness.For(kindOverride, "").Bin, Args: append([]string(nil), extra...), Env: map[string]string{}}
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
	if k := strings.TrimSpace(kindOverride); k != "" {
		resolved.Kind = k
	}
	kind, err := supervisor.KindFor(supervisor.SpawnConfig{Kind: resolved.Kind, ConfigDirEnv: resolved.ConfigDirEnv, CredentialEnv: resolved.CredentialEnv})
	if err != nil {
		return launch, err
	}
	launch.Kind = kind.Name
	if bin := strings.TrimSpace(resolved.Bin); bin != "" {
		launch.Bin = expandTilde(bin)
	} else if kind.DefaultBin != "" {
		launch.Bin = kind.DefaultBin
	}
	if kind.Name == supervisor.KindCustom {
		launch.Args = append(append([]string(nil), resolved.Args...), extra...)
	} else {
		launch.Args = append(harness.For(kind.Name, "").InteractiveArgs(resolved.PermissionMode), extra...)
	}
	if cfg := strings.TrimSpace(resolved.ConfigDir); cfg != "" && kind.ConfigDirEnv != "" {
		launch.Env[kind.ConfigDirEnv] = expandTilde(cfg)
	}
	return launch, nil
}

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
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	return filepath.Clean(p)
}

func (l claudeLaunch) accountSuffix() string {
	for k, v := range l.Env {
		return " under " + v + " (" + k + ")"
	}
	return ""
}

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
	agentClaudeCmd.Flags().String("kind", "", "Open this harness instead of the workspace's (claude, codex)")
	agentClaudeCmd.Flags().String("model", "", "Pass --model to claude (opus, sonnet, haiku, or a model id)")
	agentClaudeCmd.Flags().String("prompt-id", "", "Start with the prompt saved under this id by the phone launcher; the file is read once and removed")
	agentClaudeCmd.Flags().String("ticket", "", "The tracker ref(s) this session works on (ABC-1 or ABC-1,ABC-2): the board shows it on the ticket")
	agentClaudeCmd.Flags().String("ticket-key", "", "The inbox key of that ticket, with --ticket")
	agentClaudeCmd.Flags().Int("attempt", 0, "This session is attempt N of several on the same ticket (corgi agent watch work --attempts): its own worktree on corgi/<ticket>-N, and the board groups them")
	agentClaudeCmd.Flags().Bool("isolate", false, "Start in a worktree of its own on a corgi/<ticket> branch — every repository of the stack gets one — so this session never touches your checkout")
	agentClaudeCmd.Flags().String("bot", "", "Open as this bot (corgi agent bot list): its workspace, account, model and persona, resuming its last conversation")
	agentClaudeCmd.Flags().Bool("show", false, "Print the resolved command and exit")
	agentCmd.AddCommand(agentClaudeCmd)
}

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

const autoSwitchAt = 98

func autoProfilePath(agentDir string) string { return filepath.Join(agentDir, "auto-profile") }

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

var (
	promptIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	modelPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
)

const promptMaxAge = 10 * time.Minute

func validModel(model string) bool { return modelPattern.MatchString(model) }

func promptsDir(agentDir string) string { return filepath.Join(agentDir, "prompts") }

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

func isolationRef(ticket string, now time.Time) string {
	if first := strings.TrimSpace(strings.Split(ticket, ",")[0]); first != "" {
		return first
	}
	return "session-" + now.Format("0102-1504")
}

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
