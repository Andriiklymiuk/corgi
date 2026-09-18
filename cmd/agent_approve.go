package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var agentApproveCmd = &cobra.Command{
	Use:   "approve <code>",
	Short: "Approve a Claude connector sign-in waiting on this machine",
	Long: `When a Claude connector (or any MCP client) signs in to corgi from a browser
that has never opened the dashboard, the consent page shows a short code and
asks you to run this on the machine the daemon runs on:

  corgi agent approve ABCD-2345

That proves you are at the machine. The page then finishes the sign-in on its
own. Codes live ten minutes.`,
	Args: cobra.ExactArgs(1),
	Run:  runAgentApprove,
}

func runAgentApprove(_ *cobra.Command, args []string) {
	dir := mustAgentDir()
	client, err := approvePendingLocally(dir, strings.TrimSpace(args[0]))
	if err != nil {
		exitWithError("agent_approve", err, 1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]string{"status": "approved", "client": client})
		return
	}
	fmt.Printf("approved %s — the browser page finishes the sign-in\n", client)
}

func readLocalMCPAddr(agentDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(agentDir, mcpAddrName))
	if err != nil {
		return "", errors.New("the daemon is not running — start it with `corgi agent up`")
	}
	addr := loopbackAddr(strings.TrimSpace(string(data)))
	if addr == "" {
		return "", errors.New("the daemon's MCP address is not dialable from here")
	}
	return addr, nil
}

func init() {
	agentCmd.AddCommand(agentApproveCmd)
}
