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
	restartCmd.Flags().Bool("detach", true, "Start services detached (always on for restart)")
	restartCmd.Flags().Bool("force", true, "Ignore stale run-state and start anyway")
	restartCmd.Flags().String("host", "", "IP to use instead of localhost in service URL env vars")
}

func restartUnsupportedMessage(service string) string {
	return "restart --service is not supported yet; use: corgi stop --service " +
		service + " && corgi run --detach"
}

func runRestart(cmd *cobra.Command, args []string) {
	if restartService != "" {
		msg := restartUnsupportedMessage(restartService)
		if utils.JSONOutput {
			utils.JSONError(utils.ErrUnsupported, msg)
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(2)
	}

	prevStopService := stopService
	prevToStderr := stopSummaryToStderr
	stopService = restartService
	stopSummaryToStderr = true
	runStop(cmd, args)
	stopService = prevStopService
	stopSummaryToStderr = prevToStderr

	cmd.Flags().Set("detach", "true")
	runRun(cmd, args)
}
