package supervisor

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ExitCause string

const (
	CauseRequested       ExitCause = "requested"
	CauseNetworkTimeout  ExitCause = "network-timeout"
	CauseAuthFailure     ExitCause = "auth-failure"
	CauseStartupFailure  ExitCause = "startup-failure"
	CauseCrash           ExitCause = "crash"
	CauseUnsupportedFlag ExitCause = "unsupported-flag"
)

const MinHealthyUptime = 60 * time.Second

const MaxStartupFailures = 5

var DefaultBackoff = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
}

var authFailureMarkers = []string{
	"requires a claude.ai subscription",
	"not authenticated",
	"claude auth login",
	"invalid api key",
	"oauth token has expired",
}

const trustFailureMarker = "workspace not trusted"

type Exit struct {
	Code      int
	Uptime    time.Duration
	Output    string
	Requested bool

	healthyAfter time.Duration
}

func (e Exit) healthyThreshold() time.Duration {
	if e.healthyAfter > 0 {
		return e.healthyAfter
	}
	return MinHealthyUptime
}

type Decision struct {
	Cause   ExitCause
	Restart bool
	Delay   time.Duration
	Notify  bool
	Disable bool
	Reason  string
}

func Classify(e Exit, consecutiveStartupFailures int) ExitCause {
	if e.Requested {
		return CauseRequested
	}
	if e.Uptime < e.healthyThreshold() {
		if hasAuthFailureMarker(e.Output) {
			return CauseAuthFailure
		}
		return CauseStartupFailure
	}
	if e.Code == 0 {
		return CauseNetworkTimeout
	}
	return CauseCrash
}

func hasAuthFailureMarker(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range authFailureMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func Decide(e Exit, attempt, consecutiveStartupFailures int) Decision {
	cause := Classify(e, consecutiveStartupFailures)
	switch cause {
	case CauseRequested:
		return Decision{Cause: cause, Reason: "stopped on request"}

	case CauseAuthFailure:
		return Decision{
			Cause:   cause,
			Disable: true,
			Notify:  true,
			Reason:  "remote control could not authenticate - run `corgi agent doctor`",
		}

	case CauseStartupFailure:
		if strings.Contains(strings.ToLower(e.Output), trustFailureMarker) {
			return Decision{
				Cause:   cause,
				Disable: true,
				Notify:  true,
				Reason:  "Claude has not trusted this folder yet - run `claude` in the workspace once, accept the trust dialog, then retry",
			}
		}
		if consecutiveStartupFailures+1 >= MaxStartupFailures {
			return Decision{
				Cause:   cause,
				Disable: true,
				Notify:  true,
				Reason: withLastOutputLine(
					"remote control exited immediately "+strconv.Itoa(consecutiveStartupFailures+1)+" times - run `corgi agent doctor`",
					e.Output),
			}
		}
		return Decision{
			Cause:   cause,
			Restart: true,
			Delay:   backoffFor(attempt),
			Reason:  withLastOutputLine("remote control exited during startup, retrying", e.Output),
		}

	case CauseNetworkTimeout:
		return Decision{
			Cause:   cause,
			Restart: true,
			Delay:   backoffFor(attempt),
			Notify:  true,
			Reason:  "remote control restarted - the previous session ended after " + roughUptime(e.Uptime) + " (network timeout), worktrees kept",
		}

	default:
		return Decision{
			Cause:   CauseCrash,
			Restart: true,
			Delay:   backoffFor(attempt),
			Notify:  true,
			Reason: withLastOutputLine(
				"remote control restarted after an unexpected exit ("+exitDetail(e)+")",
				e.Output),
		}
	}
}

func exitDetail(e Exit) string {
	if e.Code < 0 {
		return "killed by a signal after " + roughUptime(e.Uptime)
	}
	return "code " + strconv.Itoa(e.Code) + " after " + roughUptime(e.Uptime)
}

func roughUptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return strconv.Itoa(h) + "h"
		}
		return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

func withLastOutputLine(reason, output string) string {
	line := lastOutputLine(output)
	if line == "" {
		return reason
	}
	return reason + " - last output: " + line
}

const maxReasonLineLen = 160

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// CleanOutput is the process tail with terminal escapes gone, so it can go in
// a log file or a notification.
func CleanOutput(output string) string {
	cleaned := ansiEscape.ReplaceAllString(output, "")
	return strings.ReplaceAll(cleaned, "\r", "")
}

func lastOutputLine(output string) string {
	lines := strings.Split(CleanOutput(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(strings.ReplaceAll(lines[i], "\r", ""))
		if line == "" || strings.Trim(line, "─│┌┐└┘├┤┬┴┼═║╔╗╚╝╠╣╦╩╬╭╮╯╰ ") == "" {
			continue
		}
		if len(line) > maxReasonLineLen {
			line = line[:maxReasonLineLen] + "…"
		}
		return line
	}
	return ""
}

func backoffFor(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(DefaultBackoff) {
		return DefaultBackoff[len(DefaultBackoff)-1]
	}
	return DefaultBackoff[attempt]
}
