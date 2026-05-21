package utils

import (
	"testing"
)

func TestSetCIMode(t *testing.T) {
	orig := CIMode
	defer func() { CIMode = orig }()

	SetCIMode(true)
	if !CIMode {
		t.Error("expected CIMode=true after SetCIMode(true)")
	}
	SetCIMode(false)
	if CIMode {
		t.Error("expected CIMode=false after SetCIMode(false)")
	}
}

func TestDetectCIMode(t *testing.T) {
	orig := CIMode
	defer func() { CIMode = orig }()

	cases := []struct {
		val      string
		expected bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true},
		{"false", false},
		{"", false},
		{"0", false},
	}
	for _, tc := range cases {
		clearCIEnv(t)
		CIMode = false
		t.Setenv("CI", tc.val)
		DetectCIMode()
		if CIMode != tc.expected {
			t.Errorf("CI=%q: expected CIMode=%v, got %v", tc.val, tc.expected, CIMode)
		}
	}
}

func TestDetectCIMode_OtherProviders(t *testing.T) {
	orig := CIMode
	defer func() { CIMode = orig }()

	for _, k := range []string{"GITHUB_ACTIONS", "GITLAB_CI", "CIRCLECI", "BUILDKITE", "JENKINS_URL", "TEAMCITY_VERSION", "TRAVIS", "DRONE", "BITBUCKET_BUILD_NUMBER", "CODEBUILD_BUILD_ID"} {
		clearCIEnv(t)
		CIMode = false
		t.Setenv(k, "true")
		DetectCIMode()
		if !CIMode {
			t.Errorf("%s=true: expected CIMode=true", k)
		}
	}
}

func clearCIEnv(t *testing.T) {
	for _, k := range ciEnvVars {
		t.Setenv(k, "")
	}
}

func TestDetectModeNonInteractive(t *testing.T) {
	cases := []struct {
		name   string
		env    map[string]string
		wantNI bool
	}{
		{"agent env CLAUDECODE", map[string]string{"CLAUDECODE": "1"}, true},
		{"ci env", map[string]string{"CI": "true"}, true},
		{"clean env", map[string]string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range append(append([]string{}, ciEnvVars...), agentEnvVars...) {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			NonInteractive = false
			CIMode = false
			detectFromEnv() // env-only check, TTY-independent
			if NonInteractive != tc.wantNI {
				t.Errorf("NonInteractive = %v, want %v", NonInteractive, tc.wantNI)
			}
		})
	}
}

func TestSetInteractive(t *testing.T) {
	orig := NonInteractive
	defer func() { NonInteractive = orig }()
	NonInteractive = true
	SetInteractive()
	if NonInteractive {
		t.Error("expected NonInteractive=false after SetInteractive")
	}
}

func TestDetectMode_NonTTYImpliesNonInteractive(t *testing.T) {
	origNI, origCI := NonInteractive, CIMode
	defer func() { NonInteractive, CIMode = origNI, origCI }()
	for _, k := range append(append([]string{}, ciEnvVars...), agentEnvVars...) {
		t.Setenv(k, "")
	}
	NonInteractive = false
	CIMode = false
	DetectMode()
	// go test runs with piped stdio, so the TTY check must flip NonInteractive on.
	if !NonInteractive {
		t.Error("expected NonInteractive=true when stdio is not a TTY")
	}
}
