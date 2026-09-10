package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/watch"
	"andriiklymiuk/corgi/utils/agent/workspace"
)

// resolveWorkspaceConfig is a registered workspace's settings: the user
// config with the repo's own committed one overlaid.
func resolveWorkspaceConfig(agentD, workspaceID string) (config.Resolved, error) {
	registry, err := workspace.Load(agentRegistryPath(agentD))
	if err != nil {
		return config.Resolved{}, err
	}
	ws, ok := registry.Find(workspaceID)
	if !ok {
		return config.Resolved{}, fmt.Errorf("%q is not a registered workspace", workspaceID)
	}
	user, err := config.LoadUser(agentUserConfigPath(agentD))
	if err != nil {
		return config.Resolved{}, err
	}
	repo, _ := config.LoadRepo(ws.AbsPath)
	return config.Resolve(ws.ID, repo, user), nil
}

// watchWriter is the tracker a workspace can change, with the project key
// its refs belong to.
func watchWriter(agentD, workspaceID string) (watch.Writer, string, error) {
	resolved, err := resolveWorkspaceConfig(agentD, workspaceID)
	if err != nil {
		return nil, "", err
	}
	wc := resolved.Watch
	if wc == nil {
		return nil, "", fmt.Errorf("%s does not watch a tracker — `corgi agent watch enable` there first", workspaceID)
	}
	secrets := watch.LoadSecretsFor(agentD, workspaceID)
	tracker := strings.TrimSpace(wc.Tracker)
	if tracker == "" {
		tracker = "jira"
		if secrets.Linear != "" && secrets.JiraToken == "" {
			tracker = "linear"
		}
	}
	w := watch.WriterFor(secrets, tracker, wc.Project)
	if w == nil {
		return nil, wc.Project, fmt.Errorf("no %s token for %s — `corgi agent watch auth %s --local` there", tracker, workspaceID, tracker)
	}
	return w, wc.Project, nil
}

var agentWatchBoardCmd = &cobra.Command{
	Use:   "board",
	Short: "The tracker columns corgi knows, and who your token is",
	Long: `Prints the workspace's board: the statuses a ticket can be moved to and the
account the token belongs to. Read once and cached, so a menu on the phone
opens without waiting on the tracker.

  corgi agent watch board                      # what is cached
  corgi agent watch board --refresh            # read it from the tracker again
  corgi agent watch board --workspace api      # another workspace`,
	Run: runAgentWatchBoard,
}

func runAgentWatchBoard(cmd *cobra.Command, _ []string) {
	dir := mustAgentDir()
	id, err := watchTargetWorkspace(dir, cmd.Flags())
	if err != nil {
		exitWithError("agent_watch_board", err, 2)
	}
	refresh, _ := cmd.Flags().GetBool("refresh")
	info := watch.LoadBoardCache(dir).Get(id)
	if refresh || !info.Has() {
		w, project, err := watchWriter(dir, id)
		if err != nil && w == nil {
			exitWithError("agent_watch_board", err, 2)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err = watch.RefreshBoard(ctx, dir, id, w, project)
		if err != nil {
			exitWithError("agent_watch_board", err, 1)
		}
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"workspace": id, "board": info})
		return
	}
	fmt.Printf("%s  %s %s\n", id, info.Tracker, info.Project)
	if info.Me.Name != "" {
		fmt.Printf("  you        %s\n", info.Me.Name)
	}
	if info.Error != "" {
		fmt.Printf("  problem    %s\n", info.Error)
	}
	if len(info.Statuses) == 0 {
		fmt.Println("  no columns cached — run with --refresh")
		return
	}
	fmt.Print("  columns    ")
	names := make([]string, 0, len(info.Statuses))
	for _, s := range info.Statuses {
		names = append(names, s.Name)
	}
	fmt.Println(strings.Join(names, " · "))
	if info.Stale(time.Now()) {
		fmt.Println("  (older than a week — --refresh to be sure)")
	}
}

var agentWatchMoveCmd = &cobra.Command{
	Use:   "move <REF> <status>",
	Short: "Move a ticket to another column",
	Long: `Moves one ticket on the tracker, as you.

  corgi agent watch move IMP-13427 "In Progress"
  corgi agent watch move IMP-13427 "TO TEST STAGING"
  corgi agent watch move ABC-1 Done --workspace api

The column names are the ones ` + "`corgi agent watch board`" + ` prints. Jira decides
which moves are legal from where the ticket is now; a refused move says what it
could have gone to instead.`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		runTrackerWrite(cmd, "agent_watch_move", args[0], func(ctx context.Context, w watch.Writer, ref string) (string, error) {
			if err := w.Move(ctx, ref, args[1]); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s → %s", ref, args[1]), nil
		})
	},
}

var agentWatchAssignCmd = &cobra.Command{
	Use:   "assign <REF>",
	Short: "Assign a ticket to yourself",
	Long: `Assigns one ticket to whoever the workspace's tracker token belongs to.

  corgi agent watch assign IMP-13427`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		runTrackerWrite(cmd, "agent_watch_assign", args[0], func(ctx context.Context, w watch.Writer, ref string) (string, error) {
			dir := mustAgentDir()
			id, _ := watchTargetWorkspace(dir, cmd.Flags())
			me := watch.LoadBoardCache(dir).Get(id).Me
			if me.ID == "" {
				got, err := w.Whoami(ctx)
				if err != nil {
					return "", err
				}
				me = got
			}
			if err := w.Assign(ctx, ref, me.ID); err != nil {
				return "", err
			}
			name := me.Name
			if name == "" {
				name = "you"
			}
			return fmt.Sprintf("%s assigned to %s", ref, name), nil
		})
	},
}

var agentWatchCommentCmd = &cobra.Command{
	Use:   "comment <REF> <text>",
	Short: "Post a comment on a ticket",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		runTrackerWrite(cmd, "agent_watch_comment", args[0], func(ctx context.Context, w watch.Writer, ref string) (string, error) {
			if err := w.Comment(ctx, ref, args[1]); err != nil {
				return "", err
			}
			return "commented on " + ref, nil
		})
	},
}

// runTrackerWrite is the shape every write shares: find the workspace, get
// its tracker, do the one thing, say what happened.
func runTrackerWrite(cmd *cobra.Command, label, ref string, do func(context.Context, watch.Writer, string) (string, error)) {
	dir := mustAgentDir()
	id, err := watchTargetWorkspace(dir, cmd.Flags())
	if err != nil {
		exitWithError(label, err, 2)
	}
	w, _, err := watchWriter(dir, id)
	if err != nil {
		exitWithError(label, err, 2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	said, err := do(ctx, w, strings.TrimSpace(ref))
	if err != nil {
		exitWithError(label, err, 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"workspace": id, "ref": ref, "done": said})
		return
	}
	fmt.Println(said)
}

func init() {
	for _, c := range []*cobra.Command{agentWatchBoardCmd, agentWatchMoveCmd, agentWatchAssignCmd, agentWatchCommentCmd} {
		c.Flags().String("workspace", "", "Workspace id; omitted means the one you are in")
	}
	agentWatchBoardCmd.Flags().Bool("refresh", false, "Read the columns from the tracker again")
	agentWatchCmd.AddCommand(agentWatchBoardCmd, agentWatchMoveCmd, agentWatchAssignCmd, agentWatchCommentCmd)
}
