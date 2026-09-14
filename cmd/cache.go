package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "What a CI cache should persist between runs",
}

var cachePathsCmd = &cobra.Command{
	Use:   "paths",
	Short: "Print the directories worth caching, and a key derived from every cacheKey file",
	Long: `Print the dependency directories a CI cache should persist, and a key that
changes whenever any beforeStart cacheKey file changes.

Derived from the compose file, so the list cannot drift when a service is added.
A service opts in by giving a beforeStart step a cacheKey; without one corgi
cannot skip that install anyway.

corgi_services/.cache is always included. It holds the markers for steps whose
install has no directory of its own (go.sum, for one). A step that produces a
dependency directory keeps its marker inside it (node_modules/.corgi-step-0),
so one cache entry carries both and a stale restore cannot pass as fresh.

GitHub Actions can read this plan at runtime through the corgi action's outputs.
GitLab cannot — its cache config is static YAML — so --gitlab renders a job
template to commit, and --check fails when that file no longer matches the
compose file.

Run it after the service directories exist. A cacheKey file that is not on
disk yet hashes to a fixed marker, so the key comes out the same on every run
and the cache never invalidates — in a workflow that means the plan belongs
after corgi init, not before. The command warns when that happens; --strict
turns the warning into exit 1, and --json reports it as complete: false with
the files under missingFiles.

Examples:
  corgi cache paths
  corgi cache paths --json
  corgi cache paths --key
  corgi cache paths --json --strict
  corgi cache paths --gitlab --out .gitlab/corgi-cache.yml
  corgi cache paths --gitlab --check .gitlab/corgi-cache.yml`,
	Run: runCachePaths,
}

func init() {
	rootCmd.AddCommand(cacheCmd)
	cacheCmd.AddCommand(cachePathsCmd)
	cachePathsCmd.Flags().Bool("key", false, "Print only the cache key")
	cachePathsCmd.Flags().Bool("strict", false, "Exit 1 when a cacheKey file does not exist, instead of only warning")
	cachePathsCmd.Flags().Bool("gitlab", false, "Render a GitLab CI cache job template instead of a path list")
	cachePathsCmd.Flags().String("out", "", "With --gitlab: write to this file instead of stdout, creating its directory")
	cachePathsCmd.Flags().String("check", "", "With --gitlab: exit non-zero when this file differs from what would be generated")
	cachePathsCmd.Flags().String("path-prefix", "", "With --gitlab: prefix every path, for a pipeline that clones the workspace into a subdirectory")
}

func runCachePaths(cmd *cobra.Command, _ []string) {
	// Set before the config is read, so the "using compose file" line does not
	// land in a command substitution.
	utils.PayloadOnStdout = true

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		exitWithError(utils.ErrConfig, err, 1)
	}

	plan := utils.CachePathsFor(corgi)
	strict, _ := cmd.Flags().GetBool("strict")

	if gitlab, _ := cmd.Flags().GetBool("gitlab"); gitlab {
		runGitLabCachePaths(cmd, plan)
		return
	}

	if keyOnly, _ := cmd.Flags().GetBool("key"); keyOnly {
		fmt.Println(plan.Key)
		warnMissingCacheKeyFiles(plan)
		failIfIncomplete(plan, strict)
		return
	}
	if utils.JSONOutput {
		utils.PrintJSON(plan)
		// The JSON already carries complete/missingFiles, so a warning here
		// would only double up the annotation the action prints from them.
		if strict && !plan.Complete {
			warnMissingCacheKeyFiles(plan)
		}
		failIfIncomplete(plan, strict)
		return
	}
	// Newline-separated, which is the format GitHub's cache action expects for
	// a multi-line path input.
	fmt.Println(strings.Join(plan.Paths, "\n"))
	warnCachingIsOff(plan)
	warnMissingCacheKeyFiles(plan)
	failIfIncomplete(plan, strict)
}

// warnMissingCacheKeyFiles is the loud half of the fix for a key hashed from
// nothing: a CI job that computes the plan before cloning the service repos
// gets a key that never changes, and a cache that never invalidates. stderr,
// so a command substitution stays clean; a GitHub annotation, so it is seen.
func warnMissingCacheKeyFiles(plan utils.CachePlan) {
	if plan.Complete {
		return
	}
	files := strings.Join(plan.MissingFiles, ", ")
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		utils.Infof("::warning::corgi cache paths: cacheKey files not found, so the key was hashed from nothing "+
			"and will not change when they do (missing: %s). Run this after `corgi init` (or after the service directories exist).\n",
			files)
		return
	}
	utils.Infof("\nwarning: these cacheKey files do not exist, so the key was hashed from nothing and will not change when they do:\n")
	for _, f := range plan.MissingFiles {
		utils.Infof("  %s\n", f)
	}
	utils.Infof("Run this after `corgi init` (or after the service directories exist).\n")
}

func failIfIncomplete(plan utils.CachePlan, strict bool) {
	if strict && !plan.Complete {
		exitProcess(1)
	}
}

// warnCachingIsOff distinguishes "nothing opts in" from "nothing to cache".
// stderr, so a command substitution stays clean.
func warnCachingIsOff(plan utils.CachePlan) {
	if len(plan.Groups) > 0 || len(plan.Hints) == 0 {
		return
	}
	utils.Infof("\nNo service declares a beforeStart cacheKey, so every install runs from scratch.\n")
	utils.Infof("These steps could skip an unchanged install:\n")
	for _, line := range utils.CacheHintLines(plan.Hints) {
		utils.Infof("  %s\n", line)
	}
}

func runGitLabCachePaths(cmd *cobra.Command, plan utils.CachePlan) {
	prefix, _ := cmd.Flags().GetString("path-prefix")
	opts := utils.GitLabCacheOptions{PathPrefix: prefix}

	rendered := utils.GitLabCacheYAML(plan, opts)

	if checkPath, _ := cmd.Flags().GetString("check"); checkPath != "" {
		if err := checkGitLabCacheFile(checkPath, rendered); err != nil {
			exitWithError(utils.ErrConfig, err, 1)
		}
		utils.Infof("%s matches the compose file\n", checkPath)
		return
	}

	if outPath, _ := cmd.Flags().GetString("out"); outPath != "" {
		if err := writeGeneratedFile(outPath, rendered); err != nil {
			exitWithError(utils.ErrConfig, err, 1)
		}
		utils.Infof("wrote %s\n", outPath)
		return
	}

	fmt.Print(rendered)
}

// checkGitLabCacheFile is the drift guard: a committed generated file is only
// trustworthy while something fails when it stops matching its source.
func checkGitLabCacheFile(path, rendered string) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf(
				"%s does not exist. Generate it with: corgi cache paths --gitlab --out %s", path, path)
		}
		return err
	}
	if string(existing) == rendered {
		return nil
	}
	return fmt.Errorf(
		"%s no longer matches corgi-compose.yml — the cache would restore the wrong paths.\n"+
			"Regenerate and commit it: corgi cache paths --gitlab --out %s", path, path)
}

// writeGeneratedFile writes a generated file, creating its directory.
func writeGeneratedFile(path, rendered string) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(rendered), 0o644)
}
