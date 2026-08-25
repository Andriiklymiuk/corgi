package cmd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/workspace"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
)

// defaultMCPAddr is where `corgi agent up` serves MCP when no --http is given,
// so nobody has to remember a port.
const defaultMCPAddr = "127.0.0.1:8765"

var agentUpCmd = &cobra.Command{
	Use:   "up",
	Short: "One command from a stack directory to phone-startable: register, daemon, tunnel, pairing",
	Long: `Does the whole remote-session-start setup in one shot:

  1. registers the current stack in the workspace registry (if run in one)
  2. starts the agent daemon in the background (if not already running)
  3. starts the MCP endpoint with a public tunnel and a pairing window
     (if not already listening)

Everything runs detached, so an AI agent can run this and keep working.
Prints the public URL and the pairing code; --json emits the same as JSON.`,
	Run: runAgentUp,
}

type agentUpResult struct {
	Workspace  string `json:"workspace,omitempty"`
	Registered bool   `json:"registered"`
	DaemonPID  int    `json:"daemonPid"`
	MCPAddr    string `json:"mcpAddr"`
	MCPStarted bool   `json:"mcpStarted"`
	PublicURL  string `json:"publicUrl,omitempty"`
	PairCode   string `json:"pairingCode,omitempty"`
	PairURL    string `json:"pairingUrl,omitempty"`
	LogPath    string `json:"mcpLog,omitempty"`
	Hint       string `json:"hint,omitempty"`
}

func runAgentUp(cmd *cobra.Command, _ []string) {
	addr, _ := cmd.Flags().GetString("http")
	provider, _ := cmd.Flags().GetString("provider")

	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}

	var res agentUpResult
	res.MCPAddr = addr

	res.Workspace, res.Registered = registerCwdWorkspace()

	info, err := ensureDaemon(dir)
	if err != nil {
		exitWithError("agent_up_daemon", err, 1)
	}
	res.DaemonPID = info.PID

	res.LogPath = filepath.Join(dir, "mcp.log")
	if mcpListening(addr) {
		// A second pairing window needs a fresh `corgi mcp` start; saying so
		// beats silently reusing an endpoint that cannot pair anyone new.
		res.Hint = "MCP already listening — to pair another device, stop it and rerun `corgi agent up`"
		if url := readMCPRecord(dir); url != "" {
			res.PublicURL = url
		}
		printAgentUp(res)
		return
	}

	if err := spawnDetachedMCP(dir, addr, provider); err != nil {
		exitWithError("agent_up_mcp", err, 1)
	}
	res.MCPStarted = true

	parsed, err := awaitMCPLog(res.LogPath, 90*time.Second)
	if err != nil {
		exitWithError("agent_up_mcp", fmt.Errorf("%w — see %s", err, res.LogPath), 1)
	}
	res.PublicURL = parsed.publicURL
	res.PairCode = parsed.pairCode
	if res.PublicURL != "" && res.PairCode != "" {
		// The code rides in the fragment: it never reaches the server or its
		// logs, only the pair page's own JS.
		res.PairURL = res.PublicURL + "/pair#" + res.PairCode
	}
	writeMCPRecord(dir, res.PublicURL)

	printAgentUp(res)
}

// registerCwdWorkspace puts the current stack in the registry, like one step of
// `corgi agent scan`. Registration only — autostart stays opt-in, and remote
// start is the point of this command anyway.
func registerCwdWorkspace() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil || !dirHasComposeFile(cwd) {
		return "", false
	}
	id := filepath.Base(cwd)
	registry, path := mustLoadRegistry()
	if existing, ok := registry.Find(id); ok {
		if existing.AbsPath == cwd {
			return id, false
		}
		// The basename is already taken by a DIFFERENT directory. Repointing it
		// would hijack that workspace; disambiguate with the parent instead so
		// two repos both called "api" can coexist.
		id = filepath.Base(filepath.Dir(cwd)) + "-" + id
		if existing2, ok := registry.Find(id); ok && existing2.AbsPath == cwd {
			return id, false
		}
	}
	existing, _ := registry.Find(id)
	existing.ID = id
	existing.AbsPath = cwd
	existing.ComposeFile = composeFileName(cwd)
	existing.Status = workspace.StatusOK
	existing.Services, existing.Repos = describeStack(cwd)
	registry.Upsert(existing)
	if err := workspace.Save(path, registry); err != nil {
		exitWithError("agent_registry_write", err, 1)
	}
	return id, true
}

// ensureDaemon returns the running daemon's record, starting one detached when
// none is up.
func ensureDaemon(dir string) (*daemon.Info, error) {
	if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
		return info, nil
	}
	if err := spawnDetached(dir, "serve.log", "agent", "serve"); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
			return info, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, fmt.Errorf("daemon did not come up — see %s", filepath.Join(dir, "serve.log"))
}

func spawnDetachedMCP(dir, addr, provider string) error {
	args := []string{"mcp", "--http", addr, "--tunnel", "--pair"}
	if provider != "" {
		args = append(args, "--tunnel-provider", provider)
	}
	// Truncate the old log first: awaitMCPLog must not read a previous run's
	// URL or pairing code as this one's.
	_ = os.Remove(filepath.Join(dir, "mcp.log"))
	return spawnDetached(dir, "mcp.log", args...)
}

// spawnDetached starts corgi itself with args, output to <dir>/<logName>, in
// its own process group so it outlives this command and later Ctrl+Cs.
func spawnDetached(dir, logName string, args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(dir, logName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	c := exec.Command(exe, args...)
	c.Stdout = logFile
	c.Stderr = logFile
	c.Stdin = nil
	utils.SetProcessGroup(c)
	return c.Start()
}

func mcpListening(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type mcpLogInfo struct {
	publicURL string
	pairCode  string
}

var (
	mcpPublicURLPattern = regexp.MustCompile(`public MCP endpoint: (\S+)/mcp`)
	mcpPairCodePattern  = regexp.MustCompile(`pairing code: (\S+)`)
	mcpFatalPattern     = regexp.MustCompile(`(?m)^(mcp server error:|corgi mcp --pair cannot|could not start pairing:|tunnel: )`)
)

// parseMCPLog extracts what the summary needs from `corgi mcp`'s own output.
func parseMCPLog(log string) (mcpLogInfo, bool) {
	var out mcpLogInfo
	if m := mcpPublicURLPattern.FindStringSubmatch(log); m != nil {
		out.publicURL = m[1]
	}
	if m := mcpPairCodePattern.FindStringSubmatch(log); m != nil {
		out.pairCode = m[1]
	}
	return out, out.publicURL != "" && out.pairCode != ""
}

// awaitMCPLog polls the detached server's log until the tunnel URL and pairing
// code both appear, or something fatal is printed.
func awaitMCPLog(path string, timeout time.Duration) (mcpLogInfo, error) {
	deadline := time.Now().Add(timeout)
	var last mcpLogInfo
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			log := string(data)
			if mcpFatalPattern.MatchString(log) {
				return last, fmt.Errorf("mcp failed to start")
			}
			info, done := parseMCPLog(log)
			last = info
			if done {
				return info, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if last.pairCode != "" {
		// The endpoint is pairable on the LAN even when no tunnel appeared —
		// usually a missing cloudflared. Report what works instead of failing.
		return last, nil
	}
	return last, fmt.Errorf("no pairing code within %s", timeout)
}

func mcpRecordPath(dir string) string { return filepath.Join(dir, "mcp-url.txt") }

func writeMCPRecord(dir, url string) {
	if url == "" {
		return
	}
	_ = os.WriteFile(mcpRecordPath(dir), []byte(url+"\n"), 0o600)
}

func readMCPRecord(dir string) string {
	data, err := os.ReadFile(mcpRecordPath(dir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func printAgentUp(res agentUpResult) {
	if utils.JSONOutput {
		utils.PrintJSON(res)
		return
	}
	fmt.Println()
	if res.Workspace != "" {
		verb := "already registered"
		if res.Registered {
			verb = "registered"
		}
		fmt.Printf("  ✓ workspace %s (%s)\n", res.Workspace, verb)
	}
	fmt.Printf("  ✓ agent daemon running (pid %d)\n", res.DaemonPID)
	switch {
	case res.PublicURL != "":
		fmt.Printf("  ✓ public endpoint: %s/mcp\n", res.PublicURL)
	default:
		fmt.Printf("  ✓ local endpoint: http://%s/mcp (no tunnel yet — see %s)\n", res.MCPAddr, res.LogPath)
	}
	if res.PairCode != "" {
		fmt.Println()
		if res.PairURL != "" {
			fmt.Println("  📱 scan to pair (single use, 2 minutes):")
			fmt.Println()
			printTerminalQR(res.PairURL)
			fmt.Printf("    or open: %s\n", res.PairURL)
		} else {
			fmt.Println("  pair a device (single use, 2 minutes):")
			fmt.Printf("    code: %s — POST http://%s/pair {\"code\":\"%s\",\"device\":\"my-phone\"}\n",
				res.PairCode, res.MCPAddr, res.PairCode)
		}
	}
	if res.Hint != "" {
		fmt.Printf("  %s\n", res.Hint)
	}
	fmt.Println()
	fmt.Println("  start a session from anywhere: corgi_session_start {\"workspace\":\"" + orDefault(res.Workspace, "<name>") + "\"}")
}

// printTerminalQR renders a scannable QR in the terminal, indented to match
// the summary block. Best-effort: a QR too big to encode just prints nothing.
func printTerminalQR(content string) {
	q, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(q.ToSmallString(false), "\n"), "\n") {
		fmt.Println("   " + line)
	}
	fmt.Println()
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func init() {
	agentUpCmd.Flags().String("http", defaultMCPAddr, "Local MCP address")
	agentUpCmd.Flags().String("provider", "", "Tunnel provider (cloudflared|ngrok|localtunnel)")
	agentCmd.AddCommand(agentUpCmd)
}
