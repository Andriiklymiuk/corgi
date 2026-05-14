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

// SetCIMode enables or disables CI mode.
func SetCIMode(enabled bool) {
	CIMode = enabled
}

// DetectCIMode auto-enables CIMode when any known CI environment variable
// is set to a non-empty, non-falsy value.
func DetectCIMode() {
	for _, k := range ciEnvVars {
		v := os.Getenv(k)
		if v == "" || v == "false" || v == "0" {
			continue
		}
		CIMode = true
		return
	}
}
