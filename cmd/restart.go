package cmd

import (
	"fmt"
	"os"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var restartService string

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Stop then start detached services (corgi stop + corgi run --detach)",
	Long: `Stops the currently detached stack (or a single --service) and brings it
back up detached. Convenience for long-lived envs.

Non-interactive safe. With --json the stdout is a single startup-summary
JSON object (the same shape as corgi run --detach --json).`,
	Run: runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
	restartCmd.Flags().StringVar(&restartService, "service", "", "Restart only this service (leave others running)")
	// runRun reads these off cmd.Flags(); register them so the run path
	// works when invoked through restart. --host in particular fatally
	// short-circuits runRun if absent.
	restartCmd.Flags().Bool("detach", true, "Start services detached (always on for restart)")
	restartCmd.Flags().Bool("force", true, "Ignore stale run-state and start anyway")
	restartCmd.Flags().String("host", "", "IP to use instead of localhost in service URL env vars")
}

func runRestart(cmd *cobra.Command, args []string) {
	// Per-service restart would overwrite the whole run-state and orphan the
	// other running services, so it isn't supported yet.
	if restartService != "" {
		msg := "restart --service is not supported yet; use: corgi stop --service " +
			restartService + " && corgi run --detach"
		if utils.JSONOutput {
			utils.JSONError("UNSUPPORTED", msg)
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(2)
	}

	// Scope the teardown to the same service (empty = full stack). Route the
	// stop summary to stderr so --json stdout carries only the run-state.
	prevStopService := stopService
	prevToStderr := stopSummaryToStderr
	stopService = restartService
	stopSummaryToStderr = true
	runStop(cmd, args)
	stopService = prevStopService
	stopSummaryToStderr = prevToStderr

	// runRun does the full startup (preflight, db start, env gen) before
	// detaching, which runDetached alone would skip — so reuse runRun.
	cmd.Flags().Set("detach", "true")
	runRun(cmd, args)
}
