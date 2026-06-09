/*
Copyright © 2023 Andrii Klymiuk
*/
package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// scriptCmd represents the script command
var scriptCmd = &cobra.Command{
	Use:     "script",
	Short:   "Runs script on each service, if it specified",
	Run:     runScript,
	Aliases: []string{"asdf", "asd", "scripts", "commands"},
}

var ScriptNamesFromFlag []string
var IgnoreDependentServices bool

func init() {
	rootCmd.AddCommand(scriptCmd)

	scriptCmd.PersistentFlags().StringSliceVarP(
		&utils.ServicesItemsFromFlag,
		"services",
		"",
		[]string{},
		`Slice of services to choose from.

If you provide at least 1 services here, than corgi will choose only this service, while ignoring all others.
none - will ignore all services run script.
(--services app,server)

By default all services are included and script are run on them.
		`,
	)

	scriptCmd.PersistentFlags().StringSliceVarP(
		&ScriptNamesFromFlag,
		"names",
		"n",
		[]string{},
		`Slice of script names to choose from.

If you provide at least 1 name here, than corgi will choose only to run these scripts, while ignoring all others.
(--names deploy_staging,test_e2e,smth_smth_script)

By default all scripts are included to run.
		`,
	)

	scriptCmd.PersistentFlags().BoolVarP(
		&IgnoreDependentServices,
		"ignore-dependent-services",
		"",
		true,
		"Ignore dependent services for scripts, while copying env from other services.",
	)

	scriptCmd.PersistentFlags().Bool(
		"continue-on-error",
		false,
		"Run the script across all matching services, print a pass/fail summary, and exit non-zero if any failed.",
	)
}

func shouldRunScript(s utils.Script) bool {
	if s.ManualRun && len(ScriptNamesFromFlag) == 0 {
		fmt.Println(s.Name, "is not run, because it should be run manually (manualRun)")
		return false
	}
	return utils.IsServiceIncludedInFlag(ScriptNamesFromFlag, s.Name)
}

func runScriptsForService(corgi *utils.CorgiCompose, service utils.Service) []scriptResult {
	utils.CreateFileForPath(service.CopyEnvFromFilePath)
	fmt.Println(art.BlueColor, "🐶 SCRIPT FOR", service.ServiceName, art.WhiteColor)
	var results []scriptResult
	for _, scriptServiceCmd := range service.Scripts {
		if !shouldRunScript(scriptServiceCmd) {
			continue
		}
		if scriptServiceCmd.CopyEnvFromFilePath != "" {
			_ = utils.GenerateEnvForService(
				corgi,
				service,
				scriptServiceCmd.CopyEnvFromFilePath,
				IgnoreDependentServices,
			)
		}
		err := runServiceScript(scriptServiceCmd, service.AbsolutePath)
		results = append(results, scriptResult{Service: service.ServiceName, Name: scriptServiceCmd.Name, OK: err == nil})
	}

	// return to previous state of .env file
	_ = utils.GenerateEnvForService(corgi, service, "", false)
	return results
}

func runScript(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println(err)
		return
	}
	if len(corgi.Services) == 0 {
		fmt.Println(art.RedColor, "No services found", art.WhiteColor)
		return
	}

	var isAnyScriptsFound bool
	var results []scriptResult
	for _, service := range corgi.Services {
		if service.Scripts == nil {
			continue
		}
		isAnyScriptsFound = true
		results = append(results, runScriptsForService(corgi, service)...)
	}
	if !isAnyScriptsFound {
		fmt.Println(art.RedColor, "No scripts found", art.WhiteColor)
		return
	}

	// --continue-on-error: print a pass/fail summary and exit non-zero on any failure.
	if continueOnError, _ := cmd.Flags().GetBool("continue-on-error"); continueOnError {
		lines, failed := summarizeScriptResults(results)
		utils.Info("\nScript summary:")
		for _, l := range lines {
			utils.Info(l)
		}
		if failed > 0 {
			utils.Infof("✗ %d of %d failed\n", failed, len(results))
			os.Exit(1)
		}
		utils.Infof("✓ all %d passed\n", len(results))
	}
}

type scriptResult struct {
	Service string
	Name    string
	OK      bool
}

// summarizeScriptResults renders a per-result pass/fail summary + failure count.
func summarizeScriptResults(results []scriptResult) (lines []string, failed int) {
	for _, r := range results {
		mark := "✓"
		if !r.OK {
			mark = "✗"
			failed++
		}
		lines = append(lines, fmt.Sprintf("  %s %s %s", mark, r.Service, r.Name))
	}
	return lines, failed
}

func runServiceScript(script utils.Script, path string) error {
	fmt.Println(art.BlueColor, "\n🤖 Executing commands for script", script.Name, art.WhiteColor)
	for _, scriptCommand := range script.Commands {
		err := utils.RunServiceCmd(
			script.Name,
			scriptCommand,
			path,
			true,
		)
		if err != nil {
			fmt.Println(
				art.RedColor,
				"Aborting all other script commands for ", script.Name, ", because of ", err,
				art.WhiteColor,
			)
			return err
		}
	}
	return nil
}
