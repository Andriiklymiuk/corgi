package cmd

import (
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

func TestHealCrashed_RecoversWithinRetries(t *testing.T) {
	policy := &utils.RestartPolicy{Mode: "on-failure", MaxRetries: 3, BackoffSeconds: 2}
	calls := 0
	relaunch := func() bool { calls++; return calls == 2 } // alive on 2nd try
	var slept time.Duration
	sleep := func(d time.Duration) { slept += d }

	recovered, attempts := healCrashed(policy, relaunch, sleep)
	if !recovered || attempts != 2 {
		t.Fatalf("want recovered after 2 attempts, got recovered=%v attempts=%d", recovered, attempts)
	}
	if slept != 4*time.Second {
		t.Fatalf("want 2 backoffs of 2s = 4s, got %v", slept)
	}
}

func TestHealCrashed_ExhaustsRetries(t *testing.T) {
	policy := &utils.RestartPolicy{Mode: "on-failure", MaxRetries: 2}
	relaunch := func() bool { return false }
	recovered, attempts := healCrashed(policy, relaunch, func(time.Duration) {})
	if recovered || attempts != 2 {
		t.Fatalf("want exhausted after 2, got recovered=%v attempts=%d", recovered, attempts)
	}
}

func TestHealCrashed_DisabledPolicies(t *testing.T) {
	cases := []*utils.RestartPolicy{
		nil,
		{Mode: "never", MaxRetries: 3},
		{Mode: "on-failure", MaxRetries: 0},
	}
	for _, p := range cases {
		called := false
		recovered, attempts := healCrashed(p, func() bool { called = true; return true }, func(time.Duration) {})
		if recovered || attempts != 0 || called {
			t.Fatalf("policy %+v should be a no-op", p)
		}
	}
}
