package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

// runE2ESuite runs the stack's e2e: block against whatever is already running.
// It deliberately does not start anything: an e2e suite asserts on a live
// stack, and booting one here would hide which half failed.
func runE2ESuite(cmd *cobra.Command) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		failE2E(err.Error())
	}

	suite := corgi.E2E
	if suite == nil || suite.Run == "" {
		failE2E("no e2e: block in corgi-compose.yml — declare one with workdir/install/run, or drop --e2e to run each service's test script")
	}

	workdir := filepath.Join(utils.CorgiComposePathDir, suite.Workdir)
	if info, statErr := os.Stat(workdir); statErr != nil || !info.IsDir() {
		failE2E(fmt.Sprintf("e2e workdir %q does not exist", workdir))
	}

	if suite.Install != "" {
		if err := utils.RunServiceCmd("e2e", suite.Install, workdir, false, utils.SkipAutoSourceEnv); err != nil {
			failE2E(fmt.Sprintf("e2e install: %v", err))
		}
	}

	if err := utils.RunServiceCmd("e2e", suite.Run, workdir, false, utils.SkipAutoSourceEnv); err != nil {
		failE2E(fmt.Sprintf("e2e: %v", err))
	}

	utils.Info("✅ e2e passed")
}

func failE2E(msg string) {
	if utils.JSONOutput {
		utils.JSONError(utils.ErrConfig, msg)
	} else {
		fmt.Fprintln(os.Stderr, "❌", msg)
	}
	os.Exit(1)
}
