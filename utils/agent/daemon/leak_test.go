package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"andriiklymiuk/corgi/utils/agent/supervisor"
)

// The diagnostic exists to say WHICH credential is in play, never WHAT it is.
// Its output is printed at startup, written to status.json, and returned over
// MCP — three places a secret must not reach.
func TestDiagnosticNeverEchoesACredentialValue(t *testing.T) {
	const secret = "sk-ant-this-must-never-be-printed"
	env := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_API_KEY=" + secret,
		"CLAUDE_CODE_OAUTH_TOKEN=" + secret + "-oauth",
		"ANTHROPIC_AUTH_TOKEN=" + secret + "-auth",
	}

	for _, inherit := range []bool{false, true} {
		c := supervisor.SpawnConfig{
			WorkspaceID:       "acme",
			Dir:               "/tmp/acme",
			ConfigDir:         "~/.claude-work",
			InheritAPIKey:     inherit,
			InheritOAuthToken: inherit,
		}

		diag := diagnose(c, env)
		encoded, err := json.Marshal(diag)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("inherit=%v: a credential value reached the diagnostic: %s", inherit, encoded)
		}

		// The names are the useful part and must survive.
		if !inherit {
			var sawKey bool
			for _, name := range diag.Stripped {
				if name == "ANTHROPIC_API_KEY" {
					sawKey = true
				}
				if strings.Contains(name, secret) {
					t.Fatal("the stripped list must hold variable names, not values")
				}
			}
			if !sawKey {
				t.Error("the diagnostic should name which credential was stripped")
			}
		}
	}
}

func TestStatusJSONCarriesNoCredentialValues(t *testing.T) {
	const secret = "sk-ant-leak-check"
	t.Setenv("ANTHROPIC_API_KEY", secret)

	d := New("test", t.TempDir())
	d.buildRunners([]supervisor.SpawnConfig{{
		WorkspaceID: "acme",
		Dir:         "/tmp/acme",
		WakeLock:    supervisor.WakeLockOff,
	}})

	encoded, err := json.Marshal(d.Status())
	if err != nil {
		t.Fatal(err)
	}

	// status.json is world-readable and is what `corgi agent status --json`
	// prints, so anything in it is effectively public to the machine.
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("status output contains a credential value: %s", encoded)
	}
}
