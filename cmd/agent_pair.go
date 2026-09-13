package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/pairing"
)

// A fresh pairing window without restarting anything: the running MCP
// server opens one on request and hands back the code. What `corgi agent
// up` printed once, printable again — for a second phone, a teammate's
// (--viewer), or the bar's AirDrop, which sends the code inside a file the
// phone opens with corgi: no QR, no typing.

var agentPairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Open a fresh pairing window and print its QR — or write a .corgipair file to AirDrop",
	Long: `The MCP server corgi agent up started keeps serving; this asks it for a new
single-use pairing code (ten minutes) and prints the QR and the link, the way
agent up did.

  corgi agent pair                    a new QR and code
  corgi agent pair --viewer           a read-only window, for a teammate's phone
  corgi agent pair --file             also write ~/Desktop/<laptop>.corgipair: AirDrop it to the phone,
                                      which opens it with corgi and is paired — no scanning, no typing
  corgi agent pair --json

The file holds the code and the address, nothing that lasts: the code dies in
ten minutes or on first use, and the phone's own token is minted on the laptop
when it pairs. AirDrop carries it end-to-end encrypted between your devices.`,
	Run: func(cmd *cobra.Command, _ []string) {
		dir := mustAgentDir()
		viewer, _ := cmd.Flags().GetBool("viewer")
		file, _ := cmd.Flags().GetBool("file")
		info, err := daemon.ReadInfo(dir)
		if err != nil || info == nil {
			exitWithError("agent_pair", fmt.Errorf("corgi agent is not running — corgi agent up first"), 1)
		}
		if _, err := os.Stat(filepath.Join(dir, "mcp.pid")); err != nil {
			exitWithError("agent_pair", fmt.Errorf("no MCP server is running — corgi agent up first"), 1)
		}
		ans, err := requestPairWindow(dir, viewer, 6*time.Second)
		if err != nil {
			exitWithError("agent_pair", err, 1)
		}
		base := firstNonEmpty(ans.PublicURL, ans.LocalURL)
		link := ""
		if base != "" {
			link = base + "/pair#" + ans.Code
		}
		out := map[string]any{"code": ans.Code, "expiresAt": ans.ExpiresAt, "daemon": ans.Daemon, "role": ans.Role, "url": base, "pairUrl": link}
		var path string
		if file {
			path, err = writePairFile(ans)
			if err != nil {
				exitWithError("agent_pair", err, 1)
			}
			out["file"] = path
		}
		if utils.JSONOutput {
			utils.PrintJSON(out)
			return
		}
		fmt.Println()
		if link != "" {
			fmt.Println("  📱 scan to pair (single use, 10 minutes):")
			fmt.Println()
			printTerminalQR(link)
			fmt.Printf("    or open: %s\n", link)
		} else {
			fmt.Printf("  code: %s (single use, 10 minutes) — the phone finds this laptop nearby and asks for it\n", ans.Code)
		}
		if ans.Role == pairing.RoleViewer {
			fmt.Println("  this window pairs a phone that only reads")
		}
		if path != "" {
			fmt.Printf("  📤 AirDrop %s to the phone: it opens in corgi and pairs itself\n", path)
		}
	},
}

// requestPairWindow asks the server for a window and waits for its answer.
func requestPairWindow(dir string, viewer bool, wait time.Duration) (pairAnswer, error) {
	answer := filepath.Join(dir, pairAnswerName)
	_ = os.Remove(answer)
	role := ""
	if viewer {
		role = pairing.RoleViewer
	}
	if err := os.WriteFile(filepath.Join(dir, pairRequestName), []byte(role+"\n"), 0o600); err != nil {
		return pairAnswer{}, err
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(answer); err == nil {
			var ans pairAnswer
			if json.Unmarshal(raw, &ans) == nil && ans.Code != "" {
				_ = os.Remove(answer)
				return ans, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = os.Remove(filepath.Join(dir, pairRequestName))
	return pairAnswer{}, fmt.Errorf("the MCP server did not open a window — is it the one corgi agent up started, on corgi 2.22.4 or newer?")
}

// PairFile is what a .corgipair holds: enough for the phone to pair, and
// nothing that outlives the code.
type PairFile struct {
	Corgi   string    `json:"corgi"`
	Daemon  string    `json:"daemon"`
	URL     string    `json:"url,omitempty"`
	Code    string    `json:"code"`
	Expires time.Time `json:"expiresAt"`
	Role    string    `json:"role,omitempty"`
}

func writePairFile(ans pairAnswer) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == ':' {
			return '-'
		}
		return r
	}, ans.Daemon)
	if name == "" {
		name = "laptop"
	}
	path := filepath.Join(home, "Desktop", name+".corgipair")
	data, _ := json.MarshalIndent(PairFile{Corgi: APP_VERSION, Daemon: ans.Daemon, URL: firstNonEmpty(ans.PublicURL, ans.LocalURL), Code: ans.Code, Expires: ans.ExpiresAt, Role: ans.Role}, "", "  ")
	return path, os.WriteFile(path, data, 0o600)
}

func init() {
	agentPairCmd.Flags().Bool("viewer", false, "A window whose phone only reads the board")
	agentPairCmd.Flags().Bool("file", false, "Also write ~/Desktop/<laptop>.corgipair to AirDrop to the phone")
	agentCmd.AddCommand(agentPairCmd)
}
