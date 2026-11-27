package cmd

import (
	"fmt"
	"strings"
	"time"

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

type announceOptions struct {
	To        string
	As        string
	Workspace string
	Edit      string
}

func rememberAnnounce(dir, workspace, channel, ts string, urls []string) {
	refs := watch.PullRefsIn(urls)
	if workspace == "" || channel == "" || ts == "" || len(refs) == 0 {
		return
	}
	now := time.Now()
	post := watch.ChannelPost{Workspace: workspace, Channel: channel, TS: ts, Refs: refs, At: now}
	if err := watch.RememberChannelPost(dir, post, now); err != nil {
		utils.Infof("the announce is posted, but its thread is not kept: %v\n", err)
	}
}

func runAnnounce(args []string, o announceOptions, post func(chatRequest) (watch.SlackPosted, error), edit func(chatEditRequest) error) error {
	dir := mustAgentDir()
	if o.Workspace == "" {
		o.Workspace, _ = currentWorkspaceID(dir)
	}
	if strings.TrimSpace(o.Edit) != "" {
		if strings.TrimSpace(o.To) != "" {
			return fmt.Errorf("--edit names the message, and so its channel: drop --to")
		}
		if len(args) == 1 {
			return fmt.Errorf("nothing to announce: pass the pull request URLs after the title")
		}
		if err := edit(chatEditRequest{Message: o.Edit, Text: announceText(args[0], args[1:]), As: o.As, Workspace: o.Workspace}); err != nil {
			return err
		}
		if msg, err := watch.ParseSlackMessage(o.Edit); err == nil {
			rememberAnnounce(dir, o.Workspace, msg.Channel, msg.TS, args[1:])
		}
		return nil
	}
	channel := announceChannel(o.To, slackDefaultsFor(dir, o.Workspace))
	if channel == "" {
		utils.Infof("no review channel for %s - nothing posted (corgi agent watch enable --review-channel '#reviews')\n", firstNonEmpty(o.Workspace, "this workspace"))
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"posted": false})
		}
		return nil
	}
	if len(args) == 1 {
		return fmt.Errorf("nothing to announce: pass the pull request URLs after the title")
	}
	posted, err := post(chatRequest{Text: announceText(args[0], args[1:]), To: channel, As: o.As, Workspace: o.Workspace})
	if err != nil {
		return err
	}
	rememberAnnounce(dir, o.Workspace, posted.Channel, posted.TS, args[1:])
	return nil
}

var agentChatAnnounceCmd = &cobra.Command{
	Use:   "announce <title> <pr url>...",
	Short: "Post the pull requests of a piece of work in the workspace's review channel",
	Long: `The post a person writes by hand when work is up for review: the title, then
the link alone for one pull request, or a list for several: one dot per pull
request, named by its repository. Pass the bare URLs; a "repo: " typed in front
of one is dropped. It goes to the first
--review-channel of the workspace (or postTo), in the voice replyAs names.
No channel configured: nothing is posted, and it says so - a workspace
without a review channel is not an error.

--edit <message> rewrites an announce already posted with the same body instead
of posting a second one - the story that gained a pull request. Pass every pull
request, old and new. <message> is the post's link or slack:<channel>:<ts>; Slack
lets only its author edit it, so --as names the voice that posted it.

  corgi agent chat announce "[ABC-12] Phone field" https://github.com/acme/api/pull/5 https://github.com/acme/web/pull/9
  corgi agent chat announce "[ABC-12] Phone field" <url> --to '#reviews'
  corgi agent chat announce --edit https://acme.slack.com/archives/C0RE/p1726000400000100 "[ABC-12] Phone field" <url> <url> <url>`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := cmd.Flags()
		o := announceOptions{}
		o.To, _ = f.GetString("to")
		o.As, _ = f.GetString("as")
		o.Workspace, _ = f.GetString("workspace")
		o.Edit, _ = f.GetString("edit")
		return runAnnounce(args, o, sendChat, runChatEdit)
	},
}

func init() {
	f := agentChatAnnounceCmd.Flags()
	f.String("to", "", "Channel instead of the workspace's review channel")
	f.String("as", "", "bot or me; default is the workspace's replyAs")
	f.String("workspace", "", "Workspace id, when not run inside it")
	f.String("edit", "", "Update this announce (its link or slack:<channel>:<ts>) instead of posting a new one")
	agentChatCmd.AddCommand(agentChatAnnounceCmd)
}
