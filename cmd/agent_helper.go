package cmd

import (
	"fmt"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/helpers"

	"github.com/spf13/cobra"
)

var agentHelperCmd = &cobra.Command{
	Use:   "helper [name] [arguments]",
	Short: "Run one of the small programs the skills use, kept inside corgi",
	Long: `The skills need a few small programs. They ship inside corgi, so the
plugin itself holds only text. Everything after the name goes to the program.

  corgi agent helper                               the list
  corgi agent helper mine-sessions --last 30
  corgi agent helper soundtrack mix sound.json mix.wav`,
	DisableFlagParsing: true,
	Run: func(_ *cobra.Command, args []string) {
		if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
			for _, name := range helpers.Names() {
				fmt.Printf("%-14s %s\n", name, helpers.Describe(name))
			}
			return
		}
		code, err := helpers.Run(args[0], args[1:])
		if err != nil {
			exitWithError("agent_helper", err, code)
		}
		if code != 0 {
			utils.Infof("%s ended with code %d\n", args[0], code)
			osExit(code)
		}
	},
}

func init() {
	agentCmd.AddCommand(agentHelperCmd)
}
