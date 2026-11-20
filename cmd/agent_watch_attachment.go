package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/watch"

	"github.com/spf13/cobra"
)

var agentWatchAttachmentCmd = &cobra.Command{
	Use:   "attachment <url>",
	Short: "Download a file attached to a Jira ticket, with the workspace's own Jira login",
	Long: `Saves one attachment of a Jira ticket to a file, so a session can read a
screenshot, a log or a document the ticket carries.

corgi signs in with the Jira login this workspace already uses
(corgi agent watch auth jira) and sends it only to that Jira site: an
address on any other host is refused, and a redirect to a file store gets
no login. The file lands in the folder you name, never above it.

  corgi agent watch attachment "https://acme.atlassian.net/rest/api/3/attachment/content/10042"
  corgi agent watch attachment "<content-url>" --out /tmp/ticket-files`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		dir := mustAgentDir()
		ws, _ := cmd.Flags().GetString("workspace")
		if ws = strings.TrimSpace(ws); ws == "" {
			if cwd, err := os.Getwd(); err == nil {
				ws = workspaceIDForDir(dir, cwd)
			}
		}
		out, _ := cmd.Flags().GetString("out")
		if out = strings.TrimSpace(out); out == "" {
			out = os.TempDir()
		}
		if err := os.MkdirAll(out, 0o700); err != nil {
			exitWithError("agent_watch", err, 1)
		}
		tmp, err := os.CreateTemp(out, ".corgi-attachment-*")
		if err != nil {
			exitWithError("agent_watch", err, 1)
		}
		defer os.Remove(tmp.Name())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		jira := watch.NewJira(watch.LoadSecretsFor(dir, ws), "")
		name, size, err := jira.Download(ctx, args[0], tmp)
		_ = tmp.Close()
		if err != nil {
			exitWithError("agent_watch", err, 2)
		}
		saved := filepath.Join(out, filepath.Base(name))
		if err := os.Rename(tmp.Name(), saved); err != nil {
			exitWithError("agent_watch", err, 1)
		}
		if utils.JSONOutput {
			utils.PrintJSON(map[string]any{"file": saved, "bytes": size})
			return
		}
		fmt.Println(saved)
	},
}

func init() {
	agentWatchAttachmentCmd.Flags().String("out", "", "the folder to save into (default: the temp folder)")
	agentWatchAttachmentCmd.Flags().String("workspace", "", "whose Jira login to use (default: the workspace this folder is in)")
	agentWatchCmd.AddCommand(agentWatchAttachmentCmd)
}
