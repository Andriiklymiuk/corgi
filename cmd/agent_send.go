package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/command"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/sessions"
)

var agentSendCmd = &cobra.Command{
	Use:   "send <session> <text...>",
	Short: "Type text into a session's terminal, after bringing it forward",
	Long: `Focuses a session and types the text into it. --enter adds Enter, so the
prompt is sent. A session in an integrated terminal takes it through the
corgi VS Code extension; one in iTerm2 or Terminal.app through AppleScript.
The Claude Code panel takes nothing from here (its input is a web view):
the board records the failure and a key falls back to its own keystrokes.

  corgi agent send corgi --enter "run the tests and fix what fails"
  corgi agent send 2 /compact --enter`,
	Args: cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		enter, _ := cmd.Flags().GetBool("enter")
		text := strings.Join(args[1:], " ")
		sendBoardCommand(command.Command{Action: command.ActionSend, SessionID: args[0], Text: text, Enter: enter, Source: "cli"},
			fmt.Sprintf("asked the daemon to type into %s", args[0]))
	},
}

var agentAnswerCmd = &cobra.Command{
	Use:   "answer <session> allow|always|deny",
	Short: "Answer a session's permission prompt from anywhere",
	Long: `Answers the permission prompt a session is waiting on: allow (this once),
always (for the rest of the session), or deny. Only a session whose board
entry carries a pending permission can be answered, and a Bash command the
board recognises as risky (rm, sudo, --force, …) is refused unseen: go look.

The keys are typed the way ` + "`corgi agent send`" + ` types, so the same hosts apply.`,
	Args: cobra.ExactArgs(2),
	Run: func(_ *cobra.Command, args []string) {
		answer := strings.ToLower(strings.TrimSpace(args[1]))
		switch answer {
		case "allow", "always", "deny", "yes", "no":
			if answer == "yes" {
				answer = "allow"
			}
			if answer == "no" {
				answer = "deny"
			}
		default:
			exitWithError("agent_answer", fmt.Errorf("answer is allow, always or deny, not %q", args[1]), 2)
		}
		sendBoardCommand(command.Command{Action: command.ActionAnswer, SessionID: args[0], Answer: answer, Source: "cli"},
			fmt.Sprintf("asked the daemon to %s %s", answer, args[0]))
	},
}

var agentNoteCmd = &cobra.Command{
	Use:   "note <session> [text]",
	Short: "Put your own line under a session on the board",
	Long: `Sets the note a session shows under its label — "waiting on PR review",
"do not touch" — on every surface that draws the board. No text, or
--clear, removes it. Notes go when the session is dismissed.`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		clear, _ := cmd.Flags().GetBool("clear")
		note := strings.Join(args[1:], " ")
		if clear {
			note = ""
		}
		done := fmt.Sprintf("note set on %s", args[0])
		if note == "" {
			done = fmt.Sprintf("note cleared on %s", args[0])
		}
		sendBoardCommand(command.Command{Action: command.ActionNote, SessionID: args[0], Note: note, Source: "cli"}, done)
	},
}

var agentInterruptCmd = &cobra.Command{
	Use:   "interrupt <session>",
	Short: "Stop what a session is doing — Escape, as you would press it",
	Long: `Presses Escape in a working session, the way you would to stop a turn
that is going the wrong way: Claude Code stops and waits for the next
message; nothing is closed and nothing is lost. A session that is not
working is left alone. The phone's Interrupt button, and the bar's, do
the same through POST /launch/interrupt.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		sendBoardCommand(command.Command{Action: command.ActionInterrupt, SessionID: args[0], Source: "cli"},
			fmt.Sprintf("asked the daemon to interrupt %s", args[0]))
	},
}

var agentCapCmd = &cobra.Command{
	Use:   "cap [<session>] <tokens|off>",
	Short: "A token budget for every session, or for one",
	Long: `Every session carries what it has spent — the token counts Claude Code
writes in its transcript, summed on the daemon's sweep — and a budget.
"corgi agent cap 50M" is the budget every session runs under; "corgi agent
cap <session> 20M" gives one session its own. The daemon rings once when a
session passes it and the row says "over budget" on every board; nothing
is stopped. "off" takes a budget away. No argument prints the default.`,
	Args: cobra.MaximumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		path := agentUserConfigPath(dir)
		if len(args) == 0 {
			user, err := config.LoadUser(path)
			if err != nil {
				exitWithError("agent_cap", err, 1)
			}
			if utils.JSONOutput {
				utils.PrintJSON(map[string]any{"tokens": user.SessionCap})
				return
			}
			if user.SessionCap == 0 {
				fmt.Println("no budget: corgi agent cap 50M")
				return
			}
			fmt.Printf("every session runs under %s tokens\n", sessions.Tokens(user.SessionCap))
			return
		}
		session, raw := "", args[0]
		if len(args) == 2 {
			session, raw = args[0], args[1]
		}
		var tokens int64
		if raw != "off" && raw != "0" {
			n, err := sessions.ParseTokens(raw)
			if err != nil {
				exitWithError("agent_cap", err, 2)
			}
			tokens = n
		}
		if session != "" {
			done := fmt.Sprintf("%s runs under %s tokens", session, sessions.Tokens(tokens))
			if tokens == 0 {
				done = fmt.Sprintf("%s runs under the default budget again", session)
			}
			sendBoardCommand(command.Command{Action: command.ActionCap, SessionID: session, Tokens: tokens, Source: "cli"}, done)
			return
		}
		user, err := config.LoadUser(path)
		if err != nil {
			exitWithError("agent_cap", err, 1)
		}
		user.SessionCap = tokens
		if err := writeUserConfig(path, user); err != nil {
			exitWithError("agent_cap", err, 1)
		}
		if info, err := daemon.ReadInfo(dir); err == nil && info != nil && info.Commands {
			if _, err := command.Write(dir, command.Command{Action: command.ActionCap, Tokens: tokens, Source: "cli"}); err == nil {
				daemon.Nudge(info)
			}
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"ok": true, "tokens": tokens})
			return
		}
		if tokens == 0 {
			utils.Info("✓ no budget")
			return
		}
		utils.Infof("✓ every session runs under %s tokens\n", sessions.Tokens(tokens))
	},
}

func init() {
	agentSendCmd.Flags().Bool("enter", false, "Press Enter after the text")
	agentNoteCmd.Flags().Bool("clear", false, "Remove the note")
	agentCmd.AddCommand(agentSendCmd, agentAnswerCmd, agentNoteCmd, agentCapCmd, agentInterruptCmd)
}
