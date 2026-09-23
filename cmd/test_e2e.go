package cmd

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

const defaultE2EArtifactsDir = "corgi_artifacts/e2e"

func runE2ESuite(cmd *cobra.Command) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		failE2E(err.Error())
	}

	suite := corgi.E2E
	if suite == nil || suite.Run == "" {
		failE2E("no e2e: block in corgi-compose.yml - declare one with workdir/install/run, or drop --e2e to run each service's test script")
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

	runErr := utils.RunServiceCmd("e2e", suite.Run, workdir, false, utils.SkipAutoSourceEnv)

	collectE2EArtifacts(cmd, suite, workdir)

	if runErr != nil {
		failE2E(fmt.Sprintf("e2e: %v", runErr))
	}

	utils.Info("✅ e2e passed")
}

func failE2E(msg string) {
	if utils.JSONOutput {
		utils.JSONError(utils.ErrConfig, msg)
	} else {
		fmt.Fprintln(os.Stderr, "❌", msg)
	}
	exitProcess(1)
}

func collectE2EArtifacts(cmd *cobra.Command, suite *utils.E2ESuite, workdir string) {
	if len(suite.Artifacts) == 0 {
		return
	}

	dest, _ := cmd.Flags().GetString("artifacts-dir")
	if dest == "" {
		dest = filepath.Join(utils.CorgiComposePathDir, defaultE2EArtifactsDir)
	}

	var collected int
	for _, declared := range suite.Artifacts {
		rel := strings.Trim(declared, "/")
		if rel == "" {
			continue
		}
		src := filepath.Join(workdir, rel)
		info, statErr := os.Stat(src)
		if statErr != nil {
			utils.Infof(
				"⚠️  e2e artifacts: %q not found under the suite workdir (%s) - `artifacts:` paths are relative to `workdir:`\n",
				declared, workdir,
			)
			continue
		}

		target := filepath.Join(dest, filepath.Base(rel))
		var copyErr error
		if info.IsDir() {
			copyErr = copyTree(src, target)
		} else {
			if copyErr = os.MkdirAll(filepath.Dir(target), 0o755); copyErr == nil {
				copyErr = copyFile(src, target)
			}
		}
		if copyErr != nil {
			utils.Infof("⚠️  e2e artifacts: could not collect %q: %v\n", declared, copyErr)
			continue
		}
		collected++
	}

	if collected > 0 {
		utils.Infof("📦 collected %d e2e artifact path(s) into %s\n", collected, dest)
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, path)
		if relErr != nil {
			return relErr
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return copyFile(path, out)
	})
}
