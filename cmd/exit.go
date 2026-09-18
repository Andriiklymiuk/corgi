package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
)

var osExit = os.Exit

func exitWithError(jsonCode string, err error, exitCode int) {
	exitWithErrorPrefix(jsonCode, "", err, exitCode)
}

func exitWithErrorPrefix(jsonCode, stderrPrefix string, err error, exitCode int) {
	if utils.JSONOutput {
		utils.JSONError(jsonCode, err.Error())
	} else if stderrPrefix != "" {
		fmt.Fprintln(utils.WithMirror(os.Stderr), stderrPrefix, err)
	} else {
		fmt.Fprintln(utils.WithMirror(os.Stderr), err)
	}
	exitProcess(exitCode)
}

func exitProcess(code int) {
	utils.CloseSessionLog()
	osExit(code)
}

func mustLoadCorgiServices(cmd *cobra.Command) *utils.CorgiCompose {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError(utils.ErrConfig, err.Error())
		} else {
			utils.Infof("couldn't get services config: %s\n", err)
		}
		exitProcess(1)
	}
	return corgi
}
