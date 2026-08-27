package supervisor

import (
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	healthy := 2 * time.Hour

	tests := []struct {
		name string
		exit Exit
		want ExitCause
	}{
		{
			name: "requested stop wins over everything",
			exit: Exit{Requested: true, Code: 1, Uptime: time.Second, Output: "not authenticated"},
			want: CauseRequested,
		},
		{
			// Remote control cannot start without credentials, so a real auth
			// failure always exits immediately.
			name: "auth failure detected from a fast exit",
			exit: Exit{Code: 1, Uptime: time.Second, Output: "Remote Control requires a claude.ai subscription"},
			want: CauseAuthFailure,
		},
		{
			name: "auth marker matched case-insensitively",
			exit: Exit{Code: 1, Uptime: time.Second, Output: "ERROR: NOT AUTHENTICATED"},
			want: CauseAuthFailure,
		},
		{
			name: "auth marker beats a plain startup failure",
			exit: Exit{Code: 1, Uptime: time.Second, Output: "run claude auth login"},
			want: CauseAuthFailure,
		},
		{
			// The output tail belongs to the SESSION. A long healthy run that
			// merely printed the phrase — reading a log, discussing an error —
			// must not permanently disable the workspace.
			name: "auth marker in a long healthy run is not an auth failure",
			exit: Exit{Code: 1, Uptime: healthy, Output: "the user asked why it said not authenticated"},
			want: CauseCrash,
		},
		{
			name: "too-fast exit is a startup failure",
			exit: Exit{Code: 1, Uptime: 3 * time.Second},
			want: CauseStartupFailure,
		},
		{
			name: "clean exit after a healthy run is the network timeout",
			exit: Exit{Code: 0, Uptime: healthy},
			want: CauseNetworkTimeout,
		},
		{
			name: "non-zero exit after a healthy run is a crash",
			exit: Exit{Code: 137, Uptime: healthy},
			want: CauseCrash,
		},
		{
			name: "exit exactly at the healthy boundary is not a startup failure",
			exit: Exit{Code: 0, Uptime: MinHealthyUptime},
			want: CauseNetworkTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.exit, 0); got != tt.want {
				t.Errorf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecideNetworkTimeoutRestartsAndTellsTheUser(t *testing.T) {
	d := Decide(Exit{Code: 0, Uptime: time.Hour}, 0, 0)

	if !d.Restart {
		t.Error("network timeout must restart — it is the whole point of the supervisor")
	}
	if !d.Notify {
		t.Error("a restart must notify: the previous session's context is gone, and silently pretending otherwise misleads the user")
	}
	if d.Disable {
		t.Error("network timeout must not disable the workspace")
	}
}

func TestDecideAuthFailureDoesNotLoop(t *testing.T) {
	d := Decide(Exit{Code: 1, Uptime: time.Second, Output: "requires a claude.ai subscription"}, 0, 0)

	if d.Restart {
		t.Error("auth failure must not restart: retrying cannot produce credentials")
	}
	if !d.Disable {
		t.Error("auth failure must disable the workspace so doctor can explain it")
	}
	if !d.Notify {
		t.Error("auth failure must notify once")
	}
}

func TestDecideRequestedStopStaysStopped(t *testing.T) {
	d := Decide(Exit{Requested: true, Code: 0, Uptime: time.Hour}, 0, 0)

	if d.Restart {
		t.Error("`corgi agent stop` must not be undone by the supervisor")
	}
	if d.Notify {
		t.Error("a deliberate stop should not notify")
	}
}

func TestDecideStopsAfterRepeatedStartupFailures(t *testing.T) {
	fast := Exit{Code: 1, Uptime: time.Second}

	for prior := 0; prior < MaxStartupFailures-1; prior++ {
		if d := Decide(fast, prior, prior); !d.Restart {
			t.Fatalf("with %d prior failures the supervisor should still retry", prior)
		}
	}

	d := Decide(fast, MaxStartupFailures-1, MaxStartupFailures-1)
	if d.Restart {
		t.Error("a persistent startup failure must stop retrying")
	}
	if !d.Disable {
		t.Error("a persistent startup failure must disable the workspace")
	}
}

func TestDecideTrustFailureDisablesWithInstructions(t *testing.T) {
	d := Decide(Exit{Code: 1, Uptime: time.Second,
		Output: "Error: Workspace not trusted. Please run `claude` in /x first"}, 0, 0)

	if d.Restart {
		t.Error("retrying cannot accept a trust dialog — must not loop")
	}
	if !d.Disable || !d.Notify {
		t.Error("a trust failure must disable and notify")
	}
	if !strings.Contains(d.Reason, "trust dialog") {
		t.Errorf("the reason must say how to fix it, got %q", d.Reason)
	}
}

func TestDecideStartupFailureCarriesTheChildsLastLine(t *testing.T) {
	d := Decide(Exit{Code: 1, Uptime: time.Second,
		Output: "some noise\nError: config file corrupt\n\n"}, 0, 0)

	if !strings.Contains(d.Reason, "Error: config file corrupt") {
		t.Errorf("the reason must quote the child's last output line so nobody reproduces the failure by hand to see it, got %q", d.Reason)
	}
	// No output at all → the generic reason stands alone, no dangling suffix.
	if d := Decide(Exit{Code: 1, Uptime: time.Second}, 0, 0); strings.Contains(d.Reason, "last output") {
		t.Errorf("no output must mean no quote suffix, got %q", d.Reason)
	}
}

func TestBackoffIsCapped(t *testing.T) {
	last := DefaultBackoff[len(DefaultBackoff)-1]

	for _, attempt := range []int{len(DefaultBackoff), 50, 10000} {
		if got := backoffFor(attempt); got != last {
			t.Errorf("backoffFor(%d) = %v, want the capped %v", attempt, got, last)
		}
	}
	if got := backoffFor(-1); got != DefaultBackoff[0] {
		t.Errorf("backoffFor(-1) = %v, want %v", got, DefaultBackoff[0])
	}
}

func TestBackoffIsMonotonic(t *testing.T) {
	for i := 1; i < len(DefaultBackoff); i++ {
		if DefaultBackoff[i] <= DefaultBackoff[i-1] {
			t.Errorf("backoff step %d (%v) does not grow past %v", i, DefaultBackoff[i], DefaultBackoff[i-1])
		}
	}
}
