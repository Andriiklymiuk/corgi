package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"andriiklymiuk/corgi/utils/agent/daemon"

	"github.com/spf13/cobra"
)

var agentHeatCmd = &cobra.Command{
	Use:   "heat [auto|pause|off]",
	Short: "What the daemon does when the laptop gets hot in a bag",
	Long: `The daemon reads the battery temperature and the thermal state once a
minute. Warm (40°C, or macOS holding the CPU under 60%): no new fix starts,
one push. Hot (45°C): the running sessions are paused (SIGSTOP) until it
cools, one push; they resume on their own, with a push, under 38°C. With the
sudoers rule from corgi agent away, Low Power Mode comes on while it is warm,
and at 50°C with the lid closed the laptop goes to sleep until you open it.

  corgi agent heat          # the reading now, and the setting
  corgi agent heat auto     # pause sessions only with the lid closed (default)
  corgi agent heat pause    # pause them lid open or closed
  corgi agent heat off      # never pause; new runs still wait for it to cool

Restart the daemon after a change (corgi agent restart).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := agentDir()
		if err != nil {
			return err
		}
		path := agentUserConfigPath(dir)
		if len(args) == 0 {
			user, _ := config.LoadUser(path)
			printHeat(user)
			return nil
		}
		mode := strings.ToLower(strings.TrimSpace(args[0]))
		switch mode {
		case "auto", "pause", "off":
		default:
			return fmt.Errorf("heat takes auto, pause or off, not %q", args[0])
		}
		value := "heat: " + mode
		if mode == "auto" {
			value = `heat: ""`
		}
		if err := writeUserConfigLine(path, heatLine, value); err != nil {
			return err
		}
		utils.Infof("✓ heat %s in %s - corgi agent restart\n", mode, path)
		return nil
	},
}

var heatLine = regexp.MustCompile(`(?m)^heat:.*$`)

func printHeat(user *config.UserConfig) {
	m := daemon.ReadMachineNow()
	if m.Known {
		utils.Infof("now: %s, battery %d%% (%s)\n", m.Words(), m.Battery, powerWords(m.Charging))
	}
	mode := ""
	if user != nil {
		mode = strings.ToLower(strings.TrimSpace(user.Heat))
	}
	switch mode {
	case "pause", "always", "on":
		utils.Info("heat: pause - hot pauses the sessions, lid open or closed")
	case "off", "false", "no":
		utils.Info("heat: off - hot pauses nothing; new runs wait for it to cool")
	default:
		utils.Info("heat: auto - hot pauses the sessions when the lid is closed")
	}
}

func powerWords(charging bool) string {
	if charging {
		return "plugged in"
	}
	return "on battery"
}

func init() {
	agentCmd.AddCommand(agentHeatCmd)
}
