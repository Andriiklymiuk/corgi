package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"
)

var agentDashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Open the dashboard in this machine's browser",
	Long: `Opens the corgi dashboard locally and authorises this browser on the way.

  corgi agent dashboard          # open it
  corgi agent dashboard --print  # print the link instead

The phone pairs by scanning a QR, which is the right shape for a device that
is not this one. A browser on the machine running the daemon needs no code:
anything that can run this command can already read the daemon's own files.`,
	Run: runAgentDashboard,
}

func runAgentDashboard(cmd *cobra.Command, _ []string) {
	dir := mustAgentDir()
	addr, err := os.ReadFile(filepath.Join(dir, mcpAddrName))
	if err != nil {
		exitWithError("agent_dashboard", fmt.Errorf("the daemon is not running — start it with `corgi agent up`"), 2)
	}
	base := "http://" + strings.TrimSpace(string(addr)) // NOSONAR — loopback on this machine, no certificate exists for it

	name, _ := cmd.Flags().GetString("name")
	token, err := pairing.PairLocal(pairing.StorePath(dir), name)
	if err != nil {
		exitWithError("agent_dashboard", err, 1)
	}
	// The token rides in the fragment, which never reaches the server or its
	// logs — only the page's own JS, which puts it in this browser's storage.
	link := base + "/app#token=" + token

	// --print writes a working key to the terminal, which is a place keys get
	// pasted into chats and screenshots. Only to a real terminal, and said
	// out loud; a pipe or a log gets the refusal instead.
	if printOnly, _ := cmd.Flags().GetBool("print"); printOnly {
		if utils.JSONOutput {
			exitWithError("agent_dashboard", fmt.Errorf("--print --json would put a working key in a log; open it instead: corgi agent dashboard"), 2)
		}
		if !stdoutIsTerminal() {
			exitWithError("agent_dashboard", fmt.Errorf("--print is for a terminal you are looking at, not a pipe — run `corgi agent dashboard` to open it directly"), 2)
		}
		fmt.Println(link)
		fmt.Println("\nthat link contains a key for this machine — do not paste it anywhere.")
		fmt.Println("revoke it any time: corgi mcp devices revoke " + name)
		return
	}
	if err := openInBrowser(link); err != nil {
		fmt.Println("open this in your browser:")
		fmt.Println("  " + link)
		return
	}
	fmt.Printf("opened the dashboard, paired as %q\n", name)
	fmt.Println("revoke it any time: corgi mcp devices revoke " + name)
}

// stdoutIsTerminal says someone is looking at this, rather than a file or a
// pipe that will keep the key after they have stopped.
func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func openInBrowser(link string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", link).Run()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", link).Run()
	default:
		return exec.Command("xdg-open", link).Run()
	}
}

func init() {
	agentDashboardCmd.Flags().String("name", "this-laptop", "Device name to pair as; pairing again replaces it")
	agentDashboardCmd.Flags().Bool("print", false, "Print the link instead of opening it")
	agentCmd.AddCommand(agentDashboardCmd)
}
