package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

type openTarget struct {
	Service string `json:"service"`
	URL     string `json:"url"`
}

var openCmd = &cobra.Command{
	Use:   "open [services...]",
	Short: "Open service URLs in the browser",
	Long: `Opens http://localhost:<port> for each selected service that has a port.
With no args, opens every service that has a port. db_services are skipped.

In --json mode or when there is no terminal, the URLs are printed instead of
launched.`,
	Run: runOpen,
}

func init() {
	rootCmd.AddCommand(openCmd)
}

// openTargets resolves the service URLs to open. names empty = all services
// with a port. db_services are never browsable and are excluded.
func openTargets(corgi *utils.CorgiCompose, names []string) []openTarget {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var targets []openTarget
	for _, svc := range corgi.Services {
		if svc.Port == 0 {
			continue
		}
		if len(names) > 0 && !want[svc.ServiceName] {
			continue
		}
		targets = append(targets, openTarget{
			Service: svc.ServiceName,
			URL:     fmt.Sprintf("http://localhost:%d", svc.Port),
		})
	}
	return targets
}

// browserCommand returns the OS launcher command + args for a URL. A non-empty
// browser opens in that specific app (macOS `open -a`; Linux best-effort via the
// named binary; Windows falls back to the default handler).
func browserCommand(url, browser string) (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		if browser != "" {
			return "open", []string{"-a", browser, url}
		}
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		if browser != "" {
			return browser, []string{url}
		}
		return "xdg-open", []string{url}
	}
}

// launcher is overridable in tests.
var launcher = func(url string) error {
	return launchBrowser(url, "")
}

func launchBrowser(url, browser string) error {
	name, cmdArgs := browserCommand(url, browser)
	return exec.Command(name, cmdArgs...).Start()
}

func runOpen(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError(utils.ErrConfig, err.Error())
		} else {
			utils.Infof("couldn't get services config: %s\n", err)
		}
		os.Exit(1)
	}

	targets := openTargets(corgi, args)

	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{"opened": targets})
		return
	}

	if len(targets) == 0 {
		utils.Info("No services with a port to open.")
		return
	}

	for _, t := range targets {
		if utils.NonInteractive {
			utils.Infof("%s → %s\n", t.Service, t.URL)
			continue
		}
		if err := launcher(t.URL); err != nil {
			utils.Infof("could not open %s (%s): %s\n", t.Service, t.URL, err)
			continue
		}
		utils.Infof("opened %s → %s\n", t.Service, t.URL)
	}
}
