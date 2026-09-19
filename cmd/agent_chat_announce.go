package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

func announceText(title string, urls []string) string {
	return watch.PullLines(title, urls)
}

func announceChannel(to string, def *slackDefaults) string {
	if to = strings.TrimSpace(to); to != "" {
		return to
	}
	if def == nil {
		return ""
	}
	if len(def.ReviewChannels) > 0 && strings.TrimSpace(def.ReviewChannels[0]) != "" {
		return strings.TrimSpace(def.ReviewChannels[0])
	}
	return strings.TrimSpace(def.PostTo)
}

var agentChatAnnounceCmd = &cobra.Command{
	Use:   "announce <title> <pr url>...",
	Short: "Post the pull requests of a piece of work in the workspace's review channel",
	Long: `The post a person writes by hand when work is up for review: the title, then
one line per pull request named by its repository. It goes to the first
--review-channel of the workspace (or postTo), in the voice replyAs names.
No channel configured: nothing is posted, and it says so — a workspace
without a review channel is not an error.

  corgi agent chat announce "[ABC-12] Phone field" https://github.com/acme/api/pull/5 https://github.com/acme/web/pull/9
  corgi agent chat announce "[ABC-12] Phone field" <url> --to '#reviews'`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := cmd.Flags()
		to, _ := f.GetString("to")
		as, _ := f.GetString("as")
		workspace, _ := f.GetString("workspace")
		dir := mustAgentDir()
		if workspace == "" {
			workspace, _ = currentWorkspaceID(dir)
		}
		channel := announceChannel(to, slackDefaultsFor(dir, workspace))
		if channel == "" {
			utils.Infof("no review channel for %s — nothing posted (corgi agent watch enable --review-channel '#reviews')\n", firstNonEmpty(workspace, "this workspace"))
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"posted": false})
			}
			return nil
		}
		if len(args) == 1 {
			return fmt.Errorf("nothing to announce: pass the pull request URLs after the title")
		}
		return runChatPost(chatRequest{Text: announceText(args[0], args[1:]), To: channel, As: as, Workspace: workspace})
	},
}

func init() {
	f := agentChatAnnounceCmd.Flags()
	f.String("to", "", "Channel instead of the workspace's review channel")
	f.String("as", "", "bot or me; default is the workspace's replyAs")
	f.String("workspace", "", "Workspace id, when not run inside it")
	agentChatCmd.AddCommand(agentChatAnnounceCmd)
}
