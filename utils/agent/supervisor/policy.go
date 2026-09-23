package supervisor

import (
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
			Reason:  "remote control restarted - the previous session ended (network timeout), worktrees kept",
		}

	default:
		return Decision{
			Cause:   CauseCrash,
			Restart: true,
			Delay:   backoffFor(attempt),
			Notify:  true,
			Reason:  "remote control restarted after an unexpected exit",
		}
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

func lastOutputLine(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
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
