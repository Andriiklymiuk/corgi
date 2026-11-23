package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var agentUpgradedCmd = &cobra.Command{
	Use:   "upgraded",
	Short: "After something other than corgi upd replaced the corgi binary: refresh the daemon's own copy and restart a daemon that still runs the old version",
	Long: `corgi upd does this by itself. Run it when the binary was replaced some other
way - the Mac app puts a newer corgi in place of the one it installed, a
package manager, a copy by hand:

  corgi agent upgraded

It rewrites the copy of corgi the login service starts (the one under
Application Support), then restarts the daemon when the running one is older
than this binary. With no login service installed it does nothing: the next
corgi agent up starts the new version anyway.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		exePath, err := os.Executable()
		if err != nil {
			exitWithError("agent_upgraded", err, 1)
		}
		refreshDaemonAfterUpgrade(exePath, APP_VERSION)
		fmt.Printf("corgi %s\n", APP_VERSION)
	},
}

func init() {
	agentCmd.AddCommand(agentUpgradedCmd)
}
