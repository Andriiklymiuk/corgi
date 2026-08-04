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
}

type buildResult struct {
	Name  string `json:"name"`
	Ok    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func runBuild(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError(utils.ErrConfig, err.Error())
		} else {
			fmt.Fprintln(os.Stderr, "couldn't get services config:", err)
		}
		os.Exit(1)
	}

	// --docker semantics don't matter here: build targets every service that
	// has a docker source, scripted or not.
	resolved, rerr := utils.ResolveRunnerModes(corgi.Services, true, false)
	if rerr != nil {
		if utils.JSONOutput {
			utils.JSONError(utils.ErrConfig, rerr.Error())
		} else {
			fmt.Fprintln(os.Stderr, "❌", rerr)
		}
		os.Exit(1)
	}

	var buildable []utils.Service
	for _, s := range resolved {
		if s.Runner.IsDocker() && s.ResolvedDockerSource != utils.SourceNone && s.Runner.Image == "" {
			buildable = append(buildable, s)
		}
	}
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

	results := make([]buildResult, len(buildable))
	var wg sync.WaitGroup
	for i, svc := range buildable {
		wg.Add(1)
		go func(i int, svc utils.Service) {
			defer wg.Done()
			if err := utils.ExecuteServiceCommandRun(svc.ServiceName, "make", "build"); err != nil {
				results[i] = buildResult{Name: svc.ServiceName, Error: err.Error()}
				return
			}
			results[i] = buildResult{Name: svc.ServiceName, Ok: true}
		}(i, svc)
	}
	wg.Wait()

	failed := 0
	for _, r := range results {
		if r.Ok {
			utils.Info("✅ built", r.Name)
		} else {
			failed++
			utils.Info("❌", r.Name+":", r.Error)
		}
	}
	if utils.JSONOutput {
		utils.PrintJSON(results)
	}
	if failed > 0 {
		os.Exit(1)
	}
}
