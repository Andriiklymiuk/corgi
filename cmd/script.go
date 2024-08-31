/*
Copyright © 2023 Andrii Klymiuk
*/
package cmd

import (
	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"
	"fmt"

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

	for _, service := range corgi.Services {
		if service.Scripts == nil {
			continue
		}
		utils.CreateFileForPath(service.CopyEnvFromFilePath)
		fmt.Println(art.BlueColor, "🐶 SCRIPT FOR", service.ServiceName, art.WhiteColor)
		isAnyScriptsFound = true
		for _, scriptServiceCmd := range service.Scripts {
			if scriptServiceCmd.ManualRun {
				if len(ScriptNamesFromFlag) == 0 {
					fmt.Println(scriptServiceCmd.Name, "is not run, because it should be run manually (manualRun)")
					continue
				}
			}
			if !utils.IsServiceIncludedInFlag(ScriptNamesFromFlag, scriptServiceCmd.Name) {
				continue
			}
			if scriptServiceCmd.CopyEnvFromFilePath != "" {
				utils.GenerateEnvForService(
					corgi,
					service,
					scriptServiceCmd.CopyEnvFromFilePath,
				)
			}
			runServiceScript(scriptServiceCmd, service.AbsolutePath)
		}

		// return to previous state of .env file
		utils.GenerateEnvForService(
			corgi,
			service,
			"",
		)
	}
	if !isAnyScriptsFound {
		fmt.Println(art.RedColor, "No scripts found", art.WhiteColor)
	}
}

func runServiceScript(script utils.Script, path string) {
	fmt.Println(art.BlueColor, "\n🤖 Executing commands for script", script.Name, art.WhiteColor)
	for _, scriptCommand := range script.Commands {
		err := utils.RunServiceCmd(script.Name, scriptCommand, path, false)
		if err != nil {
			fmt.Println(
				art.RedColor,
				"Aborting all other script commands for ", script.Name, ", because of ", err,
				art.WhiteColor,
			)
			return
		}
	}

}
