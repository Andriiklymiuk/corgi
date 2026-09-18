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
number names the session as everywhere else.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		after, _ := cmd.Flags().GetInt64("after")
		limit, _ := cmd.Flags().GetInt("max")
		session, code, msg := launchSessionFor(args[0])
		if code != 0 {
			exitWithError("agent_transcript", fmt.Errorf("%s", msg), 2)
		}
		path := transcriptPathFor(session)
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
			fmt.Printf("%-9s %s\n", e.Kind, firstLineOf(line))
		}
	},
}

func init() {
	agentTranscriptCmd.Flags().Int64("after", 0, "Read what landed after this offset (from a previous --json answer); 0 reads the newest")
	agentTranscriptCmd.Flags().Int("max", 40, "At most this many entries")
	agentCmd.AddCommand(agentTranscriptCmd)
}
