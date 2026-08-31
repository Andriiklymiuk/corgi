package cmd

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var leasesCmd = &cobra.Command{
	Use:     "leases",
	Aliases: []string{"lease"},
	Short:   "Named isolation leases: who holds which port block",
	Long: `--isolate <name> gives a run its own port block, its own database names and its
own container names, so two agents can drive the same workspace at once.

This lists the leases that exist and what each one shifted. Every command that
reads the compose file takes --isolate, so ps, status, logs and stop see the same
shifted view as the run that created it.`,
	Example: `corgi run --isolate agent-a --detach
corgi ps --isolate agent-a
corgi leases
corgi leases release agent-a`,
	Run: runLeases,
}

var leasesReleaseCmd = &cobra.Command{
	Use:     "release <name>",
	Aliases: []string{"rm", "drop"},
	Short:   "Forget a lease so its port block is free again",
	Args:    cobra.ExactArgs(1),
	Run:     runLeaseRelease,
}

func init() {
	rootCmd.AddCommand(leasesCmd)
	leasesCmd.AddCommand(leasesReleaseCmd)
}

func runLeases(cmd *cobra.Command, _ []string) {
	mustLoadCorgiServices(cmd)
	leases := utils.ListLeases()

	if utils.JSONOutput {
		if leases == nil {
			leases = []utils.Lease{}
		}
		utils.PrintJSON(leases)
		return
	}
	if len(leases) == 0 {
		utils.Info("no leases — corgi run --isolate <name> creates one")
		return
	}
	w := tabwriter.NewWriter(utils.ConsoleOut(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "LEASE\tPORT SHIFT\tCONTAINERS\tPORTS")
	for _, lease := range leases {
		fmt.Fprintf(w, "%s\t+%d\t%s\t%s\n", lease.Name, lease.Offset, lease.Containers, leasePortSummary(lease))
	}
	w.Flush()
}

func leasePortSummary(lease utils.Lease) string {
	names := make([]string, 0, len(lease.Ports))
	for name := range lease.Ports {
		names = append(names, name)
	}
	sort.Strings(names)
	summary := ""
	for _, name := range names {
		if summary != "" {
			summary += " "
		}
		summary += fmt.Sprintf("%s:%d", name, lease.Ports[name])
	}
	return summary
}

func runLeaseRelease(cmd *cobra.Command, args []string) {
	mustLoadCorgiServices(cmd)
	if err := utils.ReleaseLease(args[0]); err != nil {
		exitWithError(utils.ErrNotRunning, err, 1)
		return
	}
	utils.Infof("released lease %q — stop anything still running under it first\n", args[0])
}
