package cmd

import (
	"fmt"
	"os"
	"sync"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"

	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build docker images for docker-capable services, in parallel, without starting anything",
	Long: `Pre-builds the image of every docker-capable service (a Dockerfile or the
repo's own compose file) so the next corgi run boots from a warm cache.
Respects --services to build a subset. Services without a docker source are
skipped. Exit 1 if any build fails.`,
	Run: runBuild,
}

func init() {
	rootCmd.AddCommand(buildCmd)
	buildCmd.Flags().StringSliceVar(
		&utils.ServicesItemsFromFlag,
		"services",
		[]string{},
		"Build only these services (--services api,web)",
	)
	buildCmd.Flags().IntVar(
		&buildParallelism,
		"parallel",
		4,
		"Max concurrent image builds",
	)
}

var buildParallelism int

type buildResult struct {
	Name  string `json:"name"`
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func runBuild(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		exitWithErrorPrefix(utils.ErrConfig, "couldn't get services config:", err, 1)
	}

	cloned, notCloned := filterClonedServices(corgi.Services)
	if len(notCloned) > 0 {
		utils.Info("skipping not-yet-cloned services (run corgi init first):", notCloned)
	}

	// --docker semantics don't matter here: build targets every service that
	// has a docker source, scripted or not.
	resolved, rerr := utils.ResolveRunnerModes(cloned, true, false)
	if rerr != nil {
		exitWithErrorPrefix(utils.ErrConfig, "❌", rerr, 1)
	}

	buildable := selectBuildableServices(resolved)
	if len(buildable) == 0 {
		utils.Info("no docker-capable services to build")
		if utils.JSONOutput {
			utils.PrintJSON([]buildResult{})
		}
		return
	}

	// Seam files (docker-compose.yml/Makefile) must exist before make build.
	CreateServices(buildable)

	utils.Info(art.BlueColor, fmt.Sprintf("🔨 building %d image(s) in parallel", len(buildable)), art.WhiteColor)

	results := buildImagesInParallel(buildable)
	failed := reportBuildResults(results)
	if utils.JSONOutput {
		utils.PrintJSON(results)
	}
	if failed > 0 {
		os.Exit(1)
	}
}

// Resolution reads the repo dir — a not-yet-cloned service would produce a
// misleading "no dockerfile found" error, so filter those out up front.
func filterClonedServices(services []utils.Service) (cloned []utils.Service, notCloned []string) {
	for _, s := range services {
		if s.CloneFrom != "" {
			if _, serr := os.Stat(s.AbsolutePath); serr != nil {
				notCloned = append(notCloned, s.ServiceName)
				continue
			}
		}
		cloned = append(cloned, s)
	}
	return cloned, notCloned
}

func selectBuildableServices(resolved []utils.Service) []utils.Service {
	var buildable []utils.Service
	for _, s := range resolved {
		if s.Runner.IsDocker() && s.ResolvedDockerSource != utils.SourceNone && s.Runner.Image == "" {
			buildable = append(buildable, s)
		}
	}
	return buildable
}

func buildImagesInParallel(buildable []utils.Service) []buildResult {
	if buildParallelism < 1 {
		buildParallelism = 1
	}
	results := make([]buildResult, len(buildable))
	sem := make(chan struct{}, buildParallelism)
	var wg sync.WaitGroup
	for i, svc := range buildable {
		wg.Add(1)
		go func(i int, svc utils.Service) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := utils.ExecuteServiceCommandRun(svc.ServiceName, "make", "build"); err != nil {
				results[i] = buildResult{Name: svc.ServiceName, Error: err.Error()}
				return
			}
			results[i] = buildResult{Name: svc.ServiceName, Ok: true}
		}(i, svc)
	}
	wg.Wait()
	return results
}

func reportBuildResults(results []buildResult) int {
	failed := 0
	for _, r := range results {
		if r.Ok {
			utils.Info("✅ built", r.Name)
		} else {
			failed++
			utils.Info("❌", r.Name+":", r.Error)
		}
	}
	return failed
}
