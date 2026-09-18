package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/transcript"
)

var agentTranscriptCmd = &cobra.Command{
	Use:   "transcript <session> [--after N] [--max N]",
	Short: "A session's conversation: the newest entries, or everything after an offset",
	Long: `Reads a session's transcript as the phone's chat does — who said what, the
tools it ran, what they returned — for a panel beside the code or a script.

  corgi agent transcript 2 --json            the newest 40 entries and the offset after them
  corgi agent transcript api --after 84213   what landed after that offset (0 entries: nothing yet)

--json prints {entries, offset, empty}; an id, an id prefix, a label or a key
number names the session as everywhere else.

--why reads the whole conversation as steps: what the agent said, the tools it
ran on the strength of it, the files it touched — plan step, tool call and diff
side by side. --json prints {steps}; --max caps the steps, newest last.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		after, _ := cmd.Flags().GetInt64("after")
		limit, _ := cmd.Flags().GetInt("max")
		session, code, msg := launchSessionFor(args[0])
		if code != 0 {
			exitWithError("agent_transcript", fmt.Errorf("%s", msg), 2)
		}
		path := transcriptPathFor(session)
		if why, _ := cmd.Flags().GetBool("why"); why {
			printTranscriptWhy(session.ID, path, limit)
			return
		}
		var entries []transcript.Entry
		var offset int64
		var err error
		switch {
		case path == "" || !transcript.Exists(path):
			entries = []transcript.Entry{}
		case after <= 0:
			entries, offset, err = transcript.Last(path, limit)
		default:
			entries, offset, err = transcript.Read(path, after, limit)
		}
		if err != nil {
			exitWithError("agent_transcript", err, 1)
		}
		if entries == nil {
			entries = []transcript.Entry{}
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"session": session.ID, "entries": entries, "offset": offset, "empty": len(entries) == 0 && after <= 0})
			return
		}
		if len(entries) == 0 {
			fmt.Println("nothing yet")
			return
		}
		for _, e := range entries {
			line := e.Text
			if e.Kind == "tool" {
				line = strings.TrimSpace(e.Tool + " " + e.Subject)
			}
			if e.Picture != "" {
				line = strings.TrimSpace("🖼 picture " + line)
			}
			fmt.Printf("%-9s %s\n", e.Kind, firstLineOf(line))
		}
	},
}

func transcriptSteps(path string, limit int) ([]transcript.Step, error) {
	var all []transcript.Entry
	if path != "" && transcript.Exists(path) {
		var offset int64
		for {
			entries, next, err := transcript.Read(path, offset, transcript.MaxEntries)
			if err != nil {
				return nil, err
			}
			all = append(all, entries...)
			if len(entries) == 0 || next == offset {
				break
			}
			offset = next
		}
	}
	steps := transcript.Steps(all)
	if limit > 0 && len(steps) > limit {
		steps = steps[len(steps)-limit:]
	}
	if steps == nil {
		steps = []transcript.Step{}
	}
	return steps, nil
}

func printTranscriptWhy(sessionID, path string, limit int) {
	steps, err := transcriptSteps(path, limit)
	if err != nil {
		exitWithError("agent_transcript", err, 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"session": sessionID, "steps": steps})
		return
	}
	if len(steps) == 0 {
		fmt.Println("nothing yet")
		return
	}
	for _, st := range steps {
		who := "agent"
		if st.Kind == "user" {
			who = "you"
		}
		fmt.Printf("%-5s %s\n", who, firstLineOf(st.Text))
		for _, tool := range st.Tools {
			fmt.Printf("      · %s\n", tool)
		}
		if len(st.Files) > 0 {
			fmt.Printf("      files: %s\n", strings.Join(st.Files, ", "))
		}
	}
}

func init() {
	agentTranscriptCmd.Flags().Bool("why", false, "The conversation as steps: what was said, the tools run on it, the files touched")
	agentTranscriptCmd.Flags().Int64("after", 0, "Read what landed after this offset (from a previous --json answer); 0 reads the newest")
	agentTranscriptCmd.Flags().Int("max", 40, "At most this many entries")
	agentCmd.AddCommand(agentTranscriptCmd)
}
