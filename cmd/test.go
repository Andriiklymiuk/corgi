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

// testCmd represents the test command
var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Runs test on each service, if it specified",
	Run:   runTest,
}

var EnvItemsFromFlag []string

func init() {
	rootCmd.AddCommand(testCmd)

	testCmd.PersistentFlags().StringSliceVarP(
		&utils.ServicesItemsFromFlag,
		"services",
		"",
		[]string{},
		`Slice of services to choose from.

If you provide at least 1 services here, than corgi will choose only this service, while ignoring all others.
none - will ignore all services run test.
(--services app,server)

By default all services are included and test are run on them.
		`,
	)

	testCmd.PersistentFlags().StringSliceVarP(
		&EnvItemsFromFlag,
		"env",
		"",
		[]string{},
		`Slice of test names to choose from.

If you provide at least 1 env here, than corgi will choose only to run these test names, while ignoring all others.
(--env local,dev,prod)

By default all tests are included to run.
		`,
	)
}

func runTest(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, service := range corgi.Services {
		fmt.Println(art.BlueColor, "🐶 TESTING SERVICE", service.ServiceName, art.WhiteColor)
		for _, testServiceCmd := range service.Test {
			if !utils.IsServiceIncludedInFlag(EnvItemsFromFlag, testServiceCmd.Name) {
				continue
			}
			testService(testServiceCmd, service.Path)
		}
	}
}

func testService(test utils.TestService, path string) {
	fmt.Println(art.BlueColor, "\n🤖 Executing commands for test", test.Name, art.WhiteColor)
	for _, testCommand := range test.Command {
		err := utils.RunServiceCmd(test.Name, testCommand, path)
		if err != nil {
			fmt.Println(
				art.RedColor,
				"Aborting all other test commands for ", test.Name, ", because of ", err,
				art.WhiteColor,
			)
			return
		}
	}

}
