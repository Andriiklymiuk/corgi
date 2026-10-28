package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/config"
	"github.com/spf13/cobra"
)

var agentContinueCmd = &cobra.Command{
	Use:   "continue [on|off]",
	Short: "Let the daemon continue a limited session when its limit is over",
	Long: `A session that hit a usage limit is waiting on a clock, not on you. With this
on, the daemon plans a resume from the account's reset time (or the reset in
the limit message when those numbers are older than the limit, or a few minutes
after an overload), types "continue" once the window is back, and gives up
after a few tries if the limit comes straight back.

  corgi agent continue on
  corgi agent continue off
  corgi agent continue        what is set now

The board shows the plan on the key ("continues 14:02"). It types into a
terminal, a VS Code terminal or tmux, and into the Claude Code panel through
the corgi VS Code extension (macOS: it fills the chat box and presses Return,
so VS Code needs Accessibility once).

Typing needs a window in front and an unlocked screen, and a session the
daemon runs (corgi agent) has no terminal at all, so on macOS "on" also sets
Claude Code's unattended retry mode for every app started from now on
(launchctl setenv CLAUDE_CODE_RETRY_WATCHDOG 1, with a five-minute early-lift
check and a six-hour ceiling): a session at a usage limit waits out the reset
inside the request and carries on, no typing needed. Restart VS Code and the
terminals you start sessions from once, so they pick it up. "off" unsets it.`,
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
			utils.Infof("autoContinue is off (%s) - a limited session waits for you\n", path)
			utils.Info("turn it on with `corgi agent continue on`")
			return
		}
		utils.Infof("autoContinue is on (%s) - the daemon continues limited sessions when their window resets\n", path)
		if runtime.GOOS == "darwin" && !waitOutLimitsSet() {
			utils.Info("sessions started from VS Code or a terminal still rely on typing - `corgi agent continue on` once more sets CLAUDE_CODE_RETRY_WATCHDOG for apps you start")
		}
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
	setWaitOutLimitsEnv(on)
	utils.Info("run `corgi agent restart` so the running daemon picks it up")
}

// waitOutLimitsEnv is Claude Code's unattended retry mode, the same three
// values the daemon gives the sessions it runs: at a usage limit the
// request waits for the reset, checks every five minutes whether it lifted
// early, and gives up on a reset more than six hours out.
var waitOutLimitsEnv = [][2]string{
	{"CLAUDE_CODE_RETRY_WATCHDOG", "1"},
	{"CLAUDE_CODE_RETRY_WATCHDOG_USAGE_LIMIT_RECHECK_MS", "300000"},
	{"CLAUDE_CODE_RETRY_WATCHDOG_MAX_USAGE_LIMIT_WAIT_MS", "21600000"},
}

var launchctlEnv = func(args ...string) ([]byte, error) { return runSupervisorCommand("launchctl", args...) }

// setWaitOutLimitsEnv puts the retry mode into the launchd user session on
// macOS, where VS Code, terminals and the sessions they start read their
// environment from - or takes it out. Elsewhere it prints the lines.
func setWaitOutLimitsEnv(on bool) {
	if runtime.GOOS != "darwin" {
		if on {
			utils.Info("sessions you start yourself wait out a limit when their environment has:")
			for _, kv := range waitOutLimitsEnv {
				utils.Infof("  export %s=%s\n", kv[0], kv[1])
			}
		}
		return
	}
	for _, kv := range waitOutLimitsEnv {
		var err error
		if on {
			_, err = launchctlEnv("setenv", kv[0], kv[1])
		} else {
			_, err = launchctlEnv("unsetenv", kv[0])
		}
		if err != nil {
			utils.Infof("could not set %s for apps you start: %v\n", kv[0], err)
			return
		}
	}
	if on {
		utils.Info("✓ sessions started from now on wait out a usage limit themselves (CLAUDE_CODE_RETRY_WATCHDOG=1 in the launchd environment) - restart VS Code and your terminals once")
	} else {
		utils.Info("✓ CLAUDE_CODE_RETRY_WATCHDOG is no longer set for apps you start")
	}
}

// waitOutLimitsSet reports whether the launchd user environment carries the
// retry mode, for corgi agent continue and corgi agent setup.
func waitOutLimitsSet() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	out, err := launchctlEnv("getenv", waitOutLimitsEnv[0][0])
	return err == nil && strings.TrimSpace(string(out)) == waitOutLimitsEnv[0][1]
}

func checkWaitOutLimits(dir string) agentCheck {
	c := agentCheck{Name: "limits waited out", OK: true}
	user, err := config.LoadUser(agentUserConfigPath(dir))
	if err != nil || user == nil || !user.AutoContinue {
		c.Detail = "off - a limited session waits for you"
		c.Fix = "`corgi agent continue on`"
		return c
	}
	switch {
	case runtime.GOOS != "darwin":
		c.Detail = "the daemon's own sessions wait out their limits; others when CLAUDE_CODE_RETRY_WATCHDOG=1 is in their environment"
	case waitOutLimitsSet():
		c.Detail = "the daemon types continue, and every session started from now on waits out its limit itself"
	default:
		c.Detail = "the daemon types continue; VS Code panel and terminal sessions rely on a window in front"
		c.Fix = "`corgi agent continue on` once more sets CLAUDE_CODE_RETRY_WATCHDOG for apps you start; then restart VS Code"
	}
	return c
}

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
