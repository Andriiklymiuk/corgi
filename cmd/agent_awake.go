package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

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
  corgi agent awake --for 6h       hold it until then, whatever on/off says
  corgi agent awake --display on   keep the display lit too while the lid is open
  corgi agent awake off    back to holding it per session (the default)
  corgi agent awake        what is set now

The daemon reads the setting every half minute: no restart. With the lid
closed the display is held awake too: the panel is off either way, but once
macOS counts the display as asleep it pauses the apps the agents run in -
what a caffeinate -dimsu in a terminal was for. Off by default: a machine that never sleeps is a flat
battery, so it is the machine owner's call. A closed lid still sleeps the
machine unless sudo pmset -a disablesleep 1, or the sudoers rule from
corgi agent away, which lets corgi set it while the daemon runs.`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAgentAwake,
}

var stayAwakeLine = regexp.MustCompile(`(?m)^stayAwake:.*$`)
var awakeUntilLine = regexp.MustCompile(`(?m)^awakeUntil:.*$`)
var keepDisplayLine = regexp.MustCompile(`(?m)^keepDisplay:.*$`)

func runAgentAwake(cmd *cobra.Command, args []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	path := agentUserConfigPath(dir)

	if until, _ := cmd.Flags().GetString("for"); until != "" {
		span, err := time.ParseDuration(until)
		if err != nil || span <= 0 {
			exitWithError(utils.ErrUsage, fmt.Errorf("--for takes a span like 6h or 90m, not %q", until), 2)
		}
		at := time.Now().Add(span)
		if err := writeUserConfigLine(path, awakeUntilLine, "awakeUntil: "+at.Format(time.RFC3339)); err != nil {
			exitWithError("agent_awake", err, 1)
		}
		utils.Infof("✓ awake until %s - the daemon picks it up within a minute\n", at.Format("Mon 15:04"))
		return
	}
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
			utils.Infof("✓ keepDisplay: true in %s - the screen stays lit while the lid is open\n", path)
		} else {
			utils.Infof("✓ keepDisplay: false in %s - the display may sleep and lock\n", path)
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
	utils.Info("the daemon picks it up within a minute")
}

func printAwakeState(path string) {
	user, err := config.LoadUser(path)
	if err != nil || user == nil {
		user = &config.UserConfig{}
	}
	hold, display := user.AwakeHeld(time.Now())
	switch {
	case user.StayAwake:
		utils.Infof("stayAwake is on (%s) - held for as long as the daemon runs\n", path)
	case hold:
		utils.Infof("awake until %s (%s)\n", user.AwakeUntil, path)
	default:
		utils.Infof("stayAwake is off (%s) - the machine may sleep between sessions\n", path)
		utils.Info("turn it on with `corgi agent awake on`, or for a while with `corgi agent awake --for 6h`")
	}
	if display {
		utils.Info("the screen stays lit while the lid is open (keepDisplay)")
	}
	if hold {
		utils.Info("lid closed: the display is held awake so the agents keep running")
		if runtime.GOOS == "darwin" && !supervisor.SleepDisabled() {
			utils.Info("⚠ SleepDisabled is off - closing the lid still sleeps the Mac: sudo pmset -a disablesleep 1, or the sudoers rule from corgi agent away")
		}
	}
	for _, c := range strayCaffeinates(os.Getpid()) {
		utils.Infof("⚠ %s - the daemon already holds the machine and the display; this one can stop\n", c)
	}
}

// A caffeinate of the person's own in a terminal, beside the daemon's.
var listProcesses = func() string {
	out, _ := exec.Command("ps", "-axo", "pid,args").Output()
	return string(out)
}

func strayCaffeinates(self int) []string {
	return strayCaffeinatesIn(listProcesses(), self)
}

func strayCaffeinatesIn(ps string, self int) []string {
	var found []string
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[1] != "caffeinate" || strings.Contains(line, " -w ") || strings.Contains(line, " -t ") {
			continue
		}
		if pid, _ := strconv.Atoi(f[0]); pid == self {
			continue
		}
		found = append(found, strings.Join(f[1:], " ")+" (pid "+f[0]+")")
	}
	return found
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
	agentAwakeCmd.Flags().String("display", "", "on|off: keep the display lit too while the lid is open, so the screen never locks (macOS caffeinate -d)")
	agentAwakeCmd.Flags().String("for", "", "Hold the lock for this long (6h, 90m), on or off")
	agentCmd.AddCommand(agentAwakeCmd)
}
