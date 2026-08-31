package cmd

import (
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

// defaultMCPAddr is where `corgi agent up` serves MCP when no --http is given,
// so nobody has to remember a port.
const defaultMCPAddr = "127.0.0.1:8765"

// mcpLogName is the detached MCP server's log file, under the agent data dir.
const mcpLogName = "mcp.log"

// mcpPidName records the detached MCP server's pid so `corgi agent down` can
// stop the tunnel + pairing server that `agent up` started, not just the daemon.
const mcpPidName = "mcp.pid"

// mcpAddrName records the address that MCP listens on, so `agent down`'s
// no-pid-file fallback can look at the right port even after --http.
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
	fresh, _ := cmd.Flags().GetBool("fresh")

	dir, err := agentDir()
	if err != nil {
		exitWithError("agent_data_dir", err, 1)
	}

	// Flags given now win; anything not given falls back to what the last
	// `up` used, so `agent restart` keeps the named tunnel without retyping it.
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
	if err != nil {
		exitWithError("agent_up_tunnel", err, 2)
	}
	if err := tunnelPreflight(settings.Provider, settings.TunnelName, settings.TunnelHostname); err != nil {
		exitWithError("agent_up_tunnel", err, 2)
	}

	// One `agent up` at a time. Two racing invocations both pass the listening
	// check, both truncate mcp.log, and one ends up an orphaned server logging
	// to an unlinked file while both report failure. The lock makes the loser
	// wait its turn instead.
	release, err := acquireUpLock(dir)
	if err != nil {
		exitWithError("agent_up_locked", err, 1)
	}
	defer release()

	var res agentUpResult
	res.MCPAddr = addr

	res.Workspace, res.Registered = registerCwdWorkspace()
	if res.Workspace != "" {
		// Same trust pre-check init does: an untrusted folder produces a phone
		// card that can only ever fail, so say it now, while the fix is one
		// `claude` run away in the terminal the user is already in.
		if absPath, cfgDir, ok := workspaceSessionTarget(res.Workspace); ok {
			warnIfUntrusted(cfgDir, absPath)
		}
	}

	info, err := ensureDaemon(dir)
	if err != nil {
		exitWithError("agent_up_daemon", err, 1)
	}
	res.DaemonPID = info.PID

	res.LogPath = filepath.Join(dir, mcpLogName)
	if mcpListening(addr) {
		// Something already holds the port. Re-running `up` must be a safe,
		// idempotent ensure step — a phone may be mid-session on the running
		// server, so it is only ever replaced when --fresh says so.
		if !fresh {
			if pid, ok := readAgentPidFile(filepath.Join(dir, mcpPidName)); ok && utils.PidAlive(pid, "") {
				// Ours and healthy: report it as up, with the URL its log recorded.
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

	if err := spawnDetachedMCP(dir, addr, tunnel); err != nil {
		exitWithError("agent_up_mcp", err, 1)
	}
	res.MCPStarted = true

	parsed, err := awaitMCPLog(res.LogPath, 90*time.Second)
	if err != nil {
		exitWithError("agent_up_mcp", fmt.Errorf("%w — see %s", err, res.LogPath), 1)
	}
	_ = saveUpSettings(dir, settings)
	res.PublicURL = parsed.publicURL
	res.PairCode = parsed.pairCode
	if res.PublicURL != "" && res.PairCode != "" {
		// The code rides in the fragment: it never reaches the server or its
		// logs, only the pair page's own JS.
		res.PairURL = res.PublicURL + "/pair#" + res.PairCode
	}

	printAgentUp(res)
}

// registerCwdWorkspace puts the current workspace — a corgi stack or a plain
// git repository — in the registry, like one step of `corgi agent scan`.
// Registration only — autostart stays opt-in, and remote start is the point of
// this command anyway.
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
		// The basename is already taken by a DIFFERENT directory. Repointing it
		// would hijack that workspace; disambiguate with the parent instead so
		// two repos both called "api" can coexist.
		id = filepath.Base(filepath.Dir(cwd)) + "-" + id
		if existing2, ok := registry.Find(id); ok {
			if existing2.AbsPath == cwd {
				return id, false
			}
			// Both levels collide with other directories. Refusing beats
			// repointing: the id keys trusted settings (account, permissions),
			// which must never silently transfer to a new directory.
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
	return id, true
}

// ensureDaemon returns the running daemon's record, starting one detached when
// none is up.
func ensureDaemon(dir string) (*daemon.Info, error) {
	if info, err := daemon.ReadInfo(dir); err == nil && info != nil {
		return info, nil
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

// upSettings is what the last successful `agent up` ran with, kept so a bare
// `agent up` / `agent restart` repeats it instead of silently dropping the
// named tunnel — and with it the stable URL the phone is paired to.
type upSettings struct {
	HTTP           string `json:"http,omitempty"`
	Provider       string `json:"provider,omitempty"`
	TunnelName     string `json:"tunnelName,omitempty"`
	TunnelHostname string `json:"tunnelHostname,omitempty"`
}

const upSettingsName = "up.json"

func upSettingsFromFlags(cmd *cobra.Command) upSettings {
	var s upSettings
	s.HTTP, _ = cmd.Flags().GetString("http")
	s.Provider, _ = cmd.Flags().GetString("provider")
	s.TunnelName, _ = cmd.Flags().GetString("tunnel-name")
	s.TunnelHostname, _ = cmd.Flags().GetString("tunnel-hostname")
	return s
}

// mergeUpSettings fills every flag the user did not pass from the saved run.
// An explicitly passed flag always wins, including an explicit empty value —
// `--tunnel-hostname ""` is how you go back to a quick tunnel. The returned
// string names what was reused, for the notice.
func mergeUpSettings(cur upSettings, changed func(string) bool, saved upSettings) (upSettings, string) {
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
	pick("tunnel-name", &cur.TunnelName, saved.TunnelName)
	pick("tunnel-hostname", &cur.TunnelHostname, saved.TunnelHostname)
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

// tunnelPreflight runs the provider's own auth check before anything is
// spawned, so a missing ngrok token or cloudflared login fails here with the
// provider's instructions instead of as a 90-second wait on a log.
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

// tunnelArgs turns the up flags into `corgi mcp` tunnel flags. A named tunnel
// needs its hostname: cloudflared never reports one, and without it the
// printed launcher URL would be "https:///app".
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
	// Truncate the old log first: awaitMCPLog must not read a previous run's
	// URL or pairing code as this one's.
	_ = os.Remove(filepath.Join(dir, mcpLogName))
	pid, err := spawnDetached(dir, mcpLogName, args...)
	if err != nil {
		return err
	}
	// Best-effort: a missing pid file only means `agent down` cannot stop this
	// MCP for you, not that anything is wrong with the running server.
	_ = os.WriteFile(filepath.Join(dir, mcpPidName), []byte(strconv.Itoa(pid)+"\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, mcpAddrName), []byte(addr+"\n"), 0o600)
	return nil
}

// spawnDetached starts corgi itself with args, output to <dir>/<logName>, in
// its own process group so it outlives this command and later Ctrl+Cs. Returns
// the child pid so the caller can record it for a later stop.
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

// acquireUpLock takes an exclusive, pid-stamped lock so only one `agent up`
// runs at a time. A lock left by a crashed run whose pid is gone is reclaimed
// rather than blocking forever.
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

// upLockIsStale reports whether the lock's owning process is gone.
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

// corgiListenerPIDs returns the pids of corgi processes listening on addr's
// port — the recovery path for an MCP whose pid file was lost, which would
// otherwise hold the port against every `up`. Only pids whose command name
// contains "corgi". Unix-only (lsof); elsewhere callers fall back to a hint.
func corgiListenerPIDs(addr string) []int {
	if runtime.GOOS == "windows" {
		return nil
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	out, err := exec.Command("lsof", "-ti", "tcp:"+port, "-sTCP:LISTEN").Output()
	if err != nil {
		return nil
	}
	// Exact-name match, not Contains: a neighbour binary that merely embeds the
	// word (corgit, my-corgi-tool) must never be killed. macOS ps prints the
	// full path, Linux the bare (possibly truncated) name — Base handles both,
	// and corgi's own name fits untruncated. A differently-named dev build of
	// corgi is matched via this process's own executable name.
	wanted := map[string]bool{"corgi": true}
	if exe, err := os.Executable(); err == nil {
		wanted[filepath.Base(exe)] = true
	}
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 || pid == os.Getpid() {
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

// reclaimCorgiMCP stops the corgi MCP(s) holding addr and waits for the port to
// free. found says a corgi listener was there at all; freed says the port is
// now available — the split keeps the caller's message honest (a corgi server
// that ignored SIGTERM is not "something that is not corgi").
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
	// Anchored to the newline so a code read mid-write ("pairing code: WOR"
	// before "D-123\n" lands) is not captured truncated and QR-encoded wrong.
	// The URL pattern needs no such guard: its /mcp suffix is the terminator.
	mcpPairCodePattern  = regexp.MustCompile(`pairing code: (\S+)\n`)
	mcpFatalPattern     = regexp.MustCompile(`(?m)^(mcp server error:|corgi mcp --pair cannot|could not start pairing:|tunnel: )`)
	mcpTunnelErrPattern = regexp.MustCompile(`(?m)^🌐 ✗ tunnel: (.+)$`)
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
		// The endpoint is pairable on the LAN even when no tunnel appeared —
		// usually a missing cloudflared. Report what works instead of failing.
		return last, nil
	}
	return last, fmt.Errorf("no pairing code within %s", timeout)
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
			fmt.Println("  📱 scan to pair (single use, 10 minutes):")
			fmt.Println()
			printTerminalQR(res.PairURL)
			fmt.Printf("    or open: %s\n", res.PairURL)
		} else {
			fmt.Println("  pair a device (single use, 10 minutes):")
			fmt.Printf("    code: %s — POST http://%s/pair {\"code\":\"%s\",\"device\":\"my-phone\"}\n",
				res.PairCode, res.MCPAddr, res.PairCode)
		}
	}
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
		if hint := sharedTunnelHint(res.PublicURL); hint != "" {
			fmt.Println()
			fmt.Print(hint)
		}
	}
	fmt.Println("  or from any MCP client: corgi_session_start {\"workspace\":\"" + orDefault(res.Workspace, "<name>") + "\"}")
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

// A phone reaches this machine over the real network, so a docker bridge or a
// VPN tunnel is the wrong answer even though both carry a private address.
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
			// Wait for the exit, or `down && up` races: up reads the dying
			// daemon as running and starts nothing.
			_ = waitForDaemonExit(dir, 10*time.Second)
			utils.Infof("stopped agent daemon (pid %d)\n", info.PID)
			stopped = true
		}
	}

	// The detached MCP + tunnel `agent up` recorded; stopping it is what takes
	// the public URL down. mcp.pid outlives its process on a crash or reboot, so
	// PidAlive checks the pid is still its own group leader — a stale file must
	// not make `down` kill an unrelated process.
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
	// Fallback ONLY when the pid file stopped nothing — an MCP from an older
	// corgi, or a lost file: a corgi process still listening on the recorded
	// (or default) MCP port is ours to stop. Leaving it is exactly the stuck
	// loop where every `agent up` refuses the busy port and pairing never
	// reopens. Guarded so a just-SIGTERMed server still draining its listener
	// is not signalled twice and reported as two servers.
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

// readAgentPidFile reads a pid written by spawnDetached. A missing or malformed
// file just means there is nothing to stop.
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
	c.Flags().String("tunnel-name", "", "cloudflared named-tunnel name — a stable public URL you can bookmark, and a phone that stays paired (needs a one-time `cloudflared tunnel create` and --tunnel-hostname; see docs/agent.md)")
	c.Flags().String("tunnel-hostname", "", "Public hostname of the named tunnel, e.g. corgi.yourdomain.com (the DNS name routed to it; ngrok: your free static domain). Remembered for the next up/restart; pass \"\" to go back to a quick tunnel")
	c.Flags().Bool("fresh", false, "Replace a corgi MCP already holding the port: new tunnel + a new single-use pairing window (a phone mid-session on the old URL is cut)")
}

func init() {
	addAgentUpFlags(agentUpCmd)
	addAgentUpFlags(agentRestartCmd)
	agentCmd.AddCommand(agentUpCmd, agentRestartCmd)
}
