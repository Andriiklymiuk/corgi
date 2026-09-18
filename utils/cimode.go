package utils

import "os"

var CIMode bool

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

var NonInteractive bool

var agentEnvVars = []string{
	"CLAUDECODE",
	"CLAUDE_CODE",
	"ANTHROPIC_AGENT",
}

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

func DetectMode() {
	detectFromEnv()
	if !IsTTY() || !StdinIsTTY() {
		NonInteractive = true
	}
}

func SetInteractive() {
	NonInteractive = false
}

func DetectCIMode() { detectFromEnv() }
