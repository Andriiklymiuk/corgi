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

var agentAwakeCmd = &cobra.Command{
	Use:   "awake [on|off]",
	Short: "Keep this machine awake for as long as the agent daemon runs",
	Long: `Without this, corgi holds the machine awake only while a session runs. Between
sessions the laptop sleeps, and a phone tap reaches nothing.

  corgi agent awake on     hold the wake lock for the daemon's whole life
  corgi agent awake --display on   keep the display lit too - no lock screen while it holds
  corgi agent awake off    back to holding it per session (the default)
  corgi agent awake        what is set now

Off by default: a machine that never sleeps is a flat battery, so it is the
machine owner's call. On macOS the lock cannot beat a closed lid on battery -
keep the lid open, or plug in, for a long unattended run.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAgentAwake,
}

var stayAwakeLine = regexp.MustCompile(`(?m)^stayAwake:.*$`)
var keepDisplayLine = regexp.MustCompile(`(?m)^keepDisplay:.*$`)

func runAgentAwake(cmd *cobra.Command, args []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	path := agentUserConfigPath(dir)

	if len(args) == 0 {
		printAwakeState(path)
		return
	}
	if display, _ := cmd.Flags().GetString("display"); display != "" {
		on, err := parseOnOff(display)
		if err != nil {
			exitWithError(utils.ErrUsage, err, 2)
		}
		if err := writeUserConfigLine(path, keepDisplayLine, fmt.Sprintf("keepDisplay: %t", on)); err != nil {
			exitWithError("agent_awake", err, 1)
		}
		if on {
			utils.Infof("✓ keepDisplay: true in %s - the screen stays lit while the wake lock is held (no lock screen); corgi agent restart\n", path)
		} else {
			utils.Infof("✓ keepDisplay: false in %s - the display may sleep and lock; corgi agent restart\n", path)
		}
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
		utils.Info("back to a wake lock per session - the machine may sleep between them")
	}
	utils.Info("run `corgi agent restart` so the running daemon picks it up")
}

func printAwakeState(path string) {
	user, err := config.LoadUser(path)
	if err != nil || user == nil || !user.StayAwake {
		utils.Infof("stayAwake is off (%s) - the machine may sleep between sessions\n", path)
		utils.Info("turn it on with `corgi agent awake on`")
		return
	}
	utils.Infof("stayAwake is on (%s) - held for as long as the daemon runs\n", path)
}

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

func writeStayAwake(path string, on bool) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot read %s: %v", path, err)
	}
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
	return os.WriteFile(path, []byte(body), 0o600)
}

func init() {
	agentAwakeCmd.Flags().String("display", "", "on|off: keep the display lit too while the wake lock holds, so the screen never locks (macOS caffeinate -d)")
	agentCmd.AddCommand(agentAwakeCmd)
}
