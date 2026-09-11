package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"github.com/spf13/cobra"
)

// harden writes the safe defaults into a workspace's local Claude settings
// — the ones every unattended run should have and nobody remembers to set:
// no reading of secrets, no destructive git or shell, and a hook that
// refuses to write a key into a file. Additive: it never removes a rule
// someone wrote, and it is silent about rules already there.

var hardenDeny = []string{
	"Read(./.env)",
	"Read(./.env.*)",
	"Read(./**/.env)",
	"Read(./**/.env.*)",
	"Read(./**/secrets/**)",
	"Read(./**/*.pem)",
	"Read(./**/*.key)",
	"Read(~/.ssh/**)",
	"Read(~/.aws/**)",
	"Bash(rm -rf *)",
	"Bash(sudo *)",
	"Bash(git push --force*)",
	"Bash(git push -f*)",
	"Bash(git reset --hard*)",
	"Bash(git clean -fd*)",
	"Bash(* --no-verify*)",
	"Bash(curl * | sh)",
	"Bash(curl * | bash)",
	"Bash(wget * | sh)",
}

const (
	hookSecrets       = "corgi agent hook secrets"
	trackMarkerSecret = "agent hook secrets"
)

var agentHardenCmd = &cobra.Command{
	Use:   "harden",
	Short: "Write the safe defaults into this workspace's Claude settings",
	Long: `Adds to .claude/settings.local.json in the workspace: deny rules for reading
secrets (.env, keys, ~/.ssh, ~/.aws) and for destructive shell and git
(rm -rf, sudo, force push, reset --hard, --no-verify, curl | sh), and a hook
that refuses to write a credential into a file. Nothing is removed; a rule
already there is left alone.

  corgi agent harden               # the workspace you are in
  corgi agent harden --dry-run     # show what would change
  corgi agent doctor --security    # what is still loose`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		cwd, _ := os.Getwd()
		root := scopeWorkspaceRoot(cwd)
		if root == "" {
			root = cwd
		}
		dry, _ := cmd.Flags().GetBool("dry-run")
		added, err := hardenSettings(claudeLocalSettingsPath(root), corgiCommandPath(), dry)
		if err != nil {
			exitWithError("agent_harden", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"path": claudeLocalSettingsPath(root), "added": added, "dryRun": dry})
			return
		}
		if len(added) == 0 {
			fmt.Printf("✓ %s already has every rule harden writes\n", claudeLocalSettingsPath(root))
			return
		}
		verb := "added"
		if dry {
			verb = "would add"
		}
		fmt.Printf("%s %d to %s:\n", verb, len(added), claudeLocalSettingsPath(root))
		for _, a := range added {
			fmt.Printf("  %s\n", a)
		}
		if !dry {
			fmt.Println("new sessions here pick it up; a running one after /reload or restart")
		}
	},
}

// hardenSettings merges the rules and the hook into a settings file and
// returns what it added.
func hardenSettings(path, bin string, dry bool) ([]string, error) {
	settings, err := readUserSettings(path)
	if err != nil {
		return nil, err
	}
	var added []string
	perms, _ := settings["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	deny, _ := perms["deny"].([]any)
	have := map[string]bool{}
	for _, d := range deny {
		if s, ok := d.(string); ok {
			have[s] = true
		}
	}
	for _, rule := range hardenDeny {
		if !have[rule] {
			deny = append(deny, rule)
			added = append(added, "deny "+rule)
		}
	}
	perms["deny"] = deny
	settings["permissions"] = perms

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	if !strings.Contains(marshalCompact(hooks["PreToolUse"]), trackMarkerSecret) {
		entries, _ := hooks["PreToolUse"].([]any)
		entries = append(entries, map[string]any{"matcher": writingTools, "hooks": []any{
			map[string]any{"type": "command", "command": hookCommand(bin, hookSecrets), "timeout": 5},
		}})
		hooks["PreToolUse"] = entries
		added = append(added, "hook PreToolUse "+writingTools+" → "+hookSecrets)
	}
	settings["hooks"] = hooks
	if dry || len(added) == 0 {
		return added, nil
	}
	return added, writeJSONObject(path, settings)
}

var (
	secretValue      = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|password|passwd|private[_-]?key)\s*[:=]\s*['"]?[^\s'"$<{]{12,}`)
	knownSecret      = regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{8,}|sk-[A-Za-z0-9]{32,}|ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,}|glpat-[A-Za-z0-9_-]{16,}|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY|lin_api_[A-Za-z0-9]{20,}`)
	placeholderValue = regexp.MustCompile(`(?i)(example|placeholder|changeme|your[_-]?|xxx+|\$\{|\bprocess\.env\b|os\.Getenv|<[^>]+>)`)
)

// runSecretsHook refuses a write whose new content carries a credential.
// The place for a key is the environment or a secret store, never a file a
// commit can pick up — and never a transcript.
func runSecretsHook(stdin io.Reader, stdout io.Writer) {
	data, err := io.ReadAll(io.LimitReader(stdin, 4<<20))
	if err != nil {
		return
	}
	var in struct {
		ToolInput struct {
			FilePath  string `json:"file_path"`
			Content   string `json:"content"`
			NewString string `json:"new_string"`
			Edits     []struct {
				NewString string `json:"new_string"`
			} `json:"edits"`
		} `json:"tool_input"`
	}
	if json.Unmarshal(data, &in) != nil {
		return
	}
	var texts []string
	texts = append(texts, in.ToolInput.Content, in.ToolInput.NewString)
	for _, e := range in.ToolInput.Edits {
		texts = append(texts, e.NewString)
	}
	for _, t := range texts {
		if t == "" {
			continue
		}
		for _, line := range strings.Split(t, "\n") {
			if placeholderValue.MatchString(line) {
				continue
			}
			if knownSecret.MatchString(line) || secretValue.MatchString(line) {
				_ = json.NewEncoder(stdout).Encode(map[string]any{
					"hookSpecificOutput": map[string]any{
						"hookEventName":            "PreToolUse",
						"permissionDecision":       "deny",
						"permissionDecisionReason": fmt.Sprintf("that write to %s carries what looks like a credential; keep it in the environment or a secret store and read it from there, never in a file", in.ToolInput.FilePath),
					},
				})
				return
			}
		}
	}
}

// Security checks for doctor: what a hardened machine has, and what this one
// is missing.

func checkSecurity() []agentCheck {
	var checks []agentCheck
	dir, err := agentDir()
	if err != nil {
		return checks
	}
	registry, err := workspace.Load(agentRegistryPath(dir))
	if err != nil {
		return checks
	}
	user, _ := config.LoadUser(agentUserConfigPath(dir))
	for _, ws := range registry.Workspaces {
		repo, _ := config.LoadRepo(ws.AbsPath)
		resolved := config.Resolve(ws.ID, repo, user)
		name := "security · " + ws.ID
		var loose []string
		settings, _ := readUserSettings(claudeLocalSettingsPath(ws.AbsPath))
		perms, _ := settings["permissions"].(map[string]any)
		text := marshalCompact(perms)
		if !strings.Contains(text, "Read(./.env)") {
			loose = append(loose, "no deny rules for secrets")
		}
		if !strings.Contains(marshalCompact(settings["hooks"]), trackMarkerSecret) {
			loose = append(loose, "no secrets hook")
		}
		if resolved.DangerouslySkipPermissions {
			loose = append(loose, "runs unattended with permissions off")
		}
		if resolved.Watch != nil && resolved.Watch.Action == "fix" && !resolved.Watch.Isolate {
			loose = append(loose, "unattended fixes run in the checkout, not a worktree (--isolate)")
		}
		if len(loose) == 0 {
			checks = append(checks, agentCheck{Name: name, OK: true, Detail: "hardened"})
			continue
		}
		checks = append(checks, agentCheck{Name: name, OK: !resolved.DangerouslySkipPermissions || len(loose) == 1, Detail: strings.Join(loose, "; "),
			Fix: "`corgi agent harden` in " + ws.AbsPath})
	}
	if os.Getenv("CORGI_MCP_ALLOW_DANGEROUS_TUNNEL") != "" {
		checks = append(checks, agentCheck{Name: "security · tunnel", Detail: "CORGI_MCP_ALLOW_DANGEROUS_TUNNEL is set: exec and database tools answer over a public tunnel", Fix: "unset it unless the tunnel is private (Tailscale, LAN)"})
	} else {
		checks = append(checks, agentCheck{Name: "security · tunnel", OK: true, Detail: "dangerous tools stay off a public tunnel"})
	}
	return checks
}

func init() {
	agentHardenCmd.Flags().Bool("dry-run", false, "show what would change without writing")
	agentCmd.AddCommand(agentHardenCmd)
}
