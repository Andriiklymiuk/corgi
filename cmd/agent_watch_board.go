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

func watchWriter(agentD, workspaceID string) (watch.Writer, string, error) {
	resolved, err := resolveWorkspaceConfig(agentD, workspaceID)
	if err != nil {
		return nil, "", err
	}
	wc := resolved.Watch
	if wc == nil {
		return nil, "", fmt.Errorf("%s does not watch a tracker - `corgi agent watch enable` there first", workspaceID)
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
		return nil, wc.Project, fmt.Errorf("no %s token for %s - `corgi agent watch auth %s --local` there", tracker, workspaceID, tracker)
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
		fmt.Println("  no columns cached - run with --refresh")
		return
	}
	fmt.Print("  columns    ")
	names := make([]string, 0, len(info.Statuses))
	for _, s := range info.Statuses {
		names = append(names, s.Name)
	}
	fmt.Println(strings.Join(names, " · "))
	if info.Stale(time.Now()) {
		fmt.Println("  (older than a week - --refresh to be sure)")
	}
}

var agentWatchMoveCmd = &cobra.Command{
	Use:   "move <REF> <status>",
	Short: "Move a ticket to another column",
	Long: `Moves one ticket on the tracker, as you.

  corgi agent watch move ABC-123 "In Progress"
  corgi agent watch move ABC-123 "Ready for staging"
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

  corgi agent watch assign ABC-123`,
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
	Short: "Post a comment on a ticket, pictures, videos and documents included",
	Long: `Posts one comment on a ticket, as you.

  corgi agent watch comment ABC-123 "Fixed on staging, try build 412"
  corgi agent watch comment ABC-123 "Here is what I see on 412" --file grid.jpg --file hero.jpg
  corgi agent watch comment ABC-123 "Repro on 412" --file repro.mp4 --file console.log
  corgi agent watch comment ABC-123 "Numbers from staging" --file report.pdf --file orders.csv
  corgi agent watch comment ABC-123 "Back to 3.1.8 in [!9168](https://gitlab.com/g/p/-/merge_requests/9168)"

--file uploads each file to the ticket and puts it inside the comment: a
picture shows inline, a video (mp4, mov, webm, m4v) plays inline on Jira and
is a link on Linear, anything else (pdf, csv, log, json, zip, docx, xlsx...)
is a file card on Jira and a link on Linear. --image is the same flag.
On Jira this is the only way a file shows: a comment that just names a
file, or markdown like ![](file.jpg), leaves the reader with a file name.
Every file is checked before anything is sent: a missing, unreadable, empty
or too large one posts nothing. Jira's limit is the site's own (1 GB by
default); nothing over 2 GB goes to either tracker.
The text is plain text. A URL or a markdown link [label](url) is clickable;
a bare !9168 or #42 is not, the tracker does not know which repo it means.`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		paths, _ := cmd.Flags().GetStringArray("file")
		images, _ := cmd.Flags().GetStringArray("image")
		files, err := watch.ReadAttachments(append(paths, images...))
		if err != nil {
			exitWithError("agent_watch_comment", err, 2)
		}
		runTrackerWriteWithin(cmd, "agent_watch_comment", args[0], watch.UploadTimeout(files), func(ctx context.Context, w watch.Writer, ref string) (string, error) {
			if len(files) == 0 {
				if err := w.Comment(ctx, ref, args[1]); err != nil {
					return "", err
				}
				markInboxDone(mustAgentDir(), ref, "you")
				return "commented on " + ref, nil
			}
			attacher, ok := w.(watch.FileCommenter)
			if !ok {
				return "", fmt.Errorf("%s comments cannot carry files yet", w.Name())
			}
			if err := attacher.CommentWithFiles(ctx, ref, args[1], files); err != nil {
				return "", err
			}
			markInboxDone(mustAgentDir(), ref, "you")
			return fmt.Sprintf("commented on %s with %s", ref, watch.DescribeAttachments(files)), nil
		})
	},
}

func runTrackerWrite(cmd *cobra.Command, label, ref string, do func(context.Context, watch.Writer, string) (string, error)) {
	runTrackerWriteWithin(cmd, label, ref, 2*time.Minute, do)
}

func runTrackerWriteWithin(cmd *cobra.Command, label, ref string, timeout time.Duration, do func(context.Context, watch.Writer, string) (string, error)) {
	dir := mustAgentDir()
	id, err := watchTargetWorkspace(dir, cmd.Flags())
	if err != nil {
		exitWithError(label, err, 2)
	}
	w, _, err := watchWriter(dir, id)
	if err != nil {
		exitWithError(label, err, 2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	agentWatchCommentCmd.Flags().StringArray("file", nil, "File to upload and show inside the comment: picture, video or document (repeatable)")
	agentWatchCommentCmd.Flags().StringArray("image", nil, "Same as --file, kept for older callers (repeatable)")
	agentWatchCmd.AddCommand(agentWatchBoardCmd, agentWatchMoveCmd, agentWatchAssignCmd, agentWatchCommentCmd)
}

var autoForNames = map[string][]string{
	"tickets":  {"issue.new"},
	"issues":   {"issue.new"},
	"comments": {"issue.comment", "pr.comment"},
	"reviews":  {"pr.review"},
	"requests": {"review.requested"},
	"ci":       {"ci.failed"},
	"builds":   {"ci.failed"},
	"prs":      {"pr.review", "pr.comment"},
	"all":      nil,
}

var knownWatchKinds = map[string]bool{
	"issue.new": true, "issue.comment": true, "pr.comment": true, "pr.review": true, "review.requested": true, "ci.failed": true,
}

func parseAutoFor(raw string) ([]string, error) {
	var kinds []string
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		word := strings.ToLower(strings.TrimSpace(part))
		if word == "" {
			continue
		}
		group, named := autoForNames[word]
		if !named {
			if !knownWatchKinds[word] {
				return nil, fmt.Errorf("--auto-for %q: say tickets, comments, reviews, requests, prs, ci or all, or a kind like pr.review", part)
			}
			group = []string{word}
		}
		if word == "all" {
			return nil, nil
		}
		for _, k := range group {
			if !seen[k] {
				seen[k] = true
				kinds = append(kinds, k)
			}
		}
	}
	return kinds, nil
}
