package cmd

import (
	"fmt"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var envCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Fail when a service's env file misses keys its .env-example declares",
	Long: `Diff each service's env source against the example file its repo commits
(.env-example or .env.example), subtracting every key corgi generates itself
(db and service dependencies, ports, environment literals). What remains is a
key the service expects and nothing provides — the kind that fails at the
first request, thousands of log lines from the cause.

Exits non-zero on any missing key, on a declared env source that does not
exist, and when nothing could be checked at all — a vacuous pass would read
as coverage.

Empty values are not findings: an empty key is often a deliberate off switch.

Examples:
  corgi env check                  # each service's resolved env source
  corgi env check --file .env.ci   # a committed CI env file in each repo
  corgi env check --json`,
	RunE: runEnvCheck,
}

func init() {
	envCheckCmd.Flags().String("file", "", "Check this file inside each service repo instead of the resolved env source")
	envCheckCmd.Flags().StringVar(&utils.EnvTierFromFlag, "tier", "", "Resolve env for this compose envTier (e.g. staging, prod)")
	envCmd.AddCommand(envCheckCmd)
}

func runEnvCheck(cmd *cobra.Command, _ []string) error {
	utils.PayloadOnStdout = true

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		return fmt.Errorf("%s: %v", utils.ErrComposeNotFound, err)
	}

	fileOverride, _ := cmd.Flags().GetString("file")
	rows, err := utils.EnvCheckAll(corgi, fileOverride)
	if err != nil {
		return err
	}

	summary, findings := utils.EnvCheckSummary(rows)
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"ok": !findings, "services": rows})
	} else {
		// The verdict is the payload; Info would land on stderr here.
		fmt.Print(summary)
	}
	if findings {
		utils.CloseSessionLog()
		osExit(1)
	}
	return nil
}
