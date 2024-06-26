package cmd

import (
	"andriiklymiuk/corgi/utils"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Short:   "Upgrade corgi to the latest version",
	Long:    `Use this command to upgrade corgi to the latest version available in Homebrew.`,
	Aliases: []string{"update"},
	Run:     upgradeRun,
}

func upgradeRun(cmd *cobra.Command, args []string) {
	currentVersion := APP_VERSION
	latestVersion, err := getLatestGitHubTag()
	if err != nil {
		fmt.Println("Failed to fetch the latest version from GitHub:", err)
		return
	}
	latestVersion = strings.TrimPrefix(latestVersion, "v")

	if currentVersion == latestVersion {
		fmt.Printf("You are already using the latest version of corgi (%s).\n", currentVersion)
		return
	}

	fmt.Println("Current version:", currentVersion)
	fmt.Println("Latest version available:", latestVersion)

	brewPath, err := utils.GetHomebrewBinPath()
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


func getLatestGitHubTag() (string, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/Andriiklymiuk/corgi/releases/latest", nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", "request")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}

	return data.TagName, nil
}
