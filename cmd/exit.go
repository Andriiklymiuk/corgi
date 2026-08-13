package cmd

import (
	"fmt"
	"os"

	"andriiklymiuk/corgi/utils"
)

// exitWithError reports err as JSON (with jsonCode) or on stderr, then exits.
func exitWithError(jsonCode string, err error, exitCode int) {
	exitWithErrorPrefix(jsonCode, "", err, exitCode)
}

func exitWithErrorPrefix(jsonCode, stderrPrefix string, err error, exitCode int) {
	if utils.JSONOutput {
		utils.JSONError(jsonCode, err.Error())
	} else if stderrPrefix != "" {
		fmt.Fprintln(os.Stderr, stderrPrefix, err)
	} else {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(exitCode)
}
