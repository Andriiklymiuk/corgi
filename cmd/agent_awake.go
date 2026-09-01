package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/supervisor"

	"github.com/spf13/cobra"
)

// A wake lock held per session leaves the worst gap uncovered: with nothing
// running, the laptop sleeps and the phone cannot start anything — which is the
// one thing agent mode exists for. `awake on` holds it for the daemon's whole
// life instead, and replaces the `caffeinate` people otherwise leave running in
// a terminal all day.

var agentAwakeCmd = &cobra.Command{
	Use:   "awake [on|off]",
	Short: "Keep this machine awake for as long as the agent daemon runs",
	Long: `Without this, corgi holds the machine awake only while a session runs. Between
sessions the laptop sleeps, and a phone tap reaches nothing.

  corgi agent awake on     hold the wake lock for the daemon's whole life
  corgi agent awake off    back to holding it per session (the default)
  corgi agent awake        what is set now

Off by default: a machine that never sleeps is a flat battery, so it is the
machine owner's call. On macOS the lock cannot beat a closed lid on battery —
keep the lid open, or plug in, for a long unattended run.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAgentAwake,
}

var stayAwakeLine = regexp.MustCompile(`(?m)^stayAwake:.*$`)

func runAgentAwake(_ *cobra.Command, args []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	path := agentUserConfigPath(dir)

	if len(args) == 0 {
		printAwakeState(path)
		return
	}
	on, err := parseOnOff(args[0])
	if err != nil {
		exitWithError(utils.ErrUsage, err, 2)
		return
	}
	if err := writeStayAwake(path, on); err != nil {
		exitWithError("agent_awake", err, 1)
		return
	}
	if on {
		utils.Infof("✓ stayAwake: true in %s\n", path)
		utils.Info("this machine now stays awake for as long as the daemon runs")
		if risk := supervisor.CheckSleepRisk(); risk.AtRisk() {
			utils.Infof("  note: %s\n", risk.Reason)
		}
	} else {
		utils.Infof("✓ stayAwake: false in %s\n", path)
		utils.Info("back to a wake lock per session — the machine may sleep between them")
	}
	utils.Info("run `corgi agent restart` so the running daemon picks it up")
}

func printAwakeState(path string) {
	user, err := config.LoadUser(path)
	if err != nil || user == nil || !user.StayAwake {
		utils.Infof("stayAwake is off (%s) — the machine may sleep between sessions\n", path)
		utils.Info("turn it on with `corgi agent awake on`")
		return
	}
	utils.Infof("stayAwake is on (%s) — held for as long as the daemon runs\n", path)
}

// stayAwakeEnabled reports the setting without printing anything.
func stayAwakeEnabled(dir string) bool {
	user, err := config.LoadUser(agentUserConfigPath(dir))
	return err == nil && user != nil && user.StayAwake
}

func parseOnOff(arg string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "on", "true", "yes":
		return true, nil
	case "off", "false", "no":
		return false, nil
	}
	return false, fmt.Errorf("say `on` or `off`, not %q", arg)
}

// writeStayAwake edits the one line, like the notifyUrl writer: the file is
// hand-edited and commented, and a marshal round-trip would flatten both.
func writeStayAwake(path string, on bool) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot read %s: %v", path, err)
	}
	// A machine that never ran `agent init` has no agent dir yet, and this is a
	// perfectly reasonable first command to run.
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line := fmt.Sprintf("stayAwake: %t", on)
	body := string(data)
	if stayAwakeLine.MatchString(body) {
		body = stayAwakeLine.ReplaceAllString(body, line)
	} else if strings.TrimSpace(body) == "" {
		body = line + "\n"
	} else {
		body = strings.TrimRight(body, "\n") + "\n" + line + "\n"
	}
	// 0600 for the same reason as the rest of this file: it grants capability.
	return os.WriteFile(path, []byte(body), 0o600)
}

func init() {
	agentCmd.AddCommand(agentAwakeCmd)
}
