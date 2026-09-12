package cmd

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"

	"github.com/spf13/cobra"
)

// A bot is a session you come back to: a workspace, a persona, a model and
// an account under one name, with the conversation it last had. "Open the
// reviewer" starts Claude Code there, as that, and picks the thread up.

var agentBotCmd = &cobra.Command{
	Use:   "bot",
	Short: "Named sessions you come back to: a workspace, a persona, a model, a thread",
	Long: `A bot is who you talk to; a session is the process. Give a bot a name,
a workspace, a persona (its "soul", appended to Claude Code's system
prompt), a model and an account, and open it from the phone, the menu bar,
the editor or here: it starts in that workspace as that persona and resumes
the conversation it last had.

  corgi agent bot add reviewer --workspace api --title "Code Reviewer" \
      --soul "You review pull requests. Read the diff, run the tests, be brief." \
      --model opus --profile work --color orange
  corgi agent bot open reviewer          # in the editor window in front
  corgi agent claude --bot reviewer      # in this terminal`,
}

var agentBotAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add or replace a bot",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := strings.ToLower(strings.TrimSpace(args[0]))
		if !bots.ValidName(name) {
			exitWithError("agent_bot", fmt.Errorf("a bot name is lowercase letters, digits and dashes, up to 32: %q", name), 2)
		}
		workspace, _ := cmd.Flags().GetString("workspace")
		title, _ := cmd.Flags().GetString("title")
		soul, _ := cmd.Flags().GetString("soul")
		soulFile, _ := cmd.Flags().GetString("soul-file")
		model, _ := cmd.Flags().GetString("model")
		profile, _ := cmd.Flags().GetString("profile")
		isolate, _ := cmd.Flags().GetBool("isolate")
		color, _ := cmd.Flags().GetString("color")
		if workspace == "" {
			if _, id := workspaceRootFor(mustCwd()); id != "" {
				workspace = id
			}
		}
		if workspace == "" {
			exitWithError("agent_bot", fmt.Errorf("--workspace is required outside a registered workspace"), 2)
		}
		if _, err := workspaceRoot(workspace); err != nil {
			exitWithError("agent_bot", err, 2)
		}
		if soulFile != "" {
			raw, err := os.ReadFile(soulFile)
			if err != nil {
				exitWithError("agent_bot", err, 1)
			}
			soul = string(raw)
		}
		if len(soul) > 8000 {
			exitWithError("agent_bot", fmt.Errorf("the soul is %d characters; keep it under 8000", len(soul)), 2)
		}
		if model != "" && !validModel(model) {
			exitWithError("agent_bot", fmt.Errorf("model %q: letters, digits, dots and dashes only", model), 2)
		}
		if profile != "" && !containsString(launchProfileNames(), profile) {
			exitWithError("agent_bot", fmt.Errorf("no profile %q — corgi agent profile list", profile), 2)
		}
		if color != "" && !containsString(bots.Colors, color) {
			exitWithError("agent_bot", fmt.Errorf("color is one of %s", strings.Join(bots.Colors, ", ")), 2)
		}
		dir := mustAgentDir()
		store, err := bots.Load(bots.Path(dir))
		if err != nil {
			exitWithError("agent_bot", err, 1)
		}
		if color == "" {
			color = bots.Colors[len(store.Bots)%len(bots.Colors)]
		}
		store.Put(bots.Bot{Name: name, Title: strings.TrimSpace(title), Workspace: workspace, Soul: strings.TrimSpace(soul), Model: model, Profile: profile, Isolate: isolate, Color: color})
		if err := bots.Save(bots.Path(dir), store); err != nil {
			exitWithError("agent_bot", err, 1)
		}
		if utils.JSONOutput {
			b, _ := store.Find(name)
			utils.PrintJSON(b)
			return
		}
		utils.Infof("✓ bot %s in %s — corgi agent bot open %s\n", name, workspace, name)
	},
}

var agentBotListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every bot on this machine",
	Run: func(cmd *cobra.Command, _ []string) {
		store, err := bots.Load(bots.Path(mustAgentDir()))
		if err != nil {
			exitWithError("agent_bot", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(store.Bots)
			return
		}
		if len(store.Bots) == 0 {
			fmt.Println("No bots. corgi agent bot add reviewer --workspace api --soul \"You review pull requests.\"")
			return
		}
		for _, b := range store.Bots {
			thread := "no conversation yet"
			if b.LastSession != "" {
				thread = "last talked " + roughAge(time.Since(b.LastSeen)) + " ago"
			}
			extra := []string{b.Workspace}
			if b.Model != "" {
				extra = append(extra, b.Model)
			}
			if b.Profile != "" {
				extra = append(extra, b.Profile)
			}
			if b.Isolate {
				extra = append(extra, "own worktree")
			}
			fmt.Printf("%-16s %-20s %s · %s\n", b.Name, b.Display(), strings.Join(extra, " · "), thread)
		}
	},
}

var agentBotRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a bot (its conversations stay in Claude Code)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		store, err := bots.Load(bots.Path(dir))
		if err != nil {
			exitWithError("agent_bot", err, 1)
		}
		if !store.Remove(args[0]) {
			exitWithError("agent_bot", fmt.Errorf("no bot named %q", args[0]), 2)
		}
		if err := bots.Save(bots.Path(dir), store); err != nil {
			exitWithError("agent_bot", err, 1)
		}
		utils.Infof("✓ removed %s\n", args[0])
	},
}

var agentBotOpenCmd = &cobra.Command{
	Use:   "open <name>",
	Short: "Open a bot in the editor window in front (the '+' key, as that bot)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		window, _ := cmd.Flags().GetString("window")
		if _, err := loadBot(args[0]); err != nil {
			exitWithError("agent_bot", err, 2)
		}
		sendBoardCommand(command.Command{Action: command.ActionNew, WindowID: window, Command: daemon.NewSessionCommand("--bot", args[0]), Source: "cli"},
			fmt.Sprintf("asked the editor to open %s", args[0]))
	},
}

func mustCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// launchBotsHandler lists the bots for the phone, the bar and the editor.
func launchBotsHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
	if r.Method != http.MethodGet {
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET the bots")
		return
	}
	dir, err := agentDir()
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, err.Error())
		return
	}
	store, err := bots.Load(bots.Path(dir))
	if err != nil {
		writeLaunchError(w, http.StatusInternalServerError, "could not read the bots")
		return
	}
	// The soul stays on the machine: a phone needs to know a bot exists,
	// not what it was told.
	list := make([]map[string]any, 0, len(store.Bots))
	for _, b := range store.Bots {
		list = append(list, map[string]any{
			"name": b.Name, "title": b.Display(), "workspace": b.Workspace, "model": b.Model, "profile": b.Profile,
			"isolate": b.Isolate, "color": b.Color, "lastSession": b.LastSession, "lastSeen": b.LastSeen, "hasSoul": strings.TrimSpace(b.Soul) != "",
		})
	}
	writeLaunchJSON(w, map[string]any{"bots": list})
}

func init() {
	agentBotAddCmd.Flags().String("workspace", "", "The registered workspace it works in (default: the one you are in)")
	agentBotAddCmd.Flags().String("title", "", "What the surfaces call it (default: the name)")
	agentBotAddCmd.Flags().String("soul", "", "Who it is: appended to Claude Code's system prompt")
	agentBotAddCmd.Flags().String("soul-file", "", "Read the soul from a file")
	agentBotAddCmd.Flags().String("model", "", "opus, sonnet, haiku, or a model id")
	agentBotAddCmd.Flags().String("profile", "", "The corgi profile (account) it runs under")
	agentBotAddCmd.Flags().Bool("isolate", false, "Always in a worktree of its own")
	agentBotAddCmd.Flags().String("color", "", "Avatar colour: "+strings.Join(bots.Colors, ", "))
	agentBotOpenCmd.Flags().String("window", "", "The editor window to open it in (default: the one in front)")
	agentBotCmd.AddCommand(agentBotAddCmd, agentBotListCmd, agentBotRmCmd, agentBotOpenCmd)
	agentCmd.AddCommand(agentBotCmd)
}
