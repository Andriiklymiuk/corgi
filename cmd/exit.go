package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
)

// osExit is overridable so the exit paths can be asserted in tests. Nothing
// else should replace it.
var osExit = os.Exit

// exitWithError reports err as JSON (with jsonCode) or on stderr, then exits.
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

// exitProcess is the one exit sequence: close the session log, then exit.
// Commands ending without an error message use it directly.
func exitProcess(code int) {
	utils.CloseSessionLog()
	osExit(code)
}

// mustLoadCorgiServices loads the compose file or exits 1 reporting why.
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
