package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/atomicfile"

	"github.com/spf13/cobra"
)

var savedViewName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

func savedViewsPath(dir string) string { return filepath.Join(dir, "watch", "views.json") }

func loadSavedViews(dir string) map[string]cardFilter {
	views := map[string]cardFilter{}
	if data, err := os.ReadFile(savedViewsPath(dir)); err == nil {
		_ = json.Unmarshal(data, &views)
	}
	return views
}

func writeSavedViews(dir string, views map[string]cardFilter) error {
	if err := os.MkdirAll(filepath.Dir(savedViewsPath(dir)), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(views, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(savedViewsPath(dir), data, 0o600)
}

func filterFromFlags(cmd *cobra.Command) cardFilter {
	view, _ := cmd.Flags().GetString("view")
	owner, _ := cmd.Flags().GetString("owner")
	priority, _ := cmd.Flags().GetString("priority")
	source, _ := cmd.Flags().GetString("source")
	return cardFilter{View: view, Owner: owner, Priority: priority, Source: source}
}

func resolveFilter(dir string, f cardFilter) (cardFilter, error) {
	if saved, ok := loadSavedViews(dir)[f.View]; ok {
		if f.Owner == "" {
			f.Owner = saved.Owner
		}
		if f.Priority == "" {
			f.Priority = saved.Priority
		}
		if f.Source == "" {
			f.Source = saved.Source
		}
		f.View = saved.View
	}
	return f, f.check()
}

func addCardFilterFlags(cmd *cobra.Command) {
	cmd.Flags().String("view", "", "only the cards of a view: needs-me, unreviewed, red, stale, overdue, blocked, triage, mine, cycle, or one you saved")
	cmd.Flags().String("owner", "", "only the tasks this name holds")
	cmd.Flags().String("priority", "", "only this priority: urgent, high, medium, low")
	cmd.Flags().String("source", "", "only cards linked to jira, linear, github or gitlab")
}

var agentKanbanViewsCmd = &cobra.Command{
	Use:   "views",
	Short: "The views of the board: the built-in ones and the ones you saved",
	Long: `A view is a filter with a name.

  corgi agent kanban --view unreviewed
  corgi agent kanban views save anna-urgent --owner anna --priority urgent
  corgi agent kanban --view anna-urgent
  corgi agent kanban views rm anna-urgent`,
	Args: cobra.NoArgs,
	Run: func(_ *cobra.Command, _ []string) {
		saved := loadSavedViews(mustAgentDir())
		if utils.JSONOutput {
			builtIn := []map[string]string{}
			for _, v := range kanbanViews {
				builtIn = append(builtIn, map[string]string{"name": v.name, "what": v.what})
			}
			utils.PrintJSON(map[string]any{"views": builtIn, "saved": saved})
			return
		}
		for _, v := range kanbanViews {
			fmt.Printf("%-12s %s\n", v.name, v.what)
		}
		for _, name := range savedViewNames(saved) {
			f := saved[name]
			fmt.Printf("%-12s saved: view=%s owner=%s priority=%s source=%s\n", name, f.View, f.Owner, f.Priority, f.Source)
		}
	},
}

var agentKanbanViewSaveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Keep a filter under a name",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		name := args[0]
		if !savedViewName.MatchString(name) {
			exitWithError("agent_kanban_views", fmt.Errorf("a view's name is small letters, digits and dashes, up to 31"), 2)
		}
		for _, v := range kanbanViews {
			if v.name == name {
				exitWithError("agent_kanban_views", fmt.Errorf("%s is a built-in view: pick another name", name), 2)
			}
		}
		f := filterFromFlags(cmd)
		if err := f.check(); err != nil {
			exitWithError("agent_kanban_views", err, 2)
		}
		if f.empty() {
			exitWithError("agent_kanban_views", fmt.Errorf("give the view something to filter by: --view, --owner, --priority or --source"), 2)
		}
		views := loadSavedViews(dir)
		if len(views) >= 50 {
			exitWithError("agent_kanban_views", fmt.Errorf("50 views are saved already - remove one first"), 2)
		}
		views[name] = f
		if err := writeSavedViews(dir, views); err != nil {
			exitWithError("agent_kanban_views", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"saved": name, "filter": f})
			return
		}
		fmt.Printf("saved %s - corgi agent kanban --view %s\n", name, name)
	},
}

var agentKanbanViewRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Forget a saved view",
	Args:  cobra.ExactArgs(1),
	Run: func(_ *cobra.Command, args []string) {
		dir := mustAgentDir()
		views := loadSavedViews(dir)
		if _, ok := views[args[0]]; !ok {
			exitWithError("agent_kanban_views", fmt.Errorf("no saved view %s", args[0]), 2)
		}
		delete(views, args[0])
		if err := writeSavedViews(dir, views); err != nil {
			exitWithError("agent_kanban_views", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"removed": args[0]})
			return
		}
		fmt.Printf("removed %s\n", args[0])
	},
}

func init() {
	addCardFilterFlags(agentKanbanCmd)
	addCardFilterFlags(agentKanbanViewSaveCmd)
	agentKanbanViewsCmd.AddCommand(agentKanbanViewSaveCmd, agentKanbanViewRmCmd)
	agentKanbanCmd.AddCommand(agentKanbanViewsCmd)
}
