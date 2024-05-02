package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade corgi to the latest version",
	Long:  `Use this command to upgrade corgi to the latest version available in Homebrew.`,
	Run:   upgradeRun,
}

func upgradeRun(cmd *cobra.Command, args []string) {
	brewPath, err := getHomebrewBinPath()
	if err != nil {
		fmt.Println("Error determining Homebrew binary path:", err)
		return
	}

	if isHomebrewInstallation(brewPath) {
		fmt.Println("Upgrading corgi via Homebrew...")
		if err := upgradeCorgi(); err != nil {
			fmt.Printf("Failed to upgrade: %s\n", err)
		} else {
			fmt.Println("Upgrade successful!")
		}
	} else {
		fmt.Println("Corgi was not installed via Homebrew or not found in standard Homebrew paths.")
	}
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}
func isHomebrewInstallation(brewPath string) bool {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Error finding the executable path:", err)
		return false
	}
	exePath = filepath.Dir(exePath)
	fmt.Println("Executable path: ", exePath)

	return exePath == brewPath
}

func upgradeCorgi() error {
	cmd := exec.Command("brew", "upgrade", "corgi")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getHomebrewBinPath() (string, error) {
	cmd := exec.Command("brew", "--prefix")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/bin", strings.TrimSpace(string(output))), nil
}
