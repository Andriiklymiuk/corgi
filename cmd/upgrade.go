package cmd

import (
	"andriiklymiuk/corgi/utils"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

const (
	installScriptURL    = "https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh"
	installPs1ScriptURL = "https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.ps1"
)

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Short:   "Upgrade corgi to the latest version",
	Long:    `Upgrade corgi using whichever install method you originally used (Homebrew, curl install script, or PowerShell installer on Windows).`,
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

	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Error finding the executable path:", err)
		return
	}
	exeDir := filepath.Dir(exePath)

	switch detectInstallMethod(exeDir) {
	case installMethodHomebrew:
		fmt.Println("Detected Homebrew install. Upgrading via Homebrew...")
		if err := upgradeViaHomebrew(); err != nil {
			fmt.Printf("Failed to upgrade via Homebrew: %s\n", err)
		} else {
			fmt.Println("Upgrade successful!")
		}
	case installMethodScript:
		fmt.Printf("Detected script install at %s. Re-running install script...\n", exeDir)
		if err := upgradeViaInstallScript(exeDir); err != nil {
			fmt.Printf("Failed to upgrade via install script: %s\n", err)
		} else {
			fmt.Println("Upgrade successful!")
		}
	case installMethodWindows:
		// We can't safely overwrite the running corgi.exe from inside corgi.exe.
		fmt.Printf("Detected Windows install at %s.\n", exeDir)
		fmt.Println("Run this from another PowerShell window to upgrade:")
		fmt.Printf("  irm %s | iex\n", installPs1ScriptURL)
	default:
		fmt.Printf("Could not detect how corgi was installed (located at %s).\n", exePath)
		fmt.Println("Re-install with one of:")
		fmt.Println("  brew upgrade andriiklymiuk/homebrew-tools/corgi")
		fmt.Printf("  curl -fsSL %s | sh\n", installScriptURL)
		if runtime.GOOS == "windows" {
			fmt.Printf("  irm %s | iex\n", installPs1ScriptURL)
		}
	}
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

type installMethod int

const (
	installMethodUnknown installMethod = iota
	installMethodHomebrew
	installMethodScript
	installMethodWindows
)

func detectInstallMethod(exeDir string) installMethod {
	if runtime.GOOS == "windows" {
		for _, dir := range windowsInstallDirs() {
			if pathsEqual(exeDir, dir) {
				return installMethodWindows
			}
		}
		return installMethodUnknown
	}

	if brewBin, err := utils.GetHomebrewBinPath(); err == nil {
		if pathsEqual(exeDir, brewBin) {
			return installMethodHomebrew
		}
	}

	for _, dir := range scriptInstallDirs() {
		if pathsEqual(exeDir, dir) {
			return installMethodScript
		}
	}

	return installMethodUnknown
}

func scriptInstallDirs() []string {
	dirs := []string{"/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"))
		dirs = append(dirs, filepath.Join(home, ".corgi", "bin"))
	}
	return dirs
}

func windowsInstallDirs() []string {
	var dirs []string
	if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
		dirs = append(dirs, filepath.Join(appData, "corgi", "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".corgi", "bin"))
	}
	return dirs
}

func pathsEqual(a, b string) bool {
	ap, err := filepath.EvalSymlinks(a)
	if err != nil {
		ap = a
	}
	bp, err := filepath.EvalSymlinks(b)
	if err != nil {
		bp = b
	}
	return filepath.Clean(ap) == filepath.Clean(bp)
}

func upgradeViaHomebrew() error {
	updateCmd := exec.Command("brew", "update")
	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("brew update failed: %w", err)
	}

	upgradeCmd := exec.Command("brew", "upgrade", "andriiklymiuk/homebrew-tools/corgi")
	upgradeCmd.Stdout = os.Stdout
	upgradeCmd.Stderr = os.Stderr
	return upgradeCmd.Run()
}

func upgradeViaInstallScript(installDir string) error {
	if _, err := exec.LookPath("sh"); err != nil {
		return fmt.Errorf("sh is required to run the install script: %w", err)
	}

	curlPath, curlErr := exec.LookPath("curl")
	wgetPath, wgetErr := exec.LookPath("wget")
	if curlErr != nil && wgetErr != nil {
		return fmt.Errorf("need curl or wget on PATH to fetch the install script")
	}

	var pipeline string
	if curlErr == nil {
		pipeline = fmt.Sprintf("%s -fsSL %s | sh", curlPath, installScriptURL)
	} else {
		pipeline = fmt.Sprintf("%s -qO- %s | sh", wgetPath, installScriptURL)
	}

	c := exec.Command("sh", "-c", pipeline)
	c.Env = append(os.Environ(), "CORGI_INSTALL_DIR="+installDir)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
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
