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
