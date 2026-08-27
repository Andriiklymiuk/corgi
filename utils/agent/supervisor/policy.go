// Package supervisor keeps a `claude remote-control` process alive across the
// three ways it dies: reboot, crash, and the ~10 minute network-outage exit
// documented at https://code.claude.com/docs/en/remote-control.
package supervisor

import (
	"strconv"
	"strings"
	"time"
)

// ExitCause is why a supervised remote-control process stopped. The cause
// decides whether restarting is useful — an expired login never recovers by
// being retried, while a network timeout always does.
type ExitCause string

const (
	// CauseRequested is a deliberate stop (corgi agent stop, SIGTERM).
	CauseRequested ExitCause = "requested"
	// CauseNetworkTimeout is the documented exit after roughly ten minutes
	// awake without network. Restarting is the correct and only response.
	CauseNetworkTimeout ExitCause = "network-timeout"
	// CauseAuthFailure is a missing subscription, a logged-out account, or an
	// ambient ANTHROPIC_API_KEY that remote control refuses to run alongside.
	CauseAuthFailure ExitCause = "auth-failure"
	// CauseStartupFailure is an exit too fast to have served anything, which
	// means the next attempt will almost certainly fail the same way.
	CauseStartupFailure ExitCause = "startup-failure"
	// CauseCrash is an unexpected exit after a healthy run.
	CauseCrash ExitCause = "crash"
)

// MinHealthyUptime is how long a process must survive before the run counts as
// healthy. Anything shorter is a failed start, not a crash, and repeated failed
// starts stop the loop rather than backing off forever.
const MinHealthyUptime = 60 * time.Second

// MaxStartupFailures is how many consecutive too-fast exits are tolerated
// before the workspace is disabled and left to `corgi agent doctor`.
const MaxStartupFailures = 5

// DefaultBackoff is the capped delay sequence between restarts. The last entry
// repeats for every attempt beyond it.
var DefaultBackoff = []time.Duration{
	5 * time.Second,
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
}

// authFailureMarkers are substrings remote control prints when it will never
// start under the current credentials. Matched case-insensitively against the
// captured output tail.
var authFailureMarkers = []string{
	"requires a claude.ai subscription",
	"not authenticated",
	"claude auth login",
	"invalid api key",
	"oauth token has expired",
}

// trustFailureMarker is what remote control prints for a directory whose Claude
// workspace-trust dialog was never accepted. Retrying cannot accept a dialog,
// so this disables the workspace with instructions instead of looping.
const trustFailureMarker = "workspace not trusted"

// Exit is one observed termination of a supervised process.
type Exit struct {
	Code      int
	Uptime    time.Duration
	Output    string // tail of combined stdout/stderr
	Requested bool   // true when corgi asked it to stop

	// healthyAfter overrides MinHealthyUptime. Unexported so callers outside
	// the package always get the documented threshold; tests set it to keep
	// the suite fast.
	healthyAfter time.Duration
}

// healthyThreshold is how long this run had to last to count as healthy.
func (e Exit) healthyThreshold() time.Duration {
	if e.healthyAfter > 0 {
		return e.healthyAfter
	}
	return MinHealthyUptime
}

// Decision is what the supervisor does about an Exit.
type Decision struct {
	Cause   ExitCause
	Restart bool
	Delay   time.Duration
	Notify  bool
	Disable bool   // stop supervising this workspace until a human intervenes
	Reason  string // shown to the user; must be actionable from a phone
}

// Classify determines why a process exited. consecutiveStartupFailures counts
// prior too-fast exits in this streak.
func Classify(e Exit, consecutiveStartupFailures int) ExitCause {
	if e.Requested {
		return CauseRequested
	}
	// Only trust an auth marker from a run too short to have served anything.
	// The output tail is the SESSION's, so a long-running session that merely
	// printed "not authenticated" — reading a log, discussing an error — would
	// otherwise permanently disable the workspace.
	if e.Uptime < e.healthyThreshold() {
		if hasAuthFailureMarker(e.Output) {
			return CauseAuthFailure
		}
		return CauseStartupFailure
	}
	if e.Code == 0 {
		// Remote control exits cleanly when the network stays unreachable past
		// its timeout, so a healthy run ending in success is that, not a crash.
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

// Decide turns an Exit into an action. attempt is the number of restarts
// already made in this streak; it selects the backoff delay.
func Decide(e Exit, attempt, consecutiveStartupFailures int) Decision {
	cause := Classify(e, consecutiveStartupFailures)
	switch cause {
	case CauseRequested:
		return Decision{Cause: cause, Reason: "stopped on request"}

	case CauseAuthFailure:
		// Retrying cannot produce credentials, and a loop would spam
		// notifications while hiding the real error.
		return Decision{
			Cause:   cause,
			Disable: true,
			Notify:  true,
			Reason:  "remote control could not authenticate — run `corgi agent doctor`",
		}

	case CauseStartupFailure:
		// A missing workspace-trust acceptance never fixes itself by retrying —
		// only a human running `claude` in the folder can accept the dialog. Say
		// exactly that instead of a generic retry line the user has to debug.
		if strings.Contains(strings.ToLower(e.Output), trustFailureMarker) {
			return Decision{
				Cause:   cause,
				Disable: true,
				Notify:  true,
				Reason:  "Claude has not trusted this folder yet — run `claude` in the workspace once, accept the trust dialog, then retry",
			}
		}
		if consecutiveStartupFailures+1 >= MaxStartupFailures {
			return Decision{
				Cause:   cause,
				Disable: true,
				Notify:  true,
				Reason: withLastOutputLine(
					"remote control exited immediately "+strconv.Itoa(consecutiveStartupFailures+1)+" times — run `corgi agent doctor`",
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
			Reason:  "remote control restarted — the previous session ended (network timeout), worktrees kept",
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

// withLastOutputLine appends the child's last non-empty output line to a
// reason, so "exited during startup" carries the actual error ("Workspace not
// trusted…", a stack trace's final line) instead of making the user reproduce
// the failure by hand to see it. No output → the reason stands alone.
func withLastOutputLine(reason, output string) string {
	line := lastOutputLine(output)
	if line == "" {
		return reason
	}
	return reason + " — last output: " + line
}

// maxReasonLineLen caps the quoted output line; reasons render on a phone card.
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

// backoffFor returns the delay for a restart attempt, holding at the last
// configured step so a long outage settles instead of growing without bound.
func backoffFor(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= len(DefaultBackoff) {
		return DefaultBackoff[len(DefaultBackoff)-1]
	}
	return DefaultBackoff[attempt]
}
