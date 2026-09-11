package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"github.com/spf13/cobra"
)

// A session that hit a limit waits on a clock. The editor extension can type
// "continue" when the window resets, but only while the editor is open; the
// daemon can do it from anywhere. Off by default because it types into a
// terminal that is yours.
var agentContinueCmd = &cobra.Command{
	Use:   "continue [on|off]",
	Short: "Let the daemon continue a limited session when its limit is over",
	Long: `A session that hit a usage limit is waiting on a clock, not on you. With this
on, the daemon plans a resume from the account's reset time (or a few minutes
after an overload), types "continue" once the numbers say the window is back,
and gives up after a few tries if the limit comes straight back.

  corgi agent continue on
  corgi agent continue off
  corgi agent continue        what is set now

The board shows the plan on the key ("continues 14:02"). The VS Code extension
has the same feature; when the daemon does it, the extension stands down.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAgentContinue,
}

var autoContinueLine = regexp.MustCompile(`(?m)^autoContinue:.*$`)

func runAgentContinue(_ *cobra.Command, args []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	path := agentUserConfigPath(dir)
	if len(args) == 0 {
		user, err := config.LoadUser(path)
		if err != nil || user == nil || !user.AutoContinue {
			utils.Infof("autoContinue is off (%s) — a limited session waits for you\n", path)
			utils.Info("turn it on with `corgi agent continue on`")
			return
		}
		utils.Infof("autoContinue is on (%s) — the daemon continues limited sessions when their window resets\n", path)
		return
	}
	on, err := parseOnOff(args[0])
	if err != nil {
		exitWithError(utils.ErrUsage, err, 2)
		return
	}
	if err := writeUserConfigLine(path, autoContinueLine, fmt.Sprintf("autoContinue: %t", on)); err != nil {
		exitWithError("agent_continue", err, 1)
		return
	}
	if on {
		utils.Infof("✓ autoContinue: true in %s\n", path)
		utils.Info("the daemon will type \"continue\" into a limited session once its window resets")
	} else {
		utils.Infof("✓ autoContinue: false in %s\n", path)
	}
	utils.Info("run `corgi agent restart` so the running daemon picks it up")
}

// writeUserConfigLine edits one line of the hand-edited config, like
// writeStayAwake: a marshal round-trip would flatten the comments.
func writeUserConfigLine(path string, line *regexp.Regexp, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot read %s: %v", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body := string(data)
	switch {
	case line.MatchString(body):
		body = line.ReplaceAllString(body, value)
	case strings.TrimSpace(body) == "":
		body = value + "\n"
	default:
		body = strings.TrimRight(body, "\n") + "\n" + value + "\n"
	}
	return os.WriteFile(path, []byte(body), 0o600)
}

func init() {
	agentCmd.AddCommand(agentContinueCmd)
}
