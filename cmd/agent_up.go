package cmd

import (
	"andriiklymiuk/corgi/utils/agent/push"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/daemon"
	"andriiklymiuk/corgi/utils/agent/supervisor"
	"andriiklymiuk/corgi/utils/agent/workspace"
	"andriiklymiuk/corgi/utils/tunnel"

	qrcode "github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
)

const (
	flagTunnelName     = "tunnel-name"
	flagTunnelHostname = "tunnel-hostname"
)

const defaultMCPAddr = "127.0.0.1:8765"

const mcpLogName = "mcp.log"

const mcpPidName = "mcp.pid"

const mcpVersionName = "mcp.version"

const mcpAddrName = "mcp.addr"

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
	Workspace      string `json:"workspace,omitempty"`
	Registered     bool   `json:"registered"`
	DaemonPID      int    `json:"daemonPid"`
	MCPAddr        string `json:"mcpAddr"`
	MCPStarted     bool   `json:"mcpStarted"`
	PublicURL      string `json:"publicUrl,omitempty"`
	TunnelHostname string `json:"tunnelHostname,omitempty"`
	QuickTunnel    bool   `json:"quickTunnel,omitempty"`
	PairCode       string `json:"pairingCode,omitempty"`
	PairURL        string `json:"pairingUrl,omitempty"`
	LogPath        string `json:"mcpLog,omitempty"`
	AtLogin        bool   `json:"atLogin"`
	Hint           string `json:"hint,omitempty"`
}

func runAgentUp(cmd *cobra.Command, _ []string) {
	fresh, _ := cmd.Flags().GetBool("fresh")

	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}

	cur := upSettingsFromFlags(cmd)
	settings, reused := mergeUpSettings(cur, cmd.Flags().Changed, loadUpSettings(dir))
	if reused != "" {
		utils.Infof("using saved settings from the last up: %s (pass the flags to change them)\n", reused)
	}
	addr := settings.HTTP
	if addr == "" {
		addr = defaultMCPAddr
	}

	tunnel, err := tunnelArgs(settings.Provider, settings.TunnelName, settings.TunnelHostname)
	if viewer, _ := cmd.Flags().GetBool("viewer"); viewer && err == nil {
		tunnel = append(tunnel, "--viewer")
	}
	if err != nil {
		exitWithError("agent_up_tunnel", err, 2)
	}
	if err := tunnelPreflight(settings.Provider, settings.TunnelName, settings.TunnelHostname); err != nil {
		exitWithError("agent_up_tunnel", err, 2)
	}

	release, err := acquireUpLock(dir)
	if err != nil {
		exitWithError("agent_up_locked", err, 1)
	}
	defer release()

	var res agentUpResult
	res.TunnelHostname = strings.TrimSpace(settings.TunnelHostname)
	res.MCPAddr = addr

	res.Workspace, res.Registered = registerCwdWorkspace()
	if res.Workspace != "" {
		if absPath, cfgDir, ok := workspaceSessionTarget(res.Workspace, runningProfile(res.Workspace)); ok {
			warnIfUntrusted(cfgDir, absPath)
		}
	}

	info, err := ensureDaemon(dir)
	if err != nil {
		exitWithError("agent_up_daemon", err, 1)
	}
	res.DaemonPID = info.PID

	ensureAtLogin(dir, cmd, &settings)
	res.AtLogin = settings.AtLogin

	res.LogPath = filepath.Join(dir, mcpLogName)
	if mcpListening(addr) {
		if !fresh {
			if pid, ok := readAgentPidFile(filepath.Join(dir, mcpPidName)); ok && utils.PidAlive(pid, "") {
				if data, rerr := os.ReadFile(res.LogPath); rerr == nil {
					if parsed, _ := parseMCPLog(string(data)); parsed.publicURL != "" {
						res.PublicURL = parsed.publicURL
					}
				}
				res.Hint = "MCP + tunnel already running. To pair a NEW device (fresh pairing window + tunnel), rerun with --fresh."
			} else {
				res.Hint = fmt.Sprintf(
					"%s is held by a server corgi did not record starting. If it is a leftover corgi MCP, "+
						"rerun `corgi agent up --fresh` to replace it (or `corgi agent down` to stop it); "+
						"anything else, free the port or pass --http with a free one. Log: %s",
					addr, res.LogPath)
			}
			printAgentUp(res)
			return
		}
		found, freed := reclaimCorgiMCP(addr)
		switch {
		case freed:
			_ = os.Remove(filepath.Join(dir, mcpPidName))
		case found:
			exitWithError("agent_up_mcp", fmt.Errorf(
				"a corgi MCP on %s did not release the port within 5s — stop it manually (`corgi agent down`, or kill the pid lsof names) and rerun", addr), 1)
		default:
			res.Hint = fmt.Sprintf(
				"%s is already in use by something that is not corgi's MCP server. "+
					"Free the port (or pass --http with a free one) and rerun `corgi agent up`.", addr)
			printAgentUp(res)
			return
		}
	}

	if code, _ := cmd.Flags().GetString("pair-code"); strings.TrimSpace(code) != "" {
		ttl, _ := cmd.Flags().GetDuration("pair-ttl")
		if err := writePairLaunch(dir, code, ttl); err != nil {
			exitWithError("agent_up_pair", err, 2)
		}
	}
	if err := spawnDetachedMCP(dir, addr, tunnel); err != nil {
		exitWithError("agent_up_mcp", err, 1)
	}
	res.MCPStarted = true

	parsed, err := awaitMCPLog(res.LogPath, 90*time.Second)
	if err != nil {
		exitWithError("agent_up_mcp", fmt.Errorf("%w — see %s", err, res.LogPath), 1)
	}
	res.PublicURL = parsed.publicURL
	res.PairCode = parsed.pairCode
	if res.PublicURL != "" && settings.LastPublicURL != "" && settings.LastPublicURL != res.PublicURL {
		announceNewAddress(dir, res.PublicURL)
	}
	if res.PublicURL != "" {
		settings.LastPublicURL = res.PublicURL
	}
	_ = saveUpSettings(dir, settings)
	if res.PublicURL != "" && res.PairCode != "" {
		res.PairURL = res.PublicURL + "/pair#" + res.PairCode
	}

	printAgentUp(res)
}

func registerCwdWorkspace() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil || !dirIsWorkspace(cwd) {
		return "", false
	}
	id := filepath.Base(cwd)
	registry, path := mustLoadRegistry()
	if existing, ok := registry.Find(id); ok {
		if existing.AbsPath == cwd {
			return id, false
		}
		id = filepath.Base(filepath.Dir(cwd)) + "-" + id
		if existing2, ok := registry.Find(id); ok {
			if existing2.AbsPath == cwd {
				return id, false
			}
			utils.Infof("workspace names %q and %q are both taken by other directories — register with `corgi agent init --id <name>`\n",
				filepath.Base(cwd), id)
			return "", false
		}
	}
	existing, _ := registry.Find(id)
	existing.ID = id
	existing.AbsPath = cwd
	existing.ComposeFile = registeredComposeFile(cwd)
	existing.Status = workspace.StatusOK
	existing.Services, existing.Repos = describeStack(cwd)
	registry.Upsert(existing)
	if err := workspace.Save(path, registry); err != nil {
		exitWithError("agent_registry_write", err, 1)
	}
	if note := protectedWorkspaceNote(cwd); note != "" {
		utils.Infof("ℹ %s\n", note)
	}
	return id, true
}

func ensureDaemon(dir string) (*daemon.Info, error) {
	if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
		return info, nil
	}
	if strays := otherServers(os.Getpid()); len(strays) > 0 {
		return nil, fmt.Errorf("a corgi agent daemon (pid %d) is running without its record — `corgi agent restart` replaces it", strays[0])
	}
	if _, err := spawnDetached(dir, "serve.log", "agent", "serve"); err != nil {
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

type upSettings struct {
	HTTP           string `json:"http,omitempty"`
	Provider       string `json:"provider,omitempty"`
	TunnelName     string `json:"tunnelName,omitempty"`
	TunnelHostname string `json:"tunnelHostname,omitempty"`
	AtLogin        bool   `json:"atLogin,omitempty"`
	AtLoginAsked   bool   `json:"atLoginAsked,omitempty"`
	LastPublicURL  string `json:"lastPublicUrl,omitempty"`
}

const upSettingsName = "up.json"

func upSettingsFromFlags(cmd *cobra.Command) upSettings {
	var s upSettings
	s.HTTP, _ = cmd.Flags().GetString("http")
	s.Provider, _ = cmd.Flags().GetString("provider")
	s.TunnelName, _ = cmd.Flags().GetString(flagTunnelName)
	s.TunnelHostname, _ = cmd.Flags().GetString(flagTunnelHostname)
	return s
}

func mergeUpSettings(cur upSettings, changed func(string) bool, saved upSettings) (upSettings, string) {
	cur.AtLogin, cur.AtLoginAsked = saved.AtLogin, saved.AtLoginAsked
	var reused []string
	pick := func(flag string, cur *string, saved string) {
		if changed(flag) || saved == "" {
			return
		}
		*cur = saved
		reused = append(reused, "--"+flag+" "+saved)
	}
	pick("http", &cur.HTTP, saved.HTTP)
	pick("provider", &cur.Provider, saved.Provider)
	pick(flagTunnelName, &cur.TunnelName, saved.TunnelName)
	pick(flagTunnelHostname, &cur.TunnelHostname, saved.TunnelHostname)
	return cur, strings.Join(reused, " ")
}

func loadUpSettings(dir string) upSettings {
	var s upSettings
	data, err := os.ReadFile(filepath.Join(dir, upSettingsName))
	if err != nil || json.Unmarshal(data, &s) != nil {
		return upSettings{}
	}
	if s.HTTP == defaultMCPAddr {
		s.HTTP = ""
	}
	return s
}

func upSettingsExist(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, upSettingsName))
	return err == nil && !info.IsDir()
}

func saveUpSettings(dir string, s upSettings) error {
	if s.HTTP == defaultMCPAddr {
		s.HTTP = ""
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, upSettingsName), append(data, '\n'), 0o600)
}

func tunnelPreflight(provider, name, host string) error {
	if provider == "" {
		provider = "cloudflared"
	}
	p, ok := tunnel.Providers[provider]
	if !ok {
		names := tunnel.Names()
		sort.Strings(names)
		return fmt.Errorf("unknown tunnel provider %q. Available: %s", provider, strings.Join(names, ", "))
	}
	if name == "" && host == "" {
		return p.PreflightAuth()
	}
	return p.PreflightNamedAuth(tunnel.NamedConfig{Name: name, Hostname: host})
}

func tunnelArgs(provider, name, host string) ([]string, error) {
	var args []string
	if provider != "" {
		args = append(args, "--tunnel-provider", provider)
	}
	if name == "" && host == "" {
		return args, nil
	}
	if host == "" {
		return nil, fmt.Errorf("--tunnel-name %s needs --tunnel-hostname <host>: the DNS name you routed to it "+
			"(cloudflared tunnel route dns %s corgi.yourdomain.com)", name, name)
	}
	if name != "" {
		args = append(args, "--tunnel-name", name)
	}
	return append(args, "--tunnel-hostname", host), nil
}

func spawnDetachedMCP(dir, addr string, tunnel []string) error {
	args := append([]string{"mcp", "--http", addr, "--tunnel", "--pair"}, tunnel...)
	_ = os.Remove(filepath.Join(dir, mcpLogName))
	pid, err := spawnDetached(dir, mcpLogName, args...)
	if err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(dir, mcpPidName), []byte(strconv.Itoa(pid)+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, mcpAddrName), []byte(addr+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, mcpVersionName), []byte(APP_VERSION+"\n"), 0o600)
	return nil
}

func spawnDetached(dir, logName string, args ...string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(filepath.Join(dir, logName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	defer logFile.Close()
	c := exec.Command(exe, args...)
	c.Stdout = logFile
	c.Stderr = logFile
	c.Stdin = nil
	utils.SetProcessGroup(c)
	if err := c.Start(); err != nil {
		return 0, err
	}
	return c.Process.Pid, nil
}

func acquireUpLock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "agent-up.lock")
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if upLockIsStale(path) {
			_ = os.Remove(path)
			continue
		}
		return nil, fmt.Errorf("another `corgi agent up` is already running — wait for it, or remove %s if it crashed", path)
	}
	return nil, fmt.Errorf("could not take the agent-up lock at %s", path)
}

func upLockIsStale(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	return proc.Signal(syscall.Signal(0)) != nil
}

func corgiListenerPIDs(addr string) []int {
	if runtime.GOOS == "windows" {
		return nil
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return nil
	}
	// Exact name only: corgit or my-corgi-tool must never be killed.
	wanted := map[string]bool{"corgi": true}
	if exe, err := os.Executable(); err == nil {
		wanted[filepath.Base(exe)] = true
	}
	var pids []int
	for _, pid := range utils.ListenerPIDs(portNum) {
		if pid == os.Getpid() {
			continue
		}
		comm, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			continue
		}
		if wanted[filepath.Base(strings.TrimSpace(string(comm)))] {
			pids = append(pids, pid)
		}
	}
	return pids
}

func reclaimCorgiMCP(addr string) (found, freed bool) {
	pids := corgiListenerPIDs(addr)
	if len(pids) == 0 {
		return false, false
	}
	for _, pid := range pids {
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Signal(syscall.SIGTERM)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !mcpListening(addr) {
			return true, true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return true, !mcpListening(addr)
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
	mcpPairCodePattern  = regexp.MustCompile(`pairing code: (\S+)\n`)
	mcpFatalPattern     = regexp.MustCompile(`(?m)^(mcp server error:|corgi mcp --pair cannot|could not start pairing:|tunnel: )`)
	mcpTunnelErrPattern = regexp.MustCompile(`(?m)^🌐 ✗ tunnel: (.+)$`)
)

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
			if m := mcpTunnelErrPattern.FindStringSubmatch(log); m != nil {
				return last, fmt.Errorf("tunnel: %s", strings.TrimSpace(m[1]))
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
		return last, nil
	}
	return last, fmt.Errorf("no pairing code within %s", timeout)
}

func printAgentUp(res agentUpResult) {
	res.QuickTunnel = res.PublicURL != "" && res.TunnelHostname == ""
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
	if res.AtLogin {
		fmt.Printf("  ✓ starts at login (%s) — survives a reboot\n", installMechanism())
	}
	switch {
	case res.PublicURL != "":
		fmt.Printf("  ✓ public endpoint: %s/mcp\n", res.PublicURL)
	default:
		fmt.Printf("  ✓ local endpoint: http://%s/mcp (no tunnel yet — see %s)\n", res.MCPAddr, res.LogPath)
	}
	printAgentUpPairing(res)
	if res.Hint != "" {
		fmt.Printf("  %s\n", res.Hint)
	}
	fmt.Println()
	if risk := supervisor.CheckSleepRisk(); risk.AtRisk() {
		fmt.Printf("  ⚠ %s\n    fix: %s\n", risk.Reason, risk.Fix)
	}
	if lan := lanLauncherURL(res.MCPAddr, res.PairCode); lan != "" {
		fmt.Println()
		fmt.Print(lan)
	}
	if res.PublicURL != "" {
		fmt.Printf("  after scanning, the phone opens the launcher — tap a repo to start:\n    %s/app\n", res.PublicURL)
		if hint := quickTunnelWarning(res); hint != "" {
			fmt.Println()
			fmt.Print(hint)
		}
		if hint := sharedTunnelHint(res.PublicURL); hint != "" {
			fmt.Println()
			fmt.Print(hint)
		}
	}
	fmt.Println("  or from any MCP client: corgi_session_start {\"workspace\":\"" + orDefault(res.Workspace, "<name>") + "\"}")
}

func printAgentUpPairing(res agentUpResult) {
	if res.PairCode == "" {
		return
	}
	fmt.Println()
	if res.PairURL != "" {
		fmt.Println("  📱 scan to pair (single use, 10 minutes):")
		fmt.Println()
		printTerminalQR(res.PairURL)
		fmt.Printf("    or open: %s\n", res.PairURL)
		return
	}
	fmt.Println("  pair a device (single use, 10 minutes):")
	fmt.Printf("    code: %s — POST http://%s/pair {\"code\":\"%s\",\"device\":\"my-phone\"}\n",
		res.PairCode, res.MCPAddr, res.PairCode)
}

func lanLauncherURL(addr, code string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || (host != "0.0.0.0" && host != "::" && host != "") {
		return ""
	}
	ip := outboundIP()
	if ip == "" {
		return ""
	}
	base := "http://" + net.JoinHostPort(ip, port)
	out := "  🏠 on the same Wi-Fi, skip the tunnel entirely:\n"
	if code != "" {
		out += "    pair:     " + base + "/pair#" + code + "\n"
	}
	return out + "    launcher: " + base + "/app\n"
}

func outboundIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	return lanAddressOf(ifaces)
}

func lanAddressOf(ifaces []net.Interface) string {
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 ||
			iface.Flags&net.FlagLoopback != 0 ||
			iface.Flags&net.FlagPointToPoint != 0 ||
			strings.HasPrefix(iface.Name, "docker") ||
			strings.HasPrefix(iface.Name, "br-") ||
			strings.HasPrefix(iface.Name, "utun") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.IsLinkLocalUnicast() {
				continue
			}
			if v4 := ipnet.IP.To4(); v4 != nil && v4.IsPrivate() {
				return v4.String()
			}
		}
	}
	return ""
}

func quickTunnelWarning(res agentUpResult) string {
	if res.PublicURL == "" || res.TunnelHostname != "" {
		return ""
	}
	return "  \u26a0 this address is a quick tunnel: it changes every time the tunnel restarts\n" +
		"    (a reboot, corgi agent restart). A phone paired to it loses this laptop then,\n" +
		"    and needs the new QR. For an address that never changes:\n" +
		"      corgi agent tunnel setup corgi.yourdomain.com      (cloudflared, your DNS; one-time)\n" +
		"      corgi agent up --provider ngrok --tunnel-hostname <yours>.ngrok-free.dev   (ngrok's free dev domain)\n" +
		"    docs/agent.md#a-launcher-url-that-never-changes\n"
}

func sharedTunnelHint(publicURL string) string {
	shared := []string{".trycloudflare.com", ".loca.lt", ".ngrok-free.app", ".ngrok-free.dev", ".ngrok.io"}
	matched := false
	for _, d := range shared {
		if strings.Contains(publicURL, d) {
			matched = true
			break
		}
	}
	if !matched {
		return ""
	}
	return "  \u26a0 if the page never loads on the phone:\n" +
		"    \u2022 open the link in the real browser, not an app's built-in one\n" +
		"      (in Safari's in-app view, tap the compass icon) \u2014 scanning the QR\n" +
		"      with the camera already does this\n" +
		"    \u2022 this is a free shared tunnel domain; some carriers and filtering\n" +
		"      DNS refuse to resolve these, so it can work on Wi-Fi and not on\n" +
		"      cellular. A hostname you own is on no blocklist:\n" +
		"          corgi agent tunnel setup corgi.yourdomain.com\n"
}

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

var agentDownCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop everything `corgi agent up` started — the daemon and the detached MCP + tunnel",
	Long: `The mirror of ` + "`corgi agent up`" + `. Stops the agent daemon and the detached
MCP server that serves the launcher and pairing over the tunnel, so the public
URL goes down too. (` + "`corgi agent stop`" + ` stops only the daemon.)`,
	Run: runAgentDown,
}

func runAgentDown(_ *cobra.Command, _ []string) {
	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}
	stopped := false

	if info, rerr := daemon.ReadInfo(dir); rerr == nil && info != nil {
		if proc, ferr := os.FindProcess(info.PID); ferr == nil && proc.Signal(syscall.SIGTERM) == nil {
			if !waitForDaemonExit(dir, 10*time.Second) {
				killDaemon(proc)
			}
			utils.Infof("stopped agent daemon (pid %d)\n", info.PID)
			stopped = true
		}
	}
	if stopStrayServers() > 0 {
		stopped = true
	}

	pidPath := filepath.Join(dir, mcpPidName)
	mcpStopped := false
	if pid, ok := readAgentPidFile(pidPath); ok {
		if utils.PidAlive(pid, "") {
			if proc, ferr := os.FindProcess(pid); ferr == nil && proc.Signal(syscall.SIGTERM) == nil {
				utils.Infof("stopped MCP + tunnel (pid %d)\n", pid)
				stopped, mcpStopped = true, true
			}
		}
		_ = os.Remove(pidPath)
	}
	if !mcpStopped {
		fallbackAddr := defaultMCPAddr
		if data, rerr := os.ReadFile(filepath.Join(dir, mcpAddrName)); rerr == nil {
			if a := strings.TrimSpace(string(data)); a != "" {
				fallbackAddr = a
			}
		}
		for _, pid := range corgiListenerPIDs(fallbackAddr) {
			if proc, ferr := os.FindProcess(pid); ferr == nil && proc.Signal(syscall.SIGTERM) == nil {
				utils.Infof("stopped MCP + tunnel on %s (pid %d)\n", fallbackAddr, pid)
				stopped = true
			}
		}
	}
	_ = os.Remove(filepath.Join(dir, mcpAddrName))
	_ = os.Remove(filepath.Join(dir, "agent-up.lock"))

	if !stopped {
		utils.Info("corgi agent is not running")
	}
}

var agentRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "corgi agent down && corgi agent up --fresh, in one command",
	Long: `Stops everything ` + "`corgi agent up`" + ` started, then brings it back up with
--fresh: a new tunnel and a new single-use pairing window. The one command to
run after upgrading corgi so the daemon and launcher are the new binary.`,
	Run: runAgentRestart,
}

var (
	restartDown = runAgentDown
	restartUp   = runAgentUp
)

func runAgentRestart(cmd *cobra.Command, args []string) {
	restartDown(cmd, args)
	_ = cmd.Flags().Set("fresh", "true")
	restartUp(cmd, args)
}

func readAgentPidFile(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func addAgentUpFlags(c *cobra.Command) {
	c.Flags().String("http", defaultMCPAddr, "Local MCP address. Use 0.0.0.0:8765 to also serve phones on the same Wi-Fi, which needs no tunnel at all")
	c.Flags().String("provider", "", "Tunnel provider (cloudflared|ngrok|localtunnel)")
	c.Flags().String(flagTunnelName, "", "cloudflared named-tunnel name — a stable public URL you can bookmark, and a phone that stays paired (needs a one-time `cloudflared tunnel create` and --tunnel-hostname; see docs/agent.md)")
	c.Flags().String(flagTunnelHostname, "", "Public hostname of the named tunnel, e.g. corgi.yourdomain.com (the DNS name routed to it; ngrok: your free static domain). Remembered for the next up/restart; pass \"\" to go back to a quick tunnel")
	c.Flags().Bool(atLoginFlag, false, "Also start corgi agent at login, so the daemon, this endpoint and this tunnel come back after a reboot (--at-login=false turns it off again)")
	c.Flags().Bool("fresh", false, "Replace a corgi MCP already holding the port: new tunnel + a new single-use pairing window (a phone mid-session on the old URL is cut)")
	c.Flags().String("pair-code", "", "Open the first pairing window on this code instead of a random one — minted earlier with `corgi agent pair --mint`, so a phone prepared ahead of time can pair a headless daemon nobody types on")
	c.Flags().Duration("pair-ttl", 0, "How long the --pair-code window stays open (default 10m, at most 24h) — room for a slow boot")
	c.Flags().Bool("viewer", false, "The pairing window this opens hands out a read-only token: a teammate's phone sees the board, the inbox and the brief, never a transcript, never a button (with --fresh to reopen a window)")
}

func init() {
	addAgentUpFlags(agentUpCmd)
	addAgentUpFlags(agentRestartCmd)
	agentCmd.AddCommand(agentUpCmd, agentRestartCmd)
}

func announceNewAddress(dir, url string) {
	host, _ := os.Hostname()
	store := push.Load(dir)
	msg := push.Message{
		Title:    "corgi agent · " + host,
		Body:     "back on a new address — the app relinks itself",
		Category: "relink",
		Data:     map[string]string{"relink": "1", "url": url, "daemon": host},
		Thread:   "relink",
	}
	if err := store.Send(context.Background(), msg); err != nil {
		utils.Infof("agent: could not tell the phones the new address: %v\n", err)
		return
	}
	fmt.Printf("  ✓ paired phones told the new address (they relink on their own)\n")
}
