package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

// A friction note: what took an agent several tries, a doc that was wrong, a
// refusal that did not say what to do. It records the version and whether an
// agent wrote it, never output, code or paths, and it stays on this machine.
type feedbackNote struct {
	Time    time.Time `json:"time"`
	Version string    `json:"version"`
	Agent   bool      `json:"agent"`
	Command string    `json:"command,omitempty"`
	Note    string    `json:"note"`
}

var feedbackCommandFlag string

var feedbackCmd = &cobra.Command{
	Use:   "feedback",
	Short: "Record where corgi was hard to use, so it can be fixed (kept on this machine)",
	Long: `When a doc was wrong or missing, a refusal did not say what to do, a command
took several tries, or a workaround was needed, record it in one line:

  corgi feedback note "corgi run --detach printed no URL for web"

Notes go to ~/.corgi/feedback.jsonl with corgi's version and whether an agent
wrote them - never output, code or paths. Nothing is sent anywhere: read them
with 'corgi feedback list' and file the ones worth fixing.`,
}

var feedbackNoteCmd = &cobra.Command{
	Use:     "note <what happened>",
	Short:   "Record one friction note",
	Example: `corgi feedback note "E_READINESS_TIMEOUT did not say which dependency" --command "run --detach"`,
	Args:    cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		note := strings.TrimSpace(strings.Join(args, " "))
		if note == "" {
			exitWithError(utils.ErrUsage, fmt.Errorf("say what happened: corgi feedback note \"<what happened>\""), 2)
			return
		}
		entry := feedbackNote{
			Time:    time.Now().UTC().Truncate(time.Second),
			Version: APP_VERSION,
			Agent:   utils.AgentMode,
			Command: strings.TrimSpace(feedbackCommandFlag),
			Note:    note,
		}
		path, err := appendFeedback(entry)
		if err != nil {
			exitWithError(utils.ErrConfigPath, err, 1)
			return
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"recorded": entry, "path": path})
			return
		}
		fmt.Println("recorded in", path)
	},
}

var feedbackListCmd = &cobra.Command{
	Use:   "list",
	Short: "Print the recorded notes, oldest first",
	Run: func(cmd *cobra.Command, _ []string) {
		notes, err := readFeedback()
		if err != nil {
			exitWithError(utils.ErrConfigRead, err, 1)
			return
		}
		if utils.JSONOutput {
			utils.PrintJSON(notes)
			return
		}
		if len(notes) == 0 {
			fmt.Println("no notes yet - corgi feedback note \"<what happened>\"")
			return
		}
		for _, n := range notes {
			who := "person"
			if n.Agent {
				who = "agent"
			}
			cmdPart := ""
			if n.Command != "" {
				cmdPart = " [" + n.Command + "]"
			}
			fmt.Printf("%s  %s  v%s%s  %s\n", n.Time.Local().Format("2006-01-02 15:04"), who, n.Version, cmdPart, n.Note)
		}
	},
}

var feedbackPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print where the notes are kept",
	Run: func(cmd *cobra.Command, _ []string) {
		path, err := feedbackPath()
		if err != nil {
			exitWithError(utils.ErrConfigPath, err, 1)
			return
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]string{"path": path})
			return
		}
		fmt.Println(path)
	},
}

func init() {
	feedbackNoteCmd.Flags().StringVar(&feedbackCommandFlag, "command", "", "The corgi command it was about, e.g. \"run --detach\"")
	feedbackCmd.AddCommand(feedbackNoteCmd, feedbackListCmd, feedbackPathCmd)
	rootCmd.AddCommand(feedbackCmd)
}

func feedbackPath() (string, error) {
	dir, err := utils.GetUserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "feedback.jsonl"), nil
}

func appendFeedback(n feedbackNote) (string, error) {
	path, err := feedbackPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	line, err := json.Marshal(n)
	if err != nil {
		return "", err
	}
	_, err = f.Write(append(line, '\n'))
	return path, err
}

func readFeedback() ([]feedbackNote, error) {
	path, err := feedbackPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return []feedbackNote{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	notes := []feedbackNote{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var n feedbackNote
		if json.Unmarshal(sc.Bytes(), &n) == nil && n.Note != "" {
			notes = append(notes, n)
		}
	}
	return notes, sc.Err()
}
