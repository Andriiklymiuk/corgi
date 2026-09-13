package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/bots"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/watch"

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
  corgi agent claude --bot reviewer      # in this terminal

A bot can also act on its own: --on names the events it runs on when they
arrive in its workspace, as an unattended run under its soul.

  corgi agent bot add reviewer --template reviewer --workspace api
  corgi agent bot add fixer --on ci.failed --soul "…" --workspace api
  corgi agent bot show reviewer          # what it is, what it did`,
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
		onList, _ := cmd.Flags().GetStringSlice("on")
		template, _ := cmd.Flags().GetString("template")
		// A template fills what was not said: the reviewer's soul, its
		// triggers, its colour — and a soul or --on on the line still wins.
		if template != "" {
			t, ok := bots.Template(template)
			if !ok {
				exitWithError("agent_bot", fmt.Errorf("no template %q — one of %s", template, strings.Join(bots.TemplateNames(), ", ")), 2)
			}
			if soul == "" && soulFile == "" {
				soul = t.Soul
			}
			if title == "" {
				title = t.Title
			}
			if model == "" {
				model = t.Model
			}
			if color == "" {
				color = t.Color
			}
			if len(onList) == 0 {
				onList = t.On
			}
			if !isolate {
				isolate = t.Isolate
			}
		}
		on, err := bots.ParseTriggers(onList)
		if err != nil {
			exitWithError("agent_bot", err, 2)
		}
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
		store.Put(bots.Bot{Name: name, Title: strings.TrimSpace(title), Workspace: workspace, Soul: strings.TrimSpace(soul), Model: model, Profile: profile, Isolate: isolate, Color: color, On: on})
		if err := bots.Save(bots.Path(dir), store); err != nil {
			exitWithError("agent_bot", err, 1)
		}
		if utils.JSONOutput {
			b, _ := store.Find(name)
			utils.PrintJSON(b)
			return
		}
		if len(on) > 0 {
			utils.Infof("✓ bot %s in %s — runs on its own when %s; corgi agent bot open %s\n", name, workspace, bots.TriggerWords(on), name)
			return
		}
		utils.Infof("✓ bot %s in %s — corgi agent bot open %s\n", name, workspace, name)
	},
}

var agentBotShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "What a bot is, what it does on its own, and what it has done",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		store, err := bots.Load(bots.Path(dir))
		if err != nil {
			exitWithError("agent_bot", err, 1)
		}
		b, ok := store.Find(args[0])
		if !ok {
			exitWithError("agent_bot", fmt.Errorf("no bot named %q", args[0]), 2)
		}
		detail := botDetail(dir, b)
		if utils.JSONOutput {
			utils.PrintJSON(detail)
			return
		}
		fmt.Printf("%s (%s) in %s", b.Display(), b.Name, b.Workspace)
		if b.Model != "" {
			fmt.Printf(" · %s", b.Model)
		}
		if b.Profile != "" {
			fmt.Printf(" · account %s", b.Profile)
		}
		if b.Isolate {
			fmt.Print(" · own worktree")
		}
		fmt.Println()
		if len(b.On) > 0 {
			fmt.Printf("  runs on its own when %s\n", bots.TriggerWords(b.On))
		} else {
			fmt.Println("  only when opened: corgi agent bot open " + b.Name + " · --on pr.review,ci.failed makes it act on its own")
		}
		if strings.TrimSpace(b.Soul) != "" {
			fmt.Println("  soul: " + strings.ReplaceAll(strings.TrimSpace(b.Soul), "\n", "\n        "))
		}
		if b.LastSession != "" {
			fmt.Printf("  last conversation %s (%s)\n", agoWord(time.Since(b.LastSeen)), b.LastSession[:min(8, len(b.LastSession))])
		}
		if len(detail.Runs) == 0 {
			fmt.Println("  no runs yet")
		}
		for _, r := range detail.Runs {
			when := agoWord(time.Since(r.StartedAt))
			if !r.Done() {
				fmt.Printf("  running on %s since %s\n", r.Ref, when)
				continue
			}
			fmt.Printf("  %s · %s · %s\n", r.Ref, r.Outcome(), when)
		}
	},
}

// BotDetail is a bot with what it has done: its runs, newest first.
type BotDetail struct {
	bots.Bot
	Runs []watch.FixRecord `json:"runs"`
}

// botDetail reads the bot's runs out of the fix log.
func botDetail(dir string, b bots.Bot) BotDetail {
	d := BotDetail{Bot: b, Runs: []watch.FixRecord{}}
	for _, r := range watch.LoadFixLog(dir).RecentFixes("", 200) {
		if r.Bot == b.Name {
			d.Runs = append(d.Runs, r)
		}
		if len(d.Runs) == 12 {
			break
		}
	}
	return d
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
				thread = "last talked " + agoWord(time.Since(b.LastSeen))
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

// agoWord is roughAge as a sentence ends: "3m ago", or "just now".
func agoWord(d time.Duration) string {
	w := roughAge(d)
	if w == "just now" {
		return w
	}
	return w + " ago"
}

func mustCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// launchBotsHandler is the bots for the phone, the bar and the editor:
// GET lists them (?name= is one, with its runs); POST adds or edits one
// (a paired phone may write a soul — the body travels sealed); DELETE
// ?name= removes one. The templates ride along, so a surface can offer
// "add a reviewer" as one tap.
func launchBotsHandler(w http.ResponseWriter, r *http.Request) {
	setLaunchHeaders(w)
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
	row := func(b bots.Bot) map[string]any {
		on := b.On
		if on == nil {
			on = []string{}
		}
		return map[string]any{
			"name": b.Name, "title": b.Display(), "workspace": b.Workspace, "model": b.Model, "profile": b.Profile,
			"isolate": b.Isolate, "color": b.Color, "lastSession": b.LastSession, "lastSeen": b.LastSeen,
			"hasSoul": strings.TrimSpace(b.Soul) != "", "soul": b.Soul, "on": on, "onWords": bots.TriggerWords(b.On),
		}
	}
	switch r.Method {
	case http.MethodGet:
		if name := r.URL.Query().Get("name"); name != "" {
			b, ok := store.Find(name)
			if !ok {
				writeLaunchError(w, http.StatusNotFound, "no bot named "+name)
				return
			}
			detail := botDetail(dir, b)
			out := row(b)
			runs := make([]map[string]any, 0, len(detail.Runs))
			for _, x := range detail.Runs {
				run := map[string]any{"key": x.Key, "ref": x.Ref, "kind": x.Kind, "title": firstLineOf(x.Title), "url": x.URL, "startedAt": x.StartedAt, "running": !x.Done(), "outcome": x.Outcome()}
				if len(x.PRs) > 0 {
					run["prs"] = x.PRs
				}
				if x.Error != "" {
					run["error"] = firstLineOf(x.Error)
				}
				if x.CostUSD > 0 || x.Tokens > 0 {
					run["costUSD"], run["tokens"] = x.CostUSD, x.Tokens
				}
				runs = append(runs, run)
			}
			out["runs"] = runs
			writeLaunchJSON(w, out)
			return
		}
		list := make([]map[string]any, 0, len(store.Bots))
		for _, b := range store.Bots {
			list = append(list, row(b))
		}
		templates := make([]map[string]any, 0, len(bots.Templates))
		for _, t := range bots.Templates {
			templates = append(templates, map[string]any{"name": t.Name, "title": t.Title, "soul": t.Soul, "model": t.Model, "color": t.Color, "on": t.On, "isolate": t.Isolate, "onWords": bots.TriggerWords(t.On)})
		}
		triggers := make([]map[string]string, 0, len(bots.Triggers))
		for _, k := range bots.TriggerKinds() {
			triggers = append(triggers, map[string]string{"kind": k, "means": bots.Triggers[k]})
		}
		writeLaunchJSON(w, map[string]any{"bots": list, "templates": templates, "triggers": triggers, "colors": bots.Colors})
	case http.MethodPost:
		var req struct {
			Name      string   `json:"name"`
			Title     string   `json:"title"`
			Workspace string   `json:"workspace"`
			Soul      string   `json:"soul"`
			Model     string   `json:"model"`
			Profile   string   `json:"profile"`
			Isolate   bool     `json:"isolate"`
			Color     string   `json:"color"`
			On        []string `json:"on"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&req); err != nil {
			writeLaunchError(w, http.StatusBadRequest, "could not read the request")
			return
		}
		name := strings.ToLower(strings.TrimSpace(req.Name))
		if !bots.ValidName(name) {
			writeLaunchError(w, http.StatusBadRequest, "a bot name is lowercase letters, digits and dashes, up to 32")
			return
		}
		if _, err := workspaceRoot(req.Workspace); err != nil {
			writeLaunchError(w, http.StatusBadRequest, err.Error())
			return
		}
		if len(req.Soul) > 8000 {
			writeLaunchError(w, http.StatusBadRequest, "the soul is over 8000 characters")
			return
		}
		if req.Model != "" && !validModel(req.Model) {
			writeLaunchError(w, http.StatusBadRequest, "model: letters, digits, dots and dashes only")
			return
		}
		if req.Profile != "" && !containsString(launchProfileNames(), req.Profile) {
			writeLaunchError(w, http.StatusBadRequest, "no profile "+req.Profile)
			return
		}
		if req.Color != "" && !containsString(bots.Colors, req.Color) {
			writeLaunchError(w, http.StatusBadRequest, "color is one of "+strings.Join(bots.Colors, ", "))
			return
		}
		on, err := bots.ParseTriggers(req.On)
		if err != nil {
			writeLaunchError(w, http.StatusBadRequest, err.Error())
			return
		}
		color := req.Color
		if color == "" {
			if prev, ok := store.Find(name); ok && prev.Color != "" {
				color = prev.Color
			} else {
				color = bots.Colors[len(store.Bots)%len(bots.Colors)]
			}
		}
		store.Put(bots.Bot{Name: name, Title: strings.TrimSpace(req.Title), Workspace: req.Workspace, Soul: strings.TrimSpace(req.Soul), Model: req.Model, Profile: req.Profile, Isolate: req.Isolate, Color: color, On: on})
		if err := bots.Save(bots.Path(dir), store); err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not save the bot")
			return
		}
		b, _ := store.Find(name)
		writeLaunchJSON(w, map[string]any{"done": "saved", "bot": row(b)})
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if !store.Remove(name) {
			writeLaunchError(w, http.StatusNotFound, "no bot named "+name)
			return
		}
		if err := bots.Save(bots.Path(dir), store); err != nil {
			writeLaunchError(w, http.StatusInternalServerError, "could not save the bots")
			return
		}
		writeLaunchJSON(w, map[string]any{"done": "removed"})
	default:
		writeLaunchError(w, http.StatusMethodNotAllowed, "GET, POST or DELETE a bot")
	}
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
	agentBotAddCmd.Flags().StringSlice("on", nil, "Run on its own when these arrive in its workspace: "+strings.Join(bots.TriggerKinds(), ", "))
	agentBotAddCmd.Flags().String("template", "", "Start from a template: "+strings.Join(bots.TemplateNames(), ", "))
	agentBotOpenCmd.Flags().String("window", "", "The editor window to open it in (default: the one in front)")
	agentBotCmd.AddCommand(agentBotAddCmd, agentBotListCmd, agentBotShowCmd, agentBotRmCmd, agentBotOpenCmd)
	agentCmd.AddCommand(agentBotCmd)
}
