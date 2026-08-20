package cmd

import (
	"fmt"
	"os"

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
