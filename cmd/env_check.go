package cmd

import (
	"fmt"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var envCheckCmd = &cobra.Command{
	Use:   "check [service...]",
	Short: "Fail when a service's env file misses keys its .env-example declares",
	Long: `Diff each service's env source against the example file its repo commits
(.env-example or .env.example), subtracting every key corgi generates itself
(db and service dependencies, ports, environment literals). What remains is a
key the service expects and nothing provides — the kind that fails at the
first request, thousands of log lines from the cause.

Exits non-zero on any missing key, on a declared env source that does not
exist while the example still needs keys, and when nothing could be checked
at all — a vacuous pass would read as coverage.

Empty values are not findings: an empty key is often a deliberate off switch.

Examples:
  corgi env check                  # each service's resolved env source
  corgi env check api              # just one service
  corgi env check --file .env.ci   # a committed CI env file in each repo
  corgi env check --json`,
	RunE: runEnvCheck,
}

func init() {
	envCheckCmd.Flags().String("file", "", "Check this file inside each service repo instead of the resolved env source")
	envCmd.AddCommand(envCheckCmd)
}

func runEnvCheck(cmd *cobra.Command, args []string) error {
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
	if rows, err = filterEnvCheckRows(rows, args); err != nil {
		return err
	}

	checked, findings := utils.EnvCheckStats(rows)
	if utils.JSONOutput {
		doc := map[string]any{"ok": !findings, "services": rows}
		if checked == 0 {
			doc["reason"] = utils.EnvCheckNothingChecked
		}
		utils.PrintJSON(doc)
	} else {
		summary, _ := utils.EnvCheckSummary(rows)
		// The verdict is the payload; Info would land on stderr here.
		fmt.Print(summary)
	}
	if findings {
		exitProcess(1)
	}
	return nil
}

// filterEnvCheckRows narrows to the named services, erroring on unknown names
// so a typo cannot read as a pass.
func filterEnvCheckRows(rows []utils.EnvCheckRow, names []string) ([]utils.EnvCheckRow, error) {
	if len(names) == 0 {
		return rows, nil
	}
	byName := map[string]utils.EnvCheckRow{}
	for _, row := range rows {
		byName[row.Service] = row
	}
	out := make([]utils.EnvCheckRow, 0, len(names))
	for _, name := range names {
		row, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%s: service %q not found", utils.ErrServiceNotFound, name)
		}
		out = append(out, row)
	}
	return out, nil
}
