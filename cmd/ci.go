package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var ciCmd = &cobra.Command{
	Use:   "ci",
	Short: "Wire this workspace into a CI pipeline",
}

var ciInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a starter pipeline that boots the whole stack and runs its e2e",
	Long: `Write the pipeline that boots this compose in CI and runs the stack's e2e
suite against it, for GitHub Actions or GitLab CI.

The provider is taken from the git remote when --provider is omitted. Existing
files are never overwritten without --force.

GitHub gets .github/workflows/stack-e2e.yml, which installs corgi through the
official action and reads the cache plan from its outputs.

GitLab gets .gitlab-ci.yml, which includes corgi's published job template, plus
.gitlab/corgi-cache.yml generated from this compose — GitLab cannot read the
plan at runtime, so it is committed and guarded by
` + "`corgi cache paths --gitlab --check`" + `.

The result is a starting point: it still needs the runner tags, the secrets, and
the participating repos of this workspace.

Examples:
  corgi ci init
  corgi ci init --provider gitlab
  corgi ci init --provider github --force`,
	Run: runCIInit,
}

func init() {
	rootCmd.AddCommand(ciCmd)
	ciCmd.AddCommand(ciInitCmd)
	ciInitCmd.Flags().String("provider", "", "github or gitlab; detected from the git remote when omitted")
	ciInitCmd.Flags().Bool("force", false, "Overwrite files that already exist")
}

func runCIInit(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		exitWithError(utils.ErrConfig, err, 1)
		return
	}

	provider, err := resolveCIProvider(cmd)
	if err != nil {
		exitWithError(utils.ErrUsage, err, 1)
		return
	}

	force, _ := cmd.Flags().GetBool("force")
	written, err := writeCIFiles(provider, corgi, force)
	if err != nil {
		exitWithError(utils.ErrExecFailed, err, 1)
		return
	}

	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"provider": provider, "written": written})
		return
	}
	for _, f := range written {
		utils.Infof("wrote %s\n", f)
	}
	utils.Info(ciNextSteps(provider, corgi))
}

// resolveCIProvider prefers the flag, then the origin remote. Guessing wrong
// would write a pipeline for the wrong forge, so an unknown remote is an error
// rather than a default.
func resolveCIProvider(cmd *cobra.Command) (string, error) {
	flag, _ := cmd.Flags().GetString("provider")
	switch strings.ToLower(strings.TrimSpace(flag)) {
	case "github", "gitlab":
		return strings.ToLower(strings.TrimSpace(flag)), nil
	case "":
	default:
		return "", fmt.Errorf("unknown provider %q; use github or gitlab", flag)
	}

	remote := gitOriginURL()
	switch {
	case strings.Contains(remote, "github.com"):
		return "github", nil
	case strings.Contains(remote, "gitlab"):
		return "gitlab", nil
	}
	return "", fmt.Errorf(
		"could not tell the forge from the git remote (%q) — pass --provider github|gitlab", remote)
}

// gitOriginURL is overridable in tests.
var gitOriginURL = func() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func writeCIFiles(provider string, corgi *utils.CorgiCompose, force bool) ([]string, error) {
	files := map[string]string{}
	if provider == "github" {
		files[filepath.Join(".github", "workflows", "stack-e2e.yml")] = githubWorkflowTemplate()
	} else {
		files[".gitlab-ci.yml"] = gitlabPipelineTemplate()
		files[filepath.Join(".gitlab", "corgi-cache.yml")] =
			utils.GitLabCacheYAML(utils.CachePathsFor(corgi), utils.GitLabCacheOptions{})
	}

	var written []string
	for name := range files {
		path := filepath.Join(utils.CorgiComposePathDir, name)
		if !force {
			if _, err := os.Stat(path); err == nil {
				return nil, fmt.Errorf("%s already exists — pass --force to overwrite it", name)
			}
		}
		if err := writeGeneratedFile(path, files[name]); err != nil {
			return nil, err
		}
		written = append(written, name)
	}
	sort.Strings(written)
	return written, nil
}

func ciNextSteps(provider string, corgi *utils.CorgiCompose) string {
	var steps []string
	if provider == "gitlab" {
		steps = append(steps,
			"Set runner_tags to a shell or VM-backed runner. A docker-executor job\n"+
				"     cannot reach the database containers it starts, and the job will say so.",
			"Let each service project use this one's CI_JOB_TOKEN\n"+
				"     (Settings > CI/CD > Job token permissions).",
			"Commit .gitlab/corgi-cache.yml and regenerate it whenever services change.")
	} else {
		steps = append(steps,
			"Add a token that can clone every service repo, if any are private.",
			"Call this workflow from each service repo so one branch boots the stack.")
	}
	steps = append(steps,
		"Provide whatever copyEnvFromFilePath points at — those files are\n"+
			"     gitignored, so no runner has them. corgi doctor names the missing ones.")

	if corgi.E2E == nil {
		steps = append(steps,
			"Declare an e2e: block, or drop the `corgi test --e2e` step — there is\n"+
				"     no stack-level suite in this compose yet.")
	}

	var b strings.Builder
	b.WriteString("\nBefore this runs green:\n")
	for i, step := range steps {
		fmt.Fprintf(&b, "  %d. %s\n", i+1, step)
	}
	return b.String()
}

func githubWorkflowTemplate() string {
	return `# Boots the whole stack from the branches under review and runs its e2e.
# Generated by ` + "`corgi ci init`" + `; adapt it to this workspace.
name: Stack e2e

on:
  workflow_call:
    inputs:
      branch:
        description: Branch name to look for in every service repo
        required: true
        type: string
  pull_request:

concurrency:
  group: stack-e2e-${{ github.repository }}-${{ inputs.branch || github.head_ref }}
  cancel-in-progress: true

jobs:
  stack-e2e:
    # Never jobs.<id>.container: the database containers publish to localhost,
    # which a containerised job no longer shares.
    runs-on: ubuntu-latest
    timeout-minutes: 60
    env:
      BRANCH: ${{ inputs.branch || github.head_ref || 'main' }}
    steps:
      - uses: actions/checkout@v5

      - uses: Andriiklymiuk/corgi@v` + APP_VERSION + `
        id: corgi

      # Four slots because a workflow expression cannot loop. The action warns
      # by itself when an ecosystem does not fit.
      - uses: actions/cache@v4
        if: steps.corgi.outputs.cache-1-key != ''
        with:
          path: ${{ steps.corgi.outputs.cache-1-paths }}
          key: ${{ steps.corgi.outputs.cache-1-key }}
      - uses: actions/cache@v4
        if: steps.corgi.outputs.cache-2-key != ''
        with:
          path: ${{ steps.corgi.outputs.cache-2-paths }}
          key: ${{ steps.corgi.outputs.cache-2-key }}
      - uses: actions/cache@v4
        if: steps.corgi.outputs.cache-3-key != ''
        with:
          path: ${{ steps.corgi.outputs.cache-3-paths }}
          key: ${{ steps.corgi.outputs.cache-3-key }}
      - uses: actions/cache@v4
        if: steps.corgi.outputs.cache-4-key != ''
        with:
          path: ${{ steps.corgi.outputs.cache-4-paths }}
          key: ${{ steps.corgi.outputs.cache-4-key }}

      - run: corgi init --depth 1 --feature "$BRANCH"

      # Fails in seconds on a missing env file, a busy port or a missing tool,
      # rather than at the readiness timeout naming the wrong service.
      - run: corgi doctor

      - run: corgi run --feature "$BRANCH" --detach --wait --wait-timeout 25m --follow
        timeout-minutes: 30

      - run: corgi status --json

      - run: corgi test --e2e

      - if: always()
        run: corgi logs --dump ./ci-artifacts/logs || true

      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: stack-e2e-${{ github.run_id }}
          path: ci-artifacts/
          retention-days: 7

      - if: always()
        run: corgi stop || true
`
}

func gitlabPipelineTemplate() string {
	return `# Boots the whole stack from the branches under review and runs its e2e.
# Generated by ` + "`corgi ci init`" + `; adapt it to this workspace.
include:
  - remote: https://raw.githubusercontent.com/Andriiklymiuk/corgi/v` + APP_VERSION + `/gitlab/corgi.yml
    inputs:
      corgi_version: "` + APP_VERSION + `"
      # A docker-executor runner cannot reach the database containers it starts.
      runner_tags: []
  # Generated: corgi cache paths --gitlab --out .gitlab/corgi-cache.yml
  - local: .gitlab/corgi-cache.yml

stages:
  - test

stack-e2e:
  extends: [.corgi-stack-e2e, .corgi-cache]
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"

# Without this the committed cache plan silently stops matching the compose.
corgi-cache-drift:
  extends: .corgi-setup
  script:
    - corgi cache paths --gitlab --check .gitlab/corgi-cache.yml
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
`
}
