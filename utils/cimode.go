package utils

import "os"

// CIMode is true when corgi is running in a CI environment.
// When true: spinners, banners, and random quotes are suppressed;
// output is plain text suitable for log parsers.
var CIMode bool

// ciEnvVars are the environment variables checked by DetectCIMode. Most
// CI systems set the generic "CI" var; the others act as belt-and-suspenders
// for scripts inside those environments that may have unset CI.
var ciEnvVars = []string{
	"CI",
	"GITHUB_ACTIONS",
	"GITLAB_CI",
	"CIRCLECI",
	"BUILDKITE",
	"JENKINS_URL",
	"TEAMCITY_VERSION",
	"TRAVIS",
	"DRONE",
	"BITBUCKET_BUILD_NUMBER",
	"CODEBUILD_BUILD_ID",
}

// NonInteractive is true when prompts must be skipped: CI, an AI agent, or no TTY.
var NonInteractive bool

var agentEnvVars = []string{
	"CLAUDECODE",
	"CLAUDE_CODE",
	"ANTHROPIC_AGENT",
}

// SetCIMode enables or disables CI mode.
func SetCIMode(enabled bool) {
	CIMode = enabled
}

func anyEnvSet(keys []string) bool {
	for _, k := range keys {
		v := os.Getenv(k)
		if v == "" || v == "false" || v == "0" {
			continue
		}
		return true
	}
	return false
}

func detectFromEnv() {
	if anyEnvSet(ciEnvVars) {
		CIMode = true
		NonInteractive = true
	}
	if anyEnvSet(agentEnvVars) {
		NonInteractive = true
	}
}

// DetectMode auto-detects CI and non-interactive mode from environment and TTY.
func DetectMode() {
	detectFromEnv()
	if !IsTTY() || !StdinIsTTY() {
		NonInteractive = true
	}
}

func SetInteractive() {
	NonInteractive = false
}

// DetectCIMode is kept for compatibility; prefer DetectMode.
func DetectCIMode() { detectFromEnv() }
