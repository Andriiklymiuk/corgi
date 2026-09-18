package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"
)

// Speaking back into chat. corgi listens to Slack through the watch; this
// is the other half — a reply from a session, from the phone, or from a
// person at the shell, in the thread the message came from.

type chatRequest struct {
	Text      string
	To        string
	Reply     string
	As        string
	React     string
	Workspace string
}

// slackDefaults is the workspace's own answer for target and voice.
type slackDefaults struct {
	PostTo  string
	ReplyAs string
}

// chatTarget works out where a message goes: the event it answers, else the
// flag, else the workspace's default.
func chatTarget(req chatRequest, state *watch.State, def *slackDefaults) (watch.SlackTarget, error) {
	target := watch.SlackTarget{As: strings.TrimSpace(req.As)}
	if target.As == "" && def != nil {
		target.As = def.ReplyAs
	}
	if key := strings.TrimSpace(req.Reply); key != "" {
		parts := strings.Split(key, ":")
		if len(parts) != 3 || parts[0] != "slack" {
			return target, fmt.Errorf("--reply wants a slack event key (slack:<channel>:<ts>), got %q", key)
		}
		target.Channel = parts[1]
		// A reply to a message already in a thread joins that thread; a
		// reply to a top-level post opens one on it.
		if parent := state.Thread(key); parent != "" {
			target.ThreadTS = parent
		} else {
			target.ThreadTS = parts[2]
		}
		return target, nil
	}
	if to := strings.TrimSpace(req.To); to != "" {
		target.Channel = to
		return target, nil
	}
	if def != nil && strings.TrimSpace(def.PostTo) != "" {
		target.Channel = strings.TrimSpace(def.PostTo)
		return target, nil
	}
	return target, fmt.Errorf("no target: --to '#channel', --reply <event key>, or postTo in the workspace's chat config")
}

// slackDefaultsFor reads the workspace's chat block, if it has one.
func slackDefaultsFor(dir, workspace string) *slackDefaults {
	if workspace == "" {
		return nil
	}
	specs, err := loadWatchSpecs(dir)
	if err != nil {
		return nil
	}
	for _, s := range specs {
		if s.Workspace == workspace && s.Chat != nil {
			return &slackDefaults{PostTo: s.Chat.PostTo, ReplyAs: s.Chat.ReplyAs}
		}
	}
	return nil
}

func runChatPost(req chatRequest) error {
	dir := mustAgentDir()
	if req.Workspace == "" {
		req.Workspace, _ = currentWorkspaceID(dir)
	}
	poster := watch.NewSlackPoster(watch.LoadSecretsFor(dir, req.Workspace))
	state := watch.LoadState(dir)
	poster.Team = state.SourceIdentity("slack", "team")

	target, err := chatTarget(req, state, slackDefaultsFor(dir, req.Workspace))
	if err != nil {
		return err
	}
	ctx := context.Background()
	channel, err := poster.Resolve(ctx, target.Channel)
	if err != nil {
		return err
	}
	target.Channel = channel

	out := map[string]any{}
	if text := strings.TrimSpace(req.Text); text != "" {
		posted, err := poster.Post(ctx, target, text)
		if err != nil {
			return err
		}
		out["channel"], out["ts"], out["permalink"], out["as"] = posted.Channel, posted.TS, posted.Permalink, posted.As
		utils.Infof("posted as %s: %s\n", posted.As, posted.Permalink)
	}
	if emoji := strings.TrimSpace(req.React); emoji != "" {
		if err := poster.React(ctx, target, emoji); err != nil {
			return err
		}
		out["reacted"] = emoji
		utils.Infof("reacted :%s:\n", strings.Trim(emoji, ":"))
	}
	if len(out) == 0 {
		return fmt.Errorf("nothing to say: pass a message, or --react <emoji>")
	}
	if utils.JSONOutput {
		utils.PrintJSON(out)
	}
	return nil
}

// mcpChatPost is the tool behind corgi_chat_post: the same call the shell
// makes, so a session and a person say things the same way — with one
// difference. A session may not ASK to speak as the person. Much of what a
// session reads came from other people, so an agent that can be talked into
// posting under its owner's name is a way to put words in their mouth. The
// workspace's own replyAs still decides, because that is the machine
// owner's standing choice rather than something a message can reach.
func mcpChatPost(r mcp.CallToolRequest) (any, error) {
	if as := strings.TrimSpace(r.GetString("as", "")); strings.EqualFold(as, "me") {
		return nil, fmt.Errorf("a session may not post under your name: leave `as` out and the workspace's replyAs decides, or run `corgi agent chat post --as me` yourself")
	}
	if err := runChatPost(chatRequest{
		Text:      r.GetString("text", ""),
		To:        r.GetString("to", ""),
		Reply:     r.GetString("reply", ""),
		As:        r.GetString("as", ""),
		React:     r.GetString("react", ""),
		Workspace: r.GetString("workspace", ""),
	}); err != nil {
		return nil, err
	}
	return map[string]any{"posted": true}, nil
}

var agentChatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Speak in the chat corgi listens to",
	Long: `The watch turns a Slack mention into an event; this says something back.
A reply goes into the thread the message came from, so a conversation stays
one conversation.

Voice: --as bot (the app) or --as me (your own account). Without it the
workspace's replyAs decides, and without that the bot speaks if there is a
bot token. A machine posting under your own name is not something that can
be taken back, so it is never the accidental default.

  corgi agent chat post "deploy is out" --to '#backend'
  corgi agent chat post "on it" --reply slack:C0RE:1726000400.000100
  corgi agent chat react slack:C0RE:1726000400.000100 white_check_mark`,
}

var agentChatPostCmd = &cobra.Command{
	Use:   "post <text>",
	Short: "Say something in a channel, or in the thread of an event",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		f := cmd.Flags()
		req := chatRequest{}
		if len(args) == 1 {
			req.Text = args[0]
		}
		req.To, _ = f.GetString("to")
		req.Reply, _ = f.GetString("reply")
		req.As, _ = f.GetString("as")
		req.React, _ = f.GetString("react")
		req.Workspace, _ = f.GetString("workspace")
		return runChatPost(req)
	},
}

var agentChatReactCmd = &cobra.Command{
	Use:   "react <event key> <emoji>",
	Short: "Put one emoji on the message an event names",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		as, _ := cmd.Flags().GetString("as")
		ws, _ := cmd.Flags().GetString("workspace")
		return runChatPost(chatRequest{Reply: args[0], React: args[1], As: as, Workspace: ws})
	},
}

func init() {
	p := agentChatPostCmd.Flags()
	p.String("to", "", "Channel to post in: #name, @handle for a direct message, or a channel id")
	p.String("reply", "", "Event key to answer (slack:<channel>:<ts>); the reply lands in its thread")
	p.String("as", "", "Voice: bot or me")
	p.String("react", "", "Also put this emoji on the message --reply names")
	p.String("workspace", "", "Workspace whose token and defaults to use; omitted means the one you are in")
	r := agentChatReactCmd.Flags()
	r.String("as", "", "Voice: bot or me")
	r.String("workspace", "", "Workspace whose token to use; omitted means the one you are in")
	agentChatCmd.AddCommand(agentChatPostCmd, agentChatReactCmd)
	agentCmd.AddCommand(agentChatCmd)
}
