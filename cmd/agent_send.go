package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils/agent/command"
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

func init() {
	agentSendCmd.Flags().Bool("enter", false, "Press Enter after the text")
	agentNoteCmd.Flags().Bool("clear", false, "Remove the note")
	agentCmd.AddCommand(agentSendCmd, agentAnswerCmd, agentNoteCmd)
}
