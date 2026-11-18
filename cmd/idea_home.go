package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/idea"

	"github.com/spf13/cobra"
)

func ideaGoesIntoWorkspace(ws string, asked bool) (string, bool) {
	root := ideaWorkspaceRoot(ws)
	if root == "" {
		return "", false
	}
	if asked {
		return root, true
	}
	info, err := os.Stat(idea.WorkspaceRoot(root))
	return root, err == nil && info.IsDir()
}

func keepIdeaInWorkspace(dir, slug, ws string, asked bool) (string, error) {
	root, ok := ideaGoesIntoWorkspace(ws, asked)
	if !ok {
		return "", nil
	}
	return idea.MoveToWorkspace(dir, slug, root)
}

var ideaAdopted struct {
	sync.Mutex
	at map[string]time.Time
}

const ideaAdoptEvery = 20 * time.Second

func adoptWorkspaceIdeas(dir string) {
	ideaAdopted.Lock()
	if ideaAdopted.at == nil {
		ideaAdopted.at = map[string]time.Time{}
	}
	fresh := time.Since(ideaAdopted.at[dir]) < ideaAdoptEvery
	if !fresh {
		ideaAdopted.at[dir] = time.Now()
	}
	ideaAdopted.Unlock()
	if fresh {
		return
	}
	registry, _, err := agentRegistry()
	if err != nil {
		return
	}
	for _, ws := range registry.Sorted() {
		idea.Adopt(dir, ws.AbsPath, ws.ID)
	}
}

func listIdeas(dir string) []idea.Meta {
	adoptWorkspaceIdeas(dir)
	return idea.List(dir)
}

func whereIdeaIsKept(m idea.Meta) string {
	if m.Home == "" {
		return "on this machine"
	}
	return firstNonEmpty(m.Workspace, "the workspace") + " · " + m.Home
}

var ideaMoveCmd = &cobra.Command{
	Use:   "move <slug> --workspace <id> | --home",
	Short: "Keep the page in a workspace's .corgi/ideas folder (committed with the code), or bring it back to this machine",
	Long: `A page kept in a workspace is a folder in that repository:
<workspace>/.corgi/ideas/<name>/ holds the page, its comments and files.
Commit it and the team has the idea and its threads next to the code;
corgi idea list on their machines finds it. The share key and what the
agent is doing right now stay on this machine and never go into the folder.

  corgi idea move brewshift --workspace api     into the api workspace; it builds there too
  corgi idea move brewshift --home              back to this machine only`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		home, _ := cmd.Flags().GetBool("home")
		if (ws == "") == !home {
			exitWithError("idea", fmt.Errorf("say one of --workspace <id> or --home"), 2)
		}
		if home {
			if err := idea.MoveHome(dir, args[0]); err != nil {
				exitWithError("idea", err, 1)
			}
		} else if _, err := setIdea(dir, args[0], ideaChanges{Workspace: ws}); err != nil {
			exitWithError("idea", err, 2)
		}
		id, err := idea.Load(dir, args[0])
		if err != nil {
			exitWithError("idea", err, 2)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"idea": id.Meta})
			return
		}
		fmt.Printf("%s is kept %s\n", id.Meta.Slug, whereIdeaIsKept(id.Meta))
	},
}

func init() {
	ideaCmd.AddCommand(ideaMoveCmd)
	ideaMoveCmd.Flags().String("workspace", "", "the workspace whose .corgi/ideas folder keeps the page")
	ideaMoveCmd.Flags().Bool("home", false, "keep it on this machine only")
}
