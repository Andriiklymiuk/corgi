package cmd

import (
	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
)

var agentWakeCmd = &cobra.Command{
	Use:   "wake <peer>",
	Short: "Wake a sleeping peer Mac on this network (Wake-on-LAN)",
	Long: `Sends a Wake-on-LAN packet to a peer on the same network. The peer says
how to wake it in its pulses (corgi 2.51+), so it must have pulsed once while
awake, and macOS needs "Wake for network access" on (System Settings ›
Battery › Options, or Energy on a desktop Mac).

  corgi agent wake studio`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := wakePeer(mustAgentDir(), args[0]); err != nil {
			exitWithError("agent_wake", err, 1)
		}
		utils.Infof("✓ sent a wake-up to %s\n", args[0])
	},
}

func init() {
	agentCmd.AddCommand(agentWakeCmd)
}
