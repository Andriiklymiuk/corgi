package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Statically validate corgi-compose.yml",
	Long: `Runs static semantic checks over corgi-compose.yml without starting
containers, cloning repos, or touching the network.

Errors (exit 1):
  - depends_on_services / depends_on_db references a name that doesn't exist
  - a cycle exists in the depends_on_services graph
  - db_services.driver is not a known driver
  - a service exposes a port but has no start command and no docker runner
  - two services / db_services bind the same host port

Warnings (non-fatal, fatal under --strict):
  - a depended-on service has no healthCheck (TCP probe used)
  - cloneFrom is set without a branch

Flags:
      --json     Emit {"ok":bool,"errors":[...],"warnings":[...]}
      --strict   Treat warnings as failures`,
	Run:     runValidate,
	Aliases: []string{"lint"},
}

func init() {
	validateCmd.Flags().Bool("strict", false, "Treat warnings as failures")
	rootCmd.AddCommand(validateCmd)
}

type validateReport struct {
	Ok       bool                    `json:"ok"`
	Errors   []utils.ValidationIssue `json:"errors"`
	Warnings []utils.ValidationIssue `json:"warnings"`
}

func runValidate(cmd *cobra.Command, _ []string) {
	jsonOut := utils.JSONOutput
	strict, _ := cmd.Flags().GetBool("strict")

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if jsonOut {
			utils.JSONError(utils.ErrComposeNotFound, err.Error())
		} else {
			fmt.Fprintf(os.Stderr, "couldn't load corgi-compose.yml: %s\n", err)
		}
		os.Exit(2)
	}

	errs, warns := utils.ValidateCompose(corgi)
	if errs == nil {
		errs = []utils.ValidationIssue{}
	}
	if warns == nil {
		warns = []utils.ValidationIssue{}
	}

	failed := len(errs) > 0 || (strict && len(warns) > 0)

	if jsonOut {
		utils.PrintJSON(validateReport{Ok: !failed, Errors: errs, Warnings: warns})
		if failed {
			os.Exit(1)
		}
		return
	}

	printValidateHuman(errs, warns, strict)
	if failed {
		os.Exit(1)
	}
}

func printValidateHuman(errs, warns []utils.ValidationIssue, strict bool) {
	utils.Info("🔎 corgi validate")

	if len(errs) == 0 {
		utils.Infof("  %s✓ no errors%s\n", art.GreenColor, art.WhiteColor)
	} else {
		for _, e := range errs {
			field := ""
			if e.Field != "" {
				field = fmt.Sprintf(" (%s)", e.Field)
			}
			utils.Infof("  %s✗ [%s] %s%s%s\n", art.RedColor, e.Code, e.Message, field, art.WhiteColor)
		}
	}

	for _, w := range warns {
		field := ""
		if w.Field != "" {
			field = fmt.Sprintf(" (%s)", w.Field)
		}
		utils.Infof("  %s⚠ [%s] %s%s%s\n", art.YellowColor, w.Code, w.Message, field, art.WhiteColor)
	}

	switch {
	case len(errs) > 0:
		utils.Infof("%s%d error(s), %d warning(s)%s\n", art.RedColor, len(errs), len(warns), art.WhiteColor)
	case strict && len(warns) > 0:
		utils.Infof("%s0 errors but %d warning(s) — failing under --strict%s\n", art.YellowColor, len(warns), art.WhiteColor)
	case len(warns) > 0:
		utils.Infof("%s🎉 valid — %d warning(s)%s\n", art.GreenColor, len(warns), art.WhiteColor)
	default:
		utils.Infof("%s🎉 valid — no issues%s\n", art.GreenColor, art.WhiteColor)
	}
}
