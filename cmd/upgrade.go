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
	"time"

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
	Aliases: []string{"update", "upd"},
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
			refreshDaemonAfterUpgrade(exePath)
		}
	case installMethodScript:
		fmt.Printf("Detected script install at %s. Re-running install script...\n", exeDir)
		if err := upgradeViaInstallScript(exeDir); err != nil {
			fmt.Printf("Failed to upgrade via install script: %s\n", err)
		} else {
			fmt.Println("Upgrade successful!")
			refreshDaemonAfterUpgrade(exePath)
		}
	case installMethodWindows:
		// We can't safely overwrite the running corgi.exe from inside corgi.exe.
		fmt.Printf("Detected Windows install at %s.\n", exeDir)
		fmt.Println("Run this from another PowerShell window to upgrade:")
		fmt.Printf("  irm %s | iex\n", installPs1ScriptURL)
	default:
		fmt.Printf("Could not detect how corgi was installed (located at %s).\n", exePath)
		fmt.Println("Re-install with one of:")
		fmt.Println("  brew upgrade --cask andriiklymiuk/tools/corgi")
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
	run := func(args ...string) error {
		c := exec.Command("brew", args...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}
	if err := run("update"); err != nil {
		return fmt.Errorf("brew update failed: %w", err)
	}
	// Homebrew loads casks only from trusted taps; older brews have no
	// such command, and then there is nothing to trust.
	_ = exec.Command("brew", "trust", "andriiklymiuk/tools").Run()

	// corgi moved from a formula to a cask. An install that predates that
	// still holds the formula keg, which brew upgrade will not replace on
	// its own; swap it for the cask once.
	if exec.Command("brew", "list", "--formula", "corgi").Run() == nil {
		fmt.Println("corgi is a Homebrew cask now — replacing the old formula install")
		if err := run("uninstall", "--formula", "corgi"); err != nil {
			return err
		}
		return run("install", "--cask", "andriiklymiuk/tools/corgi")
	}
	return run("upgrade", "--cask", "andriiklymiuk/tools/corgi")
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
	// A timeout matters here beyond the upgrade command: the MCP server calls
	// this hourly from a goroutine, and a hung connection would park one for
	// good, once per hour, for the life of the process.
	client := &http.Client{Timeout: 10 * time.Second}
	if tag, err := latestTagFromAPI(client); err == nil && tag != "" {
		return tag, nil
	}
	// The API allows sixty anonymous calls an hour per address and answers
	// 403 past that; the release page's redirect carries the same tag and
	// has no such limit.
	return latestTagFromRedirect(client)
}

func latestTagFromAPI(client *http.Client) (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/Andriiklymiuk/corgi/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "corgi")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api: %s", resp.Status)
	}
	var data struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if data.TagName == "" {
		return "", fmt.Errorf("github api: no tag in the response")
	}
	return data.TagName, nil
}

func latestTagFromRedirect(client *http.Client) (string, error) {
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequest("HEAD", "https://github.com/Andriiklymiuk/corgi/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "corgi")
	resp, err := noFollow.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	tag := tagFromReleaseLocation(resp.Header.Get("Location"))
	if tag == "" {
		return "", fmt.Errorf("github releases: %s, no tag in the redirect", resp.Status)
	}
	return tag, nil
}

// tagFromReleaseLocation reads the tag out of the Location a releases/latest
// request redirects to: .../releases/tag/v1.21.50 → v1.21.50.
func tagFromReleaseLocation(location string) string {
	const marker = "/releases/tag/"
	i := strings.LastIndex(location, marker)
	if i < 0 {
		return ""
	}
	tag := location[i+len(marker):]
	if j := strings.IndexAny(tag, "?#"); j >= 0 {
		tag = tag[:j]
	}
	return strings.TrimSpace(tag)
}

// refreshDaemonAfterUpgrade hands the login service the corgi that was just
// installed. The daemon runs from its own copy (see agent_install_stable.go),
// and that copy is only refreshed by `agent install` — which has to be the
// new binary, not this process, so the copy is the new version.
func refreshDaemonAfterUpgrade(exePath string) {
	if !daemonRunsFromStableCopy() || !loginServiceInstalled() {
		return
	}
	if installedDaemonBinary() != mustStableDaemonBinary() {
		// Older installs point launchd straight at Homebrew's path, which is
		// what brings the macOS file prompt back after every update.
		fmt.Println("The daemon still starts from Homebrew's path — run `corgi agent install` once to stop macOS asking about Documents after every update.")
		return
	}
	out, err := exec.Command(exePath, "agent", "install").CombinedOutput()
	if err != nil {
		fmt.Printf("Could not refresh the daemon's copy of corgi: %s\n%s", err, out)
		fmt.Println("Run `corgi agent install` yourself.")
		return
	}
	fmt.Println("Daemon restarted from the new corgi.")
}

func mustStableDaemonBinary() string {
	p, _ := stableDaemonBinary()
	return p
}
