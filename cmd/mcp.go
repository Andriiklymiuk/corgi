package cmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/agent/pairing"
	"andriiklymiuk/corgi/utils/tunnel"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run corgi as an MCP server over stdio (for AI agent clients)",
	Long: `Starts a Model Context Protocol server over stdio. AI agent clients spawn
this as a subprocess and call corgi's commands as structured tools.

Register it in .mcp.json (project) or ~/.claude.json:
  { "mcpServers": { "corgi": { "command": "corgi", "args": ["mcp"] } } }`,
	Run: runMCP,
}

func init() {
	mcpCmd.Flags().String("http", "", "Serve MCP over Streamable HTTP at this address (e.g. :8765 or 127.0.0.1:8765) instead of stdio.")
	mcpCmd.Flags().Bool("tunnel", false, "Open a public tunnel to the --http addr (requires --http).")
	mcpCmd.Flags().String("tunnel-provider", "cloudflared", "Tunnel provider (cloudflared|ngrok|localtunnel).")
	mcpCmd.Flags().String("tunnel-hostname", "", "Custom public hostname for the tunnel (${VAR} expanded).")
	mcpCmd.Flags().String("tunnel-name", "", "cloudflared named-tunnel name.")
	mcpCmd.Flags().String("token", "", "Bearer token for HTTP auth (auto-generated when --tunnel is set).")
	mcpCmd.Flags().Bool("insecure", false, "Disable bearer-token auth on the HTTP endpoint.")
	mcpCmd.Flags().Bool("pair", false, "Open a single-use pairing window so a device can claim its own revocable token (requires --http).")
	mcpCmd.Flags().Bool("viewer", false, "The device that pairs in this window only reads: the board, the inbox, the brief — never a transcript, never a button (with --pair).")
	mcpCmd.Flags().Bool("bind-all", false, "Let --http listen on a non-loopback address (0.0.0.0, a LAN IP). Off, a bare port binds 127.0.0.1.")
	mcpCmd.Flags().Bool("no-oauth", false, "Do not serve the OAuth sign-in (Connect button) beside /mcp; the Authorization header path still works.")
	mcpCmd.Flags().StringSlice("oauth-client-host", nil, "Extra host whose https redirect URIs OAuth clients may use (claude.ai and claude.com are built in). Also CORGI_MCP_OAUTH_CLIENT_HOSTS, comma-separated.")
	mcpCmd.Flags().StringSlice("allow-origin", nil, "Extra browser Origin allowed on /mcp (scheme://host[:port]); loopback origins and the tunnel's are always allowed. Also CORGI_MCP_ALLOWED_ORIGINS, comma-separated.")
	rootCmd.AddCommand(mcpCmd)
}

const (
	errFmt            = "%s: %v"
	mimeJSON          = "application/json; charset=utf-8"
	headerContentType = "Content-Type"
)

var mcpHandlerMu sync.Mutex

var mcpPublicTunnelActive atomic.Bool

var mcpTunnelPrivate atomic.Bool

const dangerousToolBlockedMsg = "corgi_exec/corgi_db_query are disabled over a public tunnel; put the tunnel behind an identity proxy, or set CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1 to allow"

func mcpExposure() tunnel.Exposure {
	if !mcpPublicTunnelActive.Load() {
		return tunnel.ExposureLocal
	}
	if mcpTunnelPrivate.Load() {
		return tunnel.ExposurePrivate
	}
	return tunnel.ExposurePublic
}

func dangerousTunnelToolsAllowed(publicTunnel bool) bool {
	if !publicTunnel {
		return true
	}
	if mcpTunnelPrivate.Load() {
		return true
	}
	return os.Getenv("CORGI_MCP_ALLOW_DANGEROUS_TUNNEL") == "1"
}

func runMCP(cmd *cobra.Command, _ []string) {
	utils.NonInteractive = true
	utils.JSONOutput = true

	warnStrandedAgentData()

	s := newCorgiMCPServer(APP_VERSION)

	httpAddr, _ := cmd.Flags().GetString("http")
	opts := mcpHTTPOptsFromFlags(cmd)
	if opts.pair && httpAddr == "" {
		fmt.Fprintln(os.Stderr, "corgi mcp --pair requires --http (the addr a device connects to).")
		exitProcess(2)
	}
	if opts.tunnel && httpAddr == "" {
		fmt.Fprintln(os.Stderr, "corgi mcp --tunnel requires --http (the local addr to expose).")
		exitProcess(2)
	}
	if httpAddr != "" {
		listen, notice, err := resolveMCPListenAddr(httpAddr, opts.bindAll)
		if err != nil {
			fmt.Fprintln(os.Stderr, "corgi mcp:", err)
			exitProcess(2)
		}
		if notice != "" {
			fmt.Fprintln(os.Stderr, notice)
		}
		serveMCPHTTP(s, listen, resolveMCPToken(opts), opts)
		return
	}
	serveMCPStdio(s)
}

func newCorgiMCPServer(version string) *server.MCPServer {
	s := server.NewMCPServer("corgi", version,
		server.WithTitle("Corgi"),
		server.WithInstructions(mcpServerInstructions),
		server.WithToolHandlerMiddleware(mcpResultGuard(mcpMaxEmittedChars())),
	)
	registerMCPTools(s)
	registerMCPResources(s)
	return s
}

type mcpHTTPOpts struct {
	tunnel         bool
	tunnelProvider string
	tunnelHostname string
	tunnelName     string
	token          string
	insecure       bool
	pair           bool
	viewer         bool
	bindAll        bool
	allowOrigins   []string
	noOAuth        bool
	oauthHosts     []string
}

func mcpHTTPOptsFromFlags(cmd *cobra.Command) mcpHTTPOpts {
	o := mcpHTTPOpts{}
	o.tunnel, _ = cmd.Flags().GetBool("tunnel")
	o.tunnelProvider, _ = cmd.Flags().GetString("tunnel-provider")
	o.tunnelHostname, _ = cmd.Flags().GetString("tunnel-hostname")
	o.tunnelName, _ = cmd.Flags().GetString("tunnel-name")
	o.token, _ = cmd.Flags().GetString("token")
	o.insecure, _ = cmd.Flags().GetBool("insecure")
	o.pair, _ = cmd.Flags().GetBool("pair")
	o.viewer, _ = cmd.Flags().GetBool("viewer")
	o.bindAll, _ = cmd.Flags().GetBool("bind-all")
	o.allowOrigins, _ = cmd.Flags().GetStringSlice("allow-origin")
	o.noOAuth, _ = cmd.Flags().GetBool("no-oauth")
	o.oauthHosts, _ = cmd.Flags().GetStringSlice("oauth-client-host")
	if env := strings.TrimSpace(os.Getenv("CORGI_MCP_ALLOWED_ORIGINS")); env != "" {
		for _, origin := range strings.Split(env, ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				o.allowOrigins = append(o.allowOrigins, origin)
			}
		}
	}
	return o
}

func resolveMCPToken(o mcpHTTPOpts) string {
	if o.insecure {
		return ""
	}
	token := o.token
	if token == "" && o.tunnel {
		token = generateMCPToken()
	}
	return token
}

func generateMCPToken() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintln(os.Stderr, "could not generate token:", err)
		exitProcess(1)
	}
	return "corgi_mcp_" + base64.RawURLEncoding.EncodeToString(b)
}

const bearerPrefix = "Bearer "

func bearerAuth(token string, next http.Handler, deviceStorePath string) http.Handler {
	return bearerAuthWithOAuth(token, next, deviceStorePath, nil)
}

func bearerAuthWithOAuth(token string, next http.Handler, deviceStorePath string, oa *oauthServer) http.Handler {
	if token == "" && deviceStorePath == "" {
		return next
	}
	want := []byte(bearerPrefix + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if token != "" && subtle.ConstantTimeCompare(got, want) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		if d, ok := authorizedDeviceFull(deviceStorePath, r.Header.Get("Authorization")); ok && !d.Viewer() && !d.Encrypted() {
			next.ServeHTTP(w, r)
			return
		}
		if oa != nil && oa.authorizeAccessToken(r.Header.Get("Authorization")) {
			next.ServeHTTP(w, r)
			return
		}
		challenge := `Bearer realm="corgi"`
		if oa != nil {
			challenge += `, resource_metadata="` + oa.resourceMetadataURL() + `"`
		}
		if r.Header.Get("Authorization") != "" {
			challenge += `, error="invalid_token"`
		}
		w.Header().Set("WWW-Authenticate", challenge)
		w.Header().Set("Content-Type", mimeJSON)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})
}

func authorizedDevice(storePath, header string) (string, bool) {
	if storePath == "" {
		return "", false
	}
	offered, ok := strings.CutPrefix(header, bearerPrefix)
	if !ok || !strings.HasPrefix(offered, pairing.TokenPrefix) {
		return "", false
	}
	store, err := pairing.Load(storePath)
	if err != nil {
		return "", false
	}
	return store.Authorize(offered)
}

func buildMCPTunnelConfig(provider, hostname, name string) (tunnel.Provider, *tunnel.NamedConfig, error) {
	p, ok := tunnel.Providers[provider]
	if !ok {
		names := tunnel.Names()
		sort.Strings(names)
		return nil, nil, fmt.Errorf("unknown tunnel provider %q. Available: %s", provider, strings.Join(names, ", "))
	}
	var missing []string
	host := tunnel.Substitute(hostname, nil, &missing)
	if len(missing) > 0 {
		return nil, nil, tunnel.MissingError("--tunnel-hostname", missing)
	}
	if host == "" && name == "" {
		return p, nil, nil
	}
	return p, &tunnel.NamedConfig{Hostname: host, Name: name}, nil
}

func serveMCPStdio(s *server.MCPServer) {
	if err := server.ServeStdio(s, server.WithWorkerPoolSize(1)); err != nil {
		fmt.Fprintln(os.Stderr, "mcp server error:", err)
		exitProcess(1)
	}
}

func resolveDeviceStore(opts mcpHTTPOpts) string {
	if opts.insecure {
		return ""
	}
	dir, err := agentDir()
	if err != nil {
		return ""
	}
	path := pairing.StorePath(dir)
	switch pairing.InspectStore(path) {
	case pairing.StoreHasDevices:
		return path
	case pairing.StoreUnreadable:
		fmt.Fprintf(os.Stderr,
			"corgi mcp: cannot read the paired-device store at %s.\n"+
				"Fix its permissions (chmod 600) or remove it to start over; refusing to serve in the meantime.\n", path)
		exitProcess(1)
	case pairing.StoreEmpty:
		if opts.pair {
			return path
		}
	}
	return ""
}

func announceMCPAuth(token, deviceStore string) {
	switch {
	case token == "" && deviceStore != "":
		fmt.Fprintln(os.Stderr, "corgi mcp --http requires a paired device token (see `corgi mcp devices`).")
		fmt.Fprintln(os.Stderr, "Tokenless clients will be rejected; pass --token to keep one working, or --insecure for no auth.")
	case token == "":
		fmt.Fprintln(os.Stderr, "⚠️  corgi mcp --http has no auth; bind to localhost or put it behind an authenticated proxy.")
	default:
		fmt.Fprintf(os.Stderr, "corgi mcp bearer token: %s\n", token)
	}
}

func serveMCPHTTP(s *server.MCPServer, addr, token string, opts mcpHTTPOpts) {
	httpSrv := server.NewStreamableHTTPServer(s)

	deviceStore := resolveDeviceStore(opts)

	mux := http.NewServeMux()
	origins := newOriginAllowlist(addr, opts.allowOrigins)

	var oa *oauthServer
	if !opts.noOAuth && (token != "" || deviceStore != "") {
		if dir, err := agentDir(); err == nil {
			oauthDevices := deviceStore
			if oauthDevices == "" {
				oauthDevices = pairing.StorePath(dir)
			}
			oa = newOAuthServer(addr, newOAuthClientHosts(opts.oauthHosts, os.Stderr), origins, oauthDevices, oauthStatePath(dir))
			oa.mount(mux)
		}
	}
	mux.Handle("/mcp", mcpEndpointGuard(origins, bearerAuthWithOAuth(token, httpSrv, deviceStore, oa)))

	if token != "" || deviceStore != "" {
		mux.HandleFunc("/app", launcherPageHandler)
		mux.Handle("/launch/workspaces", launchAuth(token, http.HandlerFunc(launchWorkspacesHandler), deviceStore))
		mux.Handle("/launch/start", launchAuth(token, http.HandlerFunc(launchStartHandler), deviceStore))
		mux.Handle("/launch/stop", launchAuth(token, http.HandlerFunc(launchStopHandler), deviceStore))
		mux.Handle("/launch/sessions", launchAuth(token, http.HandlerFunc(launchSessionsHandler), deviceStore))
		mux.Handle("/launch/info", launchAuth(token, http.HandlerFunc(launchInfoHandler), deviceStore))
		mux.Handle("/launch/board", launchAuth(token, http.HandlerFunc(launchBoardHandler), deviceStore))
		mux.Handle("/launch/answer", launchAuth(token, http.HandlerFunc(launchAnswerHandler), deviceStore))
		mux.Handle("/launch/send", launchAuth(token, http.HandlerFunc(launchSendHandler), deviceStore))
		mux.Handle("/launch/interrupt", launchAuth(token, http.HandlerFunc(launchInterruptHandler), deviceStore))
		mux.Handle("/launch/new", launchAuth(token, http.HandlerFunc(launchNewHandler), deviceStore))
		mux.Handle("/launch/events", launchAuth(token, http.HandlerFunc(launchEventsHandler), deviceStore))
		mux.Handle("/launch/stream", launchStream(token, deviceStore))
		mux.Handle("/launch/preview", launchAuth(token, http.HandlerFunc(launchPreviewHandler), deviceStore))
		mux.Handle("/launch/undo", launchAuth(token, http.HandlerFunc(launchUndoHandler), deviceStore))
		mux.Handle("/launch/cost", launchAuth(token, http.HandlerFunc(launchCostHandler), deviceStore))
		mux.Handle("/launch/timeline", launchAuth(token, http.HandlerFunc(launchTimelineHandler), deviceStore))
		mux.Handle("/launch/plans", launchAuth(token, http.HandlerFunc(launchPlansHandler), deviceStore))
		mux.Handle("/launch/watch-status", launchAuth(token, http.HandlerFunc(launchWatchStatusHandler), deviceStore))
		mux.Handle("/launch/status", launchAuth(token, http.HandlerFunc(launchStatusHandler), deviceStore))
		mux.Handle("/launch/preview/", http.HandlerFunc(previewProxyHandler))
		mux.Handle("/launch/kanban", launchAuth(token, http.HandlerFunc(launchKanbanHandler), deviceStore))
		mux.Handle("/launch/run", launchAuth(token, http.HandlerFunc(launchRunHandler), deviceStore))
		mux.Handle("/launch/fresh", launchAuth(token, http.HandlerFunc(launchFreshHandler), deviceStore))
		mux.Handle("/launch/refresh", launchAuth(token, http.HandlerFunc(launchRefreshHandler), deviceStore))
		mux.Handle("/launch/push", launchAuth(token, http.HandlerFunc(launchPushHandler), deviceStore))
		mux.Handle("/launch/work-on", launchAuth(token, http.HandlerFunc(launchWorkOnHandler), deviceStore))
		mux.Handle("/launch/bots", launchAuth(token, http.HandlerFunc(launchBotsHandler), deviceStore))
		mux.Handle("/launch/ask", launchAuth(token, http.HandlerFunc(launchAskHandler), deviceStore))
		mux.Handle("/launch/transcript", launchAuth(token, http.HandlerFunc(launchTranscriptHandler), deviceStore))
		mux.Handle("/launch/diff", launchAuth(token, http.HandlerFunc(launchDiffHandler), deviceStore))
		mux.Handle("/launch/watch", launchAuth(token, http.HandlerFunc(launchWatchHandler), deviceStore))
		mux.Handle("/launch/brief", launchAuth(token, http.HandlerFunc(launchBriefHandler), deviceStore))
		mux.Handle("/launch/card", launchAuth(token, http.HandlerFunc(launchCardHandler), deviceStore))
		mux.Handle("/launch/attempts", launchAuth(token, http.HandlerFunc(launchAttemptsHandler), deviceStore))
		mux.Handle("/launch/mute", launchAuth(token, http.HandlerFunc(launchMuteHandler), deviceStore))
		mux.Handle("/launch/stack", launchAuth(token, http.HandlerFunc(launchStackHandler), deviceStore))
		mux.Handle("/launch/upload", launchAuth(token, http.HandlerFunc(launchUploadHandler), deviceStore))
		mux.Handle("/launch/task", launchAuth(token, http.HandlerFunc(launchTaskHandler), deviceStore))
		mux.Handle("/launch/ticket", launchAuth(token, http.HandlerFunc(launchTicketHandler), deviceStore))
		mux.Handle("/launch/profiles", launchAuth(token, http.HandlerFunc(launchProfilesHandler), deviceStore))
		mux.Handle("/launch/devices", launchAuth(token, http.HandlerFunc(launchDevicesHandler), deviceStore))
		mux.Handle("/launch/connector", launchAuth(token, http.HandlerFunc(launchConnectorHandler), deviceStore))
		mux.Handle("/launch/doctor", launchAuth(token, http.HandlerFunc(launchDoctorHandler), deviceStore))
	}

	for _, source := range []string{"linear", "github", "gitlab", "jira"} {
		mux.HandleFunc("/hooks/"+source, watchHookHandler(source))
	}

	var pairSession *pairing.Session
	if opts.pair {
		if deviceStore == "" {
			fmt.Fprintln(os.Stderr, "corgi mcp --pair cannot be combined with --insecure, and needs a writable corgi data directory.")
			exitProcess(2)
		}
		session, _, err := pairing.NewSession()
		if err != nil {
			fmt.Fprintln(os.Stderr, "could not start pairing:", err)
			exitProcess(1)
		}
		pairSession = session
		role := ""
		if opts.viewer {
			role = pairing.RoleViewer
		}
		window := &pairWindow{}
		window.set(session, role)
		mux.Handle("/pair", pairingHandlerFor(window, deviceStore))
		defer session.Close()
		go watchPairRequests(filepath.Dir(deviceStore), window)
	}

	srv := &http.Server{Addr: addr, Handler: mux}

	announceMCPAuth(token, deviceStore)
	fmt.Fprintf(os.Stderr, "corgi mcp serving Streamable HTTP on %s/mcp\n", addr)
	printMCPClientConfig(os.Stderr, "http://"+localURL(addr)+"/mcp", token)
	if pairSession != nil {
		announcePairing(pairSession.Code(), addr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var tunnelDone <-chan struct{}
	if opts.tunnel {
		tunnelDone = startMCPTunnel(ctx, addr, token, opts, origins, oa)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		<-sig
		cancel()
		_ = srv.Close()
	}()

	err := srv.ListenAndServe()
	cancel()
	if tunnelDone != nil {
		select {
		case <-tunnelDone:
		case <-time.After(2 * time.Second):
		}
	}
	if err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "mcp server error:", err)
		exitProcess(1)
	}
}

func startMCPTunnel(ctx context.Context, addr, token string, opts mcpHTTPOpts, origins *originAllowlist, oa *oauthServer) <-chan struct{} {
	provider, named, err := buildMCPTunnelConfig(opts.tunnelProvider, opts.tunnelHostname, opts.tunnelName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tunnel:", err)
		exitProcess(2)
	}
	port, err := mcpAddrPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tunnel:", err)
		exitProcess(2)
	}
	if token == "" {
		fmt.Fprintln(os.Stderr, "⚠️⚠️⚠️  --tunnel --insecure exposes corgi control (corgi_up ⇒ arbitrary command execution) to ANYONE with the URL. Do not use on untrusted networks.")
	}
	fmt.Fprintln(os.Stderr, "⚠️  corgi_exec/corgi_db_query are disabled over a public tunnel; set CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1 to allow.")

	done := make(chan struct{})
	events := make(chan tunnel.Event, 32)
	go func() {
		tunnel.Run(ctx, provider, "mcp", port, named, events)
		close(events)
	}()
	go func() {
		defer close(done)
		for ev := range events {
			switch {
			case ev.Err != nil:
				fmt.Fprintf(os.Stderr, "🌐 ✗ tunnel: %s\n", ev.Err)
			case ev.URL != "":
				mcpPublicTunnelActive.Store(true)
				if origins != nil {
					origins.add(ev.URL)
				}
				if oa != nil {
					oa.setIssuer(ev.URL)
				}
				go probeTunnelExposure(ctx, mcpProbeTarget(ev.URL), nil)
				recordPublicURL(ev.URL)
				fmt.Fprintf(os.Stderr, "🌐 ✓ public MCP endpoint: %s/mcp\n", ev.URL)
				printMCPClientConfig(os.Stderr, ev.URL+"/mcp", "")
				if token != "" {
					fmt.Fprintln(os.Stderr, "token configured; see the local config above for the Authorization header")
				}
			}
		}
	}()
	return done
}

var probeDelays = []time.Duration{8 * time.Second, 20 * time.Second, 45 * time.Second}

func probeTunnelExposure(ctx context.Context, url string, sleep func(context.Context, time.Duration) bool) {
	if sleep == nil {
		sleep = waitOrDone
	}
	var result tunnel.AccessResult
	for _, delay := range probeDelays {
		if !sleep(ctx, delay) {
			return
		}
		result = exposureProbe(ctx, url)
		if result.Protected || !probeNameUnresolved(result) {
			break
		}
	}
	if !result.Protected {
		fmt.Fprintf(os.Stderr, "🌐 exposure: public — %s\n", result.Detail)
		return
	}
	mcpTunnelPrivate.Store(true)
	fmt.Fprintf(os.Stderr,
		"🌐 exposure: private — %s (%s). corgi_exec/corgi_db_query stay enabled; no CORGI_MCP_ALLOW_DANGEROUS_TUNNEL needed.\n",
		result.Provider, result.Detail)
}

var exposureProbe = tunnel.ProbeAccess

func probeNameUnresolved(r tunnel.AccessResult) bool {
	d := strings.ToLower(r.Detail)
	return strings.Contains(d, "no such host") || strings.Contains(d, "server misbehaving")
}

func waitOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func mcpProbeTarget(tunnelURL string) string {
	return strings.TrimSuffix(tunnelURL, "/") + "/mcp"
}

func mcpAddrPort(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("cannot parse port from %q: %w", addr, err)
	}
	return strconv.Atoi(portStr)
}

func localURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return host + ":" + port
}

func printMCPClientConfig(w io.Writer, url, token string) {
	cfg := map[string]any{"mcpServers": map[string]any{"corgi": map[string]any{"url": url}}}
	if token != "" {
		cfg["mcpServers"].(map[string]any)["corgi"].(map[string]any)["headers"] = map[string]any{
			"Authorization": "Bearer " + token,
		}
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Fprintln(w, string(b))
}

type composeContext struct {
	corgi   *utils.CorgiCompose
	cmd     *cobra.Command
	cleanup func()
}

func loadComposeCtx(composePath string) (composeContext, error) {
	rootCmd.Flags().AddFlagSet(rootCmd.PersistentFlags())

	tmp := &cobra.Command{Use: "mcp-load"}
	rootCmd.AddCommand(tmp)
	tmp.Flags().Bool("global", false, "")
	tmp.Flags().Bool("seed", false, "")
	tmp.Flags().String("artifacts-dir", "", "")

	cleanup := func() {
		_ = rootCmd.PersistentFlags().Set("filename", "")
		rootCmd.RemoveCommand(tmp)
	}

	if composePath != "" {
		if err := tmp.Root().PersistentFlags().Set("filename", composePath); err != nil {
			cleanup()
			return composeContext{}, err
		}
	}
	corgi, err := loadComposeCached(tmp, composePath)
	if err != nil {
		cleanup()
		return composeContext{}, err
	}
	return composeContext{corgi: corgi, cmd: tmp, cleanup: cleanup}, nil
}

func loadComposeCached(cmd *cobra.Command, composePath string) (*utils.CorgiCompose, error) {
	cwd, _ := os.Getwd()
	key := composeLookup{arg: composePath, cwd: cwd, tier: utils.EnvTierFromFlag}
	if corgi, ok := mcpCache.lookupCompose(key); ok {
		utils.CorgiComposeFileContent = corgi
		utils.SetContainerScope(corgi)
		return corgi, nil
	}
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		return nil, err
	}
	mcpCache.storeCompose(key, corgi)
	return corgi, nil
}

var loadComposeForMCP = func(composePath string) (*utils.CorgiCompose, error) {
	ctx, err := loadComposeCtx(composePath)
	if err != nil {
		return nil, err
	}
	ctx.cleanup()
	return ctx.corgi, nil
}

type validateArgs struct {
	ComposePath string `json:"composePath"`
}

type validateResult struct {
	Ok       bool                    `json:"ok"`
	Errors   []utils.ValidationIssue `json:"errors"`
	Warnings []utils.ValidationIssue `json:"warnings"`
}

func mcpValidate(args validateArgs) (validateResult, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return validateResult{}, composeLoadError(err)
	}
	errs, warns := utils.ValidateCompose(corgi)
	if errs == nil {
		errs = []utils.ValidationIssue{}
	}
	if warns == nil {
		warns = []utils.ValidationIssue{}
	}
	return validateResult{Ok: len(errs) == 0, Errors: errs, Warnings: warns}, nil
}

type planArgs struct {
	ComposePath string `json:"composePath"`
	Profile     string `json:"profile"`
}

func mcpPlan(args planArgs) (dryRunPlan, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return dryRunPlan{}, composeLoadError(err)
	}
	if args.Profile != "" {
		filterByProfile(corgi, args.Profile)
	}
	return computeDryRunPlan(corgi, false), nil
}

type statusEntry struct {
	Label   string `json:"label"`
	Port    int    `json:"port"`
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail"`
}

type statusArgs struct {
	ComposePath   string `json:"composePath"`
	Service       string `json:"service"`
	UnhealthyOnly bool   `json:"unhealthyOnly"`
}

func mcpStatus(args statusArgs) ([]statusEntry, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return nil, composeLoadError(err)
	}
	if args.Service != "" && !composeDeclares(corgi, args.Service) {
		return nil, fmt.Errorf("%s: %q is not a declared service or db_service", utils.ErrServiceNotFound, args.Service)
	}
	entries, ok := mcpCache.cachedStatus(utils.CorgiComposePath)
	if !ok {
		entries = probeStatusEntries(corgi)
		mcpCache.storeStatus(utils.CorgiComposePath, entries)
	}
	return filterStatusEntries(entries, args.Service, args.UnhealthyOnly), nil
}

func composeDeclares(corgi *utils.CorgiCompose, name string) bool {
	for _, s := range corgi.Services {
		if s.ServiceName == name {
			return true
		}
	}
	for _, db := range corgi.DatabaseServices {
		if db.ServiceName == name {
			return true
		}
	}
	return false
}

func filterStatusEntries(entries []statusEntry, service string, unhealthyOnly bool) []statusEntry {
	out := make([]statusEntry, 0, len(entries))
	for _, e := range entries {
		if service != "" && statusEntryName(e.Label) != service {
			continue
		}
		if unhealthyOnly && e.Healthy {
			continue
		}
		out = append(out, e)
	}
	return out
}

func statusEntryName(label string) string {
	name := strings.TrimPrefix(strings.TrimPrefix(label, "db_services."), "services.")
	if i := strings.Index(name, " ("); i >= 0 {
		name = name[:i]
	}
	return name
}

func probeStatusEntries(corgi *utils.CorgiCompose) []statusEntry {
	rows := collectStatusRows(corgi)
	up, down := probeAll(rows)
	out := make([]statusEntry, 0, len(up)+len(down))
	for _, pr := range append(append([]probeResult{}, up...), down...) {
		out = append(out, statusEntry{
			Label:   pr.Row.Label,
			Port:    pr.Row.Port,
			Kind:    pr.Row.Kind,
			URL:     pr.Row.URL,
			Healthy: pr.Healthy,
			Detail:  pr.Detail,
		})
	}
	return out
}

type envArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Key         string `json:"key"`
}

const (
	mcpEnvVarCap      = 40
	envTruncatedKey   = "_truncated"
	envTruncatedLabel = "corgi_env"
)

func mcpEnv(args envArgs) (map[string]map[string]envEntry, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return nil, composeLoadError(err)
	}
	all, err := utils.ResolveAllEnv(corgi)
	if err != nil {
		return nil, err
	}
	if args.Service != "" {
		vars, ok := all[args.Service]
		if !ok {
			return nil, fmt.Errorf("%s: service %q not found; valid services: %s",
				utils.ErrServiceNotFound, args.Service, strings.Join(serviceNames(corgi), ", "))
		}
		all = map[string][]utils.EnvVar{args.Service: vars}
	}
	order := make([]string, 0, len(all))
	for name := range all {
		order = append(order, name)
	}
	sort.Strings(order)
	doc := envKeyedMap(all, order)
	if args.Key != "" {
		for name, vars := range doc {
			e, ok := vars[args.Key]
			if !ok {
				delete(doc, name)
				continue
			}
			doc[name] = map[string]envEntry{args.Key: e}
		}
		return doc, nil
	}
	if args.Service == "" {
		capEnvDoc(doc, mcpEnvVarCap)
	}
	return doc, nil
}

func capEnvDoc(doc map[string]map[string]envEntry, limit int) {
	for name, vars := range doc {
		if len(vars) <= limit {
			continue
		}
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		kept := make(map[string]envEntry, limit+1)
		for _, k := range keys[:limit] {
			kept[k] = vars[k]
		}
		kept[envTruncatedKey] = envEntry{
			Value:  fmt.Sprintf("%d more vars hidden; call again with service=%q for all of them or key=<KEY> for one", len(vars)-limit, name),
			Source: envTruncatedLabel,
		}
		doc[name] = kept
	}
}

func mcpPs(args validateArgs) ([]psRow, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return nil, composeLoadError(err)
	}
	return buildPsRows(corgi, utils.IsPortListening), nil
}

type upArgs struct {
	ComposePath   string `json:"composePath"`
	Profile       string `json:"profile"`
	Omit          string `json:"omit"`
	Seed          bool   `json:"seed"`
	ServiceBranch string `json:"serviceBranch"`
	ServiceDir    string `json:"serviceDir"`
}

func splitPairs(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func mcpUp(args upArgs) (utils.RunState, error) {
	ctx, err := loadComposeCtx(args.ComposePath)
	if err != nil {
		return utils.RunState{}, composeLoadError(err)
	}
	defer ctx.cleanup()
	corgi := ctx.corgi

	if args.Profile != "" {
		filterByProfile(corgi, args.Profile)
	}
	if args.Seed {
		_ = ctx.cmd.Flags().Set("seed", "true")
	}

	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if isAlreadyRunning(statePath) {
		return utils.RunState{}, fmt.Errorf(errFmt, utils.ErrAlreadyRunning, "corgi is already running for this project — call corgi_down first")
	}

	var (
		state       utils.RunState
		envErr      error
		overrideErr error
	)
	withOmit(splitPairs(args.Omit), func() {
		withStdoutToStderr(func() {
			if CheckClonedReposExistence(corgi.Services) {
				CloneServices(corgi.Services)
			}
			if overrideErr = utils.ApplyServiceWorkdirs(corgi, splitPairs(args.ServiceDir), splitPairs(args.ServiceBranch), nil); overrideErr != nil {
				return
			}
			runPreflight(ctx.cmd, corgi)
			runBeforeStart(corgi)
			CreateDatabaseServices(corgi.DatabaseServices)
			runDatabaseServices(ctx.cmd, corgi.DatabaseServices)
			if envErr = utils.GenerateEnvForServices(corgi); envErr != nil {
				return
			}
			setupLogWriters(corgi)
			CreateServices(corgi.Services)
			procs := spawnDetachedServices(corgi)
			dbs := detachedDBEntries(corgi)
			state = buildDetachState(utils.CorgiComposePath, procs, dbs)
		})
	})
	if overrideErr != nil {
		return utils.RunState{}, fmt.Errorf(errFmt, utils.ErrConfig, overrideErr)
	}
	if envErr != nil {
		return utils.RunState{}, fmt.Errorf(errFmt, utils.ErrExecFailed, envErr)
	}
	mcpCache.invalidateStatus()
	if err := utils.WriteRunState(statePath, state); err != nil {
		return state, fmt.Errorf("%s: could not write run-state: %v", utils.ErrExecFailed, err)
	}
	return state, nil
}

func isAlreadyRunning(statePath string) bool {
	if _, err := os.Stat(statePath); err != nil {
		return false
	}
	prev, err := utils.ReadRunState(statePath)
	if err != nil {
		return false
	}
	prev = utils.ReconcileRunState(prev, utils.PidAlive, utils.ContainerRunning)
	for _, s := range prev.Services {
		if s.Status == "running" {
			return true
		}
	}
	return false
}

func mcpDown(args validateArgs) (stopSummary, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return stopSummary{}, composeLoadError(err)
	}

	summary := stopSummary{Stopped: []string{}, Failed: []stopFailure{}}
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if _, err := os.Stat(statePath); err != nil {
		return summary, nil
	}
	st, err := utils.ReadRunState(statePath)
	if err != nil {
		return summary, nil
	}
	st = utils.ReconcileRunState(st, utils.PidAlive, utils.ContainerRunning)
	if !anythingRunning(st) {
		os.Remove(statePath)
		return summary, nil
	}

	withStdoutToStderr(func() {
		for _, t := range append(append([]utils.RunStateEntry{}, st.Services...), st.DBServices...) {
			if t.Kind != "service" || t.Status != "running" {
				continue
			}
			if err := stopProcessGroup(t); err != nil {
				summary.Failed = append(summary.Failed, stopFailure{Name: t.Name, Error: err.Error()})
				continue
			}
			summary.Stopped = append(summary.Stopped, t.Name)
		}
		cleanup(corgi)
		if len(corgi.DatabaseServices) != 0 {
			utils.ExecuteForEachService("down")
		}
	})
	mcpCache.invalidateStatus()
	os.Remove(statePath)
	return summary, nil
}

type logsArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Lines       int    `json:"lines"`
	Grep        string `json:"grep"`
	Since       string `json:"since"`
	ErrorsOnly  bool   `json:"errorsOnly"`
}

type logsResult struct {
	Service   string   `json:"service"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated,omitempty"`
}

type mcpLogFilter struct {
	stream     logStreamFilter
	errorsOnly bool
}

func buildMCPLogFilter(args logsArgs) (mcpLogFilter, error) {
	f := mcpLogFilter{errorsOnly: args.ErrorsOnly}
	if args.Since != "" {
		since, err := parseSince(args.Since)
		if err != nil {
			return f, err
		}
		f.stream.since = since
	}
	if args.Grep != "" {
		f.stream.grep = compileLogGrep(args.Grep)
	}
	return f, nil
}

func compileLogGrep(pattern string) *regexp.Regexp {
	if re, err := regexp.Compile(pattern); err == nil {
		return re
	}
	return regexp.MustCompile(regexp.QuoteMeta(pattern))
}

func (f mcpLogFilter) allows(ts, content string) bool {
	if f.errorsOnly && detectLevel(content) != "error" {
		return false
	}
	return f.stream.allows(ts, content)
}

func mcpLogs(args logsArgs) (logsResult, error) {
	if strings.TrimSpace(args.Service) == "" {
		return logsResult{}, fmt.Errorf("%s: service is required", utils.ErrUsage)
	}
	filter, err := buildMCPLogFilter(args)
	if err != nil {
		return logsResult{}, fmt.Errorf(errFmt, utils.ErrUsage, err)
	}
	if _, err := loadComposeForMCP(args.ComposePath); err != nil {
		return logsResult{}, composeLoadError(err)
	}
	n := args.Lines
	if n <= 0 {
		n = 200
	}
	base := logsBase()
	runs, err := utils.ListServiceRuns(base, args.Service)
	if err != nil || len(runs) == 0 {
		return logsResult{}, fmt.Errorf("%s: no logs found for %q (start it with corgi run; capture is on unless --logs=false)", utils.ErrServiceNotFound, args.Service)
	}
	lines, truncated, err := readLogLines(runs[0], n, filter)
	if err != nil {
		return logsResult{}, fmt.Errorf(errFmt, utils.ErrExecFailed, err)
	}
	return logsResult{Service: args.Service, Lines: lines, Truncated: truncated}, nil
}

func tailLogFile(path string, n int) ([]string, error) {
	lines, _, err := readLogLines(path, n, mcpLogFilter{})
	return lines, err
}

func readLogLines(path string, n int, filter mcpLogFilter) ([]string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	stripPrefix := looksLikeStampedLog(path)
	raw := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(raw) == 1 && raw[0] == "" {
		return []string{}, false, nil
	}
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		ts, content := splitLogLine(line, stripPrefix)
		if filter.allows(ts, content) {
			out = append(out, content)
		}
	}
	truncated := len(out) > n
	if truncated {
		out = out[len(out)-n:]
	}
	return out, truncated, nil
}

type execArgs struct {
	ComposePath   string `json:"composePath"`
	Service       string `json:"service"`
	Command       string `json:"command"`
	EnsureDeps    bool   `json:"ensureDeps"`
	ServiceBranch string `json:"serviceBranch"`
	ServiceDir    string `json:"serviceDir"`
}

type execResult struct {
	ExitCode   int    `json:"exitCode"`
	Output     string `json:"output"`
	Truncated  bool   `json:"truncated,omitempty"`
	TimedOut   bool   `json:"timedOut,omitempty"`
	DurationMs int64  `json:"durationMs"`
}

// Output is buffered so nothing leaks onto the JSON-RPC channel.
func mcpExec(args execArgs) (execResult, error) {
	if strings.TrimSpace(args.Service) == "" || strings.TrimSpace(args.Command) == "" {
		return execResult{}, fmt.Errorf("%s: service and command are required", utils.ErrUsage)
	}
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return execResult{}, composeLoadError(err)
	}

	var overrideErr error
	withStdoutToStderr(func() {
		overrideErr = utils.ApplyServiceWorkdirs(corgi, splitPairs(args.ServiceDir), splitPairs(args.ServiceBranch), nil)
	})
	if overrideErr != nil {
		return execResult{}, fmt.Errorf(errFmt, utils.ErrConfig, overrideErr)
	}

	var service *utils.Service
	for i := range corgi.Services {
		if corgi.Services[i].ServiceName == args.Service {
			service = &corgi.Services[i]
			break
		}
	}
	if service == nil {
		return execResult{}, fmt.Errorf("%s: service %q not found; valid services: %s",
			utils.ErrServiceNotFound, args.Service, strings.Join(serviceNames(corgi), ", "))
	}

	if args.EnsureDeps {
		if err := ensureServiceDeps(corgi, *service, defaultReadyTimeout); err != nil {
			return execResult{}, fmt.Errorf(errFmt, utils.ErrReadinessTimeout, err)
		}
	}

	var (
		buf  bytes.Buffer
		code int
		err2 error
	)
	start := time.Now()
	budgetCtx, cancel := context.WithTimeout(context.Background(), mcpCallBudget())
	defer cancel()
	withStdoutToStderr(func() {
		code, err2 = utils.RunServiceCommandExitCodeContext(
			budgetCtx,
			args.Command,
			service.AbsolutePath,
			false,
			&buf,
			&buf,
			getServiceEnv(*service),
		)
	})
	durationMs := time.Since(start).Milliseconds()
	if err2 != nil && budgetCtx.Err() == nil {
		return execResult{}, fmt.Errorf("%s: failed to run command for %s: %v", utils.ErrExecFailed, args.Service, err2)
	}
	out, truncated := capMCPOutput(buf.String(), mcpMaxOutputLines, mcpMaxOutputBytes)
	return execResult{ExitCode: code, Output: out, Truncated: truncated, TimedOut: budgetCtx.Err() != nil, DurationMs: durationMs}, nil
}

type testArgs struct {
	ComposePath   string `json:"composePath"`
	Service       string `json:"service"`
	Profile       string `json:"profile"`
	EnsureDeps    bool   `json:"ensureDeps"`
	ServiceBranch string `json:"serviceBranch"`
	ServiceDir    string `json:"serviceDir"`
	Changed       bool   `json:"changed"`
	Base          string `json:"base"`
	E2E           bool   `json:"e2e"`
}

type testRunResult struct {
	Services []testResult `json:"services"`
	Passed   bool         `json:"passed"`
	TimedOut bool         `json:"timedOut,omitempty"`
	Note     string       `json:"note,omitempty"`
}

func mcpTest(args testArgs) (testRunResult, error) {
	ctx, err := loadComposeCtx(args.ComposePath)
	if err != nil {
		return testRunResult{}, composeLoadError(err)
	}
	defer ctx.cleanup()
	corgi := ctx.corgi
	if args.E2E {
		return mcpE2E(ctx.cmd, corgi)
	}
	var overrideErr error
	withStdoutToStderr(func() {
		overrideErr = utils.ApplyServiceWorkdirs(corgi, splitPairs(args.ServiceDir), splitPairs(args.ServiceBranch), nil)
	})
	if overrideErr != nil {
		return testRunResult{}, fmt.Errorf(errFmt, utils.ErrConfig, overrideErr)
	}
	sel, err := resolveSelection(corgi, args.Service, args.Profile)
	if err != nil {
		return testRunResult{}, fmt.Errorf(errFmt, utils.ErrServiceNotFound, err)
	}
	if args.Changed {
		base := args.Base
		if base == "" {
			base = "main"
		}
		withStdoutToStderr(func() { sel = narrowToChangedServices(sel, base) })
		if len(sel.services) == 0 {
			return testRunResult{
				Services: []testResult{},
				Passed:   true,
				Note:     fmt.Sprintf("no service repo differs from %s — nothing to test", base),
			}, nil
		}
	}
	var (
		results []testResult
		passed  bool
	)
	budgetCtx, cancel := context.WithTimeout(context.Background(), mcpCallBudget())
	defer cancel()
	withStdoutToStderr(func() {
		results, passed = runTestsContext(budgetCtx, corgi, sel, args.EnsureDeps, defaultReadyTimeout)
	})
	return testRunResult{Services: results, Passed: passed, TimedOut: budgetCtx.Err() != nil}, nil
}

func mcpE2E(cmd *cobra.Command, corgi *utils.CorgiCompose) (testRunResult, error) {
	suite := corgi.E2E
	if suite == nil || suite.Run == "" {
		return testRunResult{}, fmt.Errorf("%s: no e2e: block in corgi-compose.yml — declare one with workdir/install/run, or drop e2e to run each service's test script", utils.ErrConfig)
	}
	workdir := filepath.Join(utils.CorgiComposePathDir, suite.Workdir)
	if info, statErr := os.Stat(workdir); statErr != nil || !info.IsDir() {
		return testRunResult{}, fmt.Errorf("%s: e2e workdir %q does not exist", utils.ErrConfig, workdir)
	}
	var buf bytes.Buffer
	start := time.Now()
	code, err := runE2ECommands(suite, workdir, &buf)
	if err != nil {
		return testRunResult{}, fmt.Errorf("%s: e2e: %v", utils.ErrExecFailed, err)
	}
	result := testResult{Name: "e2e", ExitCode: code, DurationMs: time.Since(start).Milliseconds(), Passed: code == 0}
	result.Message, _ = capMCPOutput(buf.String(), mcpMaxOutputLines, mcpMaxOutputBytes)
	withStdoutToStderr(func() { collectE2EArtifacts(cmd, suite, workdir) })
	return testRunResult{Services: []testResult{result}, Passed: result.Passed}, nil
}

func runE2ECommands(suite *utils.E2ESuite, workdir string, out io.Writer) (int, error) {
	for _, step := range []string{suite.Install, suite.Run} {
		if step == "" {
			continue
		}
		code, err := utils.RunServiceCommandExitCode("set -e\n"+step, workdir, false, out, out, utils.SkipAutoSourceEnv)
		if err != nil || code != 0 {
			return code, err
		}
	}
	return 0, nil
}

func mcpDoctor(args validateArgs) (doctorResult, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return doctorResult{}, composeLoadError(err)
	}
	return buildDoctorResult(corgi), nil
}

type restartArgs struct {
	ComposePath string `json:"composePath"`
	Profile     string `json:"profile"`
}

func mcpRestart(args restartArgs) (upLaunch, error) {
	if _, err := mcpDown(validateArgs{ComposePath: args.ComposePath}); err != nil {
		return upLaunch{}, err
	}
	return mcpUpDetached(upArgs{ComposePath: args.ComposePath, Profile: args.Profile}, mcpUpWait)
}

type dbQueryArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Query       string `json:"query"`
}

type dbQueryResult struct {
	Service   string `json:"service"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
}

func mcpDBQuery(args dbQueryArgs) (dbQueryResult, error) {
	if strings.TrimSpace(args.Service) == "" || strings.TrimSpace(args.Query) == "" {
		return dbQueryResult{}, fmt.Errorf("%s: service and query are required", utils.ErrUsage)
	}
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return dbQueryResult{}, composeLoadError(err)
	}
	db, err := utils.GetDbServiceByName(args.Service, corgi.DatabaseServices)
	if err != nil {
		return dbQueryResult{}, fmt.Errorf("%s: db_service %q not found: %v", utils.ErrServiceNotFound, args.Service, err)
	}
	out, err := utils.ExecDBQueryCapture(db, args.Query)
	capped, truncated := capMCPOutput(out, mcpMaxOutputLines, mcpMaxOutputBytes)
	if err != nil {
		return dbQueryResult{Service: args.Service, Output: capped, Truncated: truncated}, fmt.Errorf(errFmt, utils.ErrExecFailed, err)
	}
	return dbQueryResult{Service: args.Service, Output: capped, Truncated: truncated}, nil
}

type dbSnapshotArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Name        string `json:"name"`
	Force       bool   `json:"force"`
}

type dbSnapshotResult struct {
	Service        string `json:"service"`
	Name           string `json:"name"`
	Archive        string `json:"archive"`
	SizeBytes      int64  `json:"sizeBytes"`
	PgVersionMajor string `json:"pgVersionMajor"`
	Image          string `json:"image"`
	Arch           string `json:"arch"`
}

func mcpDBSnapshot(args dbSnapshotArgs) (dbSnapshotResult, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return dbSnapshotResult{}, composeLoadError(err)
	}
	svc, err := resolvePostgresService(args.Service, corgi.DatabaseServices)
	if err != nil {
		return dbSnapshotResult{}, fmt.Errorf(errFmt, utils.ErrServiceNotFound, err)
	}
	name := args.Name
	if name == "" {
		name = utils.DefaultSnapshotName(time.Now())
	}
	name, err = utils.SanitizeSnapshotName(name)
	if err != nil {
		return dbSnapshotResult{}, fmt.Errorf(errFmt, utils.ErrUsage, err)
	}
	if err := refuseWhileSupervised(); err != nil {
		return dbSnapshotResult{}, err
	}
	container := utils.ContainerName(svc.Driver, svc.ServiceName)
	wasRunning, _ := utils.IsServiceRunning(container)
	var meta utils.SnapshotMeta
	withStdoutToStderr(func() {
		meta, err = utils.RunSnapshot(utils.SnapshotRequest{
			Service: svc.ServiceName, Driver: svc.Driver,
			Stack: filepath.Base(utils.CorgiComposePathDir),
			Name:  name, Force: args.Force, WasRunning: wasRunning,
		}, time.Now())
	})
	mcpCache.invalidateStatus()
	if err != nil {
		return dbSnapshotResult{}, fmt.Errorf("%s: snapshot failed: %v", utils.ErrExecFailed, err)
	}
	archive, _, _ := utils.SnapshotPaths(svc.ServiceName, name)
	return dbSnapshotResult{
		Service: svc.ServiceName, Name: name, Archive: archive,
		SizeBytes: meta.SizeBytes, PgVersionMajor: meta.PgVersionMajor, Image: meta.Image, Arch: meta.Arch,
	}, nil
}

type dbRestoreArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Name        string `json:"name"`
	Force       bool   `json:"force"`
}

type dbRestoreResult struct {
	Service string `json:"service"`
	Archive string `json:"archive"`
}

func mcpDBRestore(args dbRestoreArgs) (dbRestoreResult, error) {
	if strings.TrimSpace(args.Name) == "" {
		return dbRestoreResult{}, fmt.Errorf("%s: name (a snapshot name or an archive path) is required", utils.ErrUsage)
	}
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return dbRestoreResult{}, composeLoadError(err)
	}
	svc, err := resolvePostgresService(args.Service, corgi.DatabaseServices)
	if err != nil {
		return dbRestoreResult{}, fmt.Errorf(errFmt, utils.ErrServiceNotFound, err)
	}
	if err := refuseWhileSupervised(); err != nil {
		return dbRestoreResult{}, err
	}
	archive, metaPath, fromPath, err := resolveRestoreSource(svc.ServiceName, args.Name)
	if err != nil {
		return dbRestoreResult{}, fmt.Errorf(errFmt, utils.ErrUsage, err)
	}
	withStdoutToStderr(func() {
		err = utils.RunRestore(utils.RestoreRequest{
			Service: svc.ServiceName, Driver: svc.Driver,
			ArchivePath: archive, MetaPath: metaPath,
			FromPath: fromPath, Force: args.Force,
		})
	})
	mcpCache.invalidateStatus()
	if err != nil {
		return dbRestoreResult{}, fmt.Errorf("%s: restore failed: %v", utils.ErrExecFailed, err)
	}
	return dbRestoreResult{Service: svc.ServiceName, Archive: archive}, nil
}

func refuseWhileSupervised() error {
	if utils.IsStackSupervised(utils.CorgiComposePathDir) {
		return fmt.Errorf("%s: a detached run is supervising this stack's services — call corgi_down first (databases alone may stay up)", utils.ErrAlreadyRunning)
	}
	return nil
}

func mcpSchema() string { return utils.ComposeJSONSchema() }

const (
	mcpMaxOutputLines = 200
	mcpMaxOutputBytes = 16 * 1024
)

func capMCPOutput(s string, maxLines, maxBytes int) (string, bool) {
	truncated := false
	if maxLines > 0 {
		lines := strings.Split(s, "\n")
		if len(lines) > maxLines {
			head := maxLines / 3
			tail := maxLines - head
			omitted := len(lines) - head - tail
			s = strings.Join(lines[:head], "\n") +
				fmt.Sprintf("\n…[%d lines omitted]…\n", omitted) +
				strings.Join(lines[len(lines)-tail:], "\n")
			truncated = true
		}
	}
	if maxBytes > 0 && len(s) > maxBytes {
		s = "…[truncated]…\n" + s[len(s)-maxBytes:]
		truncated = true
	}
	return s, truncated
}

func filterByProfile(corgi *utils.CorgiCompose, profile string) {
	services, dbs := utils.SelectByProfiles(corgi, utils.ParseProfiles(profile))
	filteredSvcs := corgi.Services[:0]
	for _, s := range corgi.Services {
		if services[s.ServiceName] {
			filteredSvcs = append(filteredSvcs, s)
		}
	}
	corgi.Services = filteredSvcs
	filteredDbs := corgi.DatabaseServices[:0]
	for _, db := range corgi.DatabaseServices {
		if dbs[db.ServiceName] {
			filteredDbs = append(filteredDbs, db)
		}
	}
	corgi.DatabaseServices = filteredDbs
}

func composeLoadError(err error) error {
	return fmt.Errorf(errFmt, utils.ErrComposeNotFound, err)
}

func withStdoutToStderr(fn func()) {
	utils.SetConsoleOverride(os.Stderr)
	defer utils.ClearConsoleOverride()
	fn()
}

const profileDesc = "Only these profiles (comma-separated union, e.g. backend,worker)"
const omitDesc = `Compose keys to skip for this call, comma-separated, same as corgi run --omit: beforeStart, useAwsVpn (do not launch the AWS VPN client), useDocker (do not auto-start Docker). Use useAwsVpn when the VPN is not needed for the slice you start or cannot be driven from this session.`

const serviceBranchDesc = `Run service(s) on a git branch via an isolated reused worktree, without editing path: in corgi-compose.yml. Format "svc=branch[,svc2=branch2]". Non-destructive — the main checkout is untouched.`
const serviceDirDesc = `Run service(s) from an existing directory, e.g. a git worktree. Format "svc=/path[,svc2=/path2]".`

func registerMCPTools(s *server.MCPServer) {
	composeOpt := mcp.WithString("composePath", mcp.Description("compose path (default: cwd)"))
	serviceOpt := mcp.WithString("service", mcp.Required(), mcp.Description("Service name"))

	registerAgentMCPTools(s)
	registerPolicyMCPTools(s)

	s.AddTool(newCorgiTool("corgi_watch_status",
		mcp.WithDescription("What each registered workspace watches on the tracker and code host, and what it still needs before anything arrives. Returns one row per workspace: {workspace, dir, enabled, tracker, project, repos, states, prs, comments, action, hasOwnTokens, sources[], whatIsMissing[]}. sources says which of linear/jira/github/gitlab has a token (fingerprint only, never the token) and when each last polled. whatIsMissing is the ordered list of what to fix — read it before calling corgi_watch_enable. Read-only."),
		mcp.WithString("workspace", mcp.Description("Only this workspace id")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWatchStatus(watchStatusArgs{Workspace: r.GetString("workspace", "")})
	}))

	s.AddTool(newCorgiTool("corgi_watch_enable",
		mcp.WithDescription("Turn on the tracker and code-host watch for one workspace, and report what it still needs. Use when someone asks to watch a tracker or their reviews (\"watch the jira issues here\", \"tell me when someone comments on my MRs\"). project is the issue-key prefix and repos are owner/name — they are what route an event to this workspace, so derive them from the repos' own commit ids and remotes rather than guessing; a wrong key routes nothing and looks like a quiet week. states filters NEW ISSUES only, and stops a backlog arriving every poll. Tokens are NOT set here and must never be put in a tool call: run `corgi agent watch auth <source> --local` inside the workspace. Writes the user config; run corgi agent restart afterwards."),
		mcp.WithString("workspace", mcp.Description("Workspace id; omitted means the one the cwd is in")),
		mcp.WithString("tracker", mcp.Description("linear or jira; omitted keeps whichever token exists")),
		mcp.WithString("project", mcp.Description("Issue key prefix, e.g. ABC for ABC-123 — read it off the repos, never guess")),
		mcp.WithArray("repos", mcp.Description("owner/name of every repo whose reviews belong to this workspace"), mcp.WithStringItems()),
		mcp.WithArray("states", mcp.Description("Tracker state names that a NEW issue must be in; use the tracker's real names"), mcp.WithStringItems()),
		mcp.WithArray("labels", mcp.Description("Labels a new issue must carry"), mcp.WithStringItems()),
		mcp.WithBoolean("comments", mcp.Description("Comments on issues assigned to me")),
		mcp.WithBoolean("prs", mcp.Description("Reviews and comments on my pull requests")),
		mcp.WithString("action", mcp.Description("notify (default) or fix — fix runs a headless agent, draft PRs only")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		args := watchEnableArgs{
			Workspace: r.GetString("workspace", ""),
			Tracker:   r.GetString("tracker", ""),
			Project:   r.GetString("project", ""),
			Repos:     r.GetStringSlice("repos", nil),
			States:    r.GetStringSlice("states", nil),
			Labels:    r.GetStringSlice("labels", nil),
			Action:    r.GetString("action", ""),
		}
		args.Comments = optionalBool(r, "comments")
		args.PRs = optionalBool(r, "prs")
		return mcpWatchEnable(args)
	}))

	s.AddTool(newCorgiTool("corgi_watch_events",
		mcp.WithDescription("What the watch has seen, newest first: {key, kind, ref, title, url, workspace, at, canWorkOn}. kind is issue.new | issue.comment | pr.comment | pr.review. canWorkOn says corgi knows a skill for it — a new issue hands to /corgi:stories, a review to /corgi:review in address mode. Use it to answer \"what came in?\" and to pick what to work on. Read-only."),
		mcp.WithString("workspace", mcp.Description("Only this workspace's events")),
		mcp.WithNumber("limit", mcp.Description("How many, newest first (default 25)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		events, err := watchEventsForMCP(r.GetString("workspace", ""), int(r.GetFloat("limit", 25)))
		if err != nil {
			return nil, err
		}
		return map[string]any{"events": events}, nil
	}))

	s.AddTool(newCorgiTool("corgi_watch_fixes",
		mcp.WithDescription("What the unattended watch (action: fix) has worked on and what it opened: [{key, ref, kind, workspace, startedAt, running, issueUrl?, prs[], note?, error?}], newest first. A fix announces its pull request in a notification that is gone in a second — this outlives it, so it answers \"what did it do while I was away?\" and \"did it open anything?\". Read-only."),
		mcp.WithString("workspace", mcp.Description("Only this workspace's fixes")),
		mcp.WithNumber("limit", mcp.Description("How many, newest first (default 20)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		fixes, err := watchFixesForMCP(r.GetString("workspace", ""), int(r.GetFloat("limit", 20)))
		if err != nil {
			return nil, err
		}
		return map[string]any{"fixes": fixes}, nil
	}))

	s.AddTool(newCorgiTool("corgi_watch_board",
		mcp.WithDescription("The tracker columns a workspace can move a ticket to, and who its token belongs to: {workspace, tracker, project, columns[], me}. Read once and cached, so this is cheap. Read it BEFORE offering or making a move — a column name that is not on this list will be refused. Read-only."),
		mcp.WithString("workspace", mcp.Description("Which workspace; omitted means the one you are in")),
		mcp.WithBoolean("refresh", mcp.Description("Read the columns from the tracker again instead of the cache")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWatchBoard(r.GetString("workspace", ""), r.GetBool("refresh", false))
	}))

	s.AddTool(newCorgiTool("corgi_watch_move",
		mcp.WithDescription("Move one ticket to another column, as the user. `status` must be one corgi_watch_board lists; Jira also decides which moves are legal from where the ticket is now, and a refused move names the ones that were. This writes to a real board someone else reads — do it when asked, never to tidy up."),
		mcp.WithString("ref", mcp.Required(), mcp.Description("The ticket, e.g. ABC-123")),
		mcp.WithString("status", mcp.Required(), mcp.Description("The column to move it to, from corgi_watch_board")),
		mcp.WithString("workspace", mcp.Description("Which workspace; omitted means the one you are in")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpWatchMove(r.GetString("workspace", ""), r.GetString("ref", ""), r.GetString("status", ""))
	}))

	s.AddTool(newCorgiTool("corgi_today",
		mcp.WithDescription("What has been done today, per workspace: {since, headline, totals, workspaces[{workspace, dir, commits[], prompts[], fixes[], arrived[], deferred[]}], waits{count, medianS, longestS}, days[{date, sessions, messages, toolCalls}]}. Answers \"what have I done today?\" in one call — the commits that landed, what Claude was asked, and what the unattended watch did on its own with the pull requests it opened; `waits` is how often sessions waited on a person today, `days` a fortnight of activity per day across every account (the numbers the phone's share card draws; 2.22.6). The window is since midnight unless `since` asks for a rolling one. Read-only."),
		mcp.WithString("since", mcp.Description("A rolling window like 8h or 72h; omitted means since midnight")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return todayForMCP(r.GetString("since", ""), time.Now())
	}))

	s.AddTool(newCorgiTool("corgi_validate",
		mcp.WithDescription("Statically validate corgi-compose.yml (no side effects). Returns {ok, errors[], warnings[]}."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpValidate(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(newCorgiTool("corgi_plan",
		mcp.WithDescription("Compute the dry-run plan: start order, databases, services, validation. No side effects."),
		composeOpt,
		mcp.WithString("profile", mcp.Description(profileDesc)),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpPlan(planArgs{ComposePath: r.GetString("composePath", ""), Profile: r.GetString("profile", "")})
	}))

	s.AddTool(newCorgiTool("corgi_status",
		mcp.WithDescription("Live health of declared services and db_services (TCP, or HTTP when a healthCheck path is declared). Returns one entry per target: {label, kind, port, url, healthy, detail}. This is the liveness truth — corgi_ps only says whether a pid or container exists. Targets with no declared port are not probed and do not appear, so an empty list is not a healthy stack. Pass service to get one target, unhealthyOnly to get only what is down (empty = all healthy). Probe results are reused for 1s across calls. Read-only."),
		composeOpt,
		mcp.WithString("service", mcp.Description("Only this service or db_service (declared name)")),
		mcp.WithBoolean("unhealthyOnly", mcp.Description("Only targets that failed the probe")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpStatus(statusArgs{
			ComposePath:   r.GetString("composePath", ""),
			Service:       r.GetString("service", ""),
			UnhealthyOnly: r.GetBool("unhealthyOnly", false),
		})
	}))

	s.AddTool(newCorgiTool("corgi_env",
		mcp.WithDescription("Resolved environment per service with source attribution. Returns {service: {KEY: {value, source}}}, real values. Prefer service (one service, every var) or key (one var across services): without either, each service is capped at 40 vars and a \"_truncated\" entry says how many were hidden. Read-only."),
		composeOpt,
		mcp.WithString("service", mcp.Description("Only this service's vars (uncapped)")),
		mcp.WithString("key", mcp.Description("Only this var, in every service that has it")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpEnv(envArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Key:         r.GetString("key", ""),
		})
	}))

	registerAgentSurfaceTools(s, composeOpt)

	s.AddTool(newCorgiTool("corgi_ps",
		mcp.WithDescription("Runtime snapshot of the detached run: one row per declared service and db_service, {name, kind, port, status, url, startedAt}, reconciled against corgi_services/.state.json with a cheap port-listening check. status is process/container state (running | crashed | stopped), not health — db_services and container-backed services never report crashed. Use corgi_status for liveness. Read-only."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpPs(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(newCorgiTool("corgi_up",
		mcp.WithDescription("Start every database and service detached, in a child process, and return within about 20 seconds: {status, handle{pid, logPath}, next, state?}. status \"started\" means the boot finished and state is the run-state {services[], dbServices[]} with each entry's name, pid, port, status; \"starting\" means it is still cloning repos, running beforeStart (installs, migrations, builds) or bringing databases up — minutes on a cold stack — and handle.logPath is where to read the boot log; \"failed\" carries the last log lines in error. Neither is a ready gate: poll corgi_status until healthy. Fails with E_ALREADY_RUNNING while a run is live — call corgi_down first; a second call during a boot returns the same handle. A service that crashed on spawn shows status \"crashed\" in state."),
		composeOpt,
		mcp.WithString("profile", mcp.Description(profileDesc)),
		mcp.WithString("omit", mcp.Description(omitDesc)),
		mcp.WithBoolean("seed", mcp.Description("Seed db_services that have a dump/seed source")),
		mcp.WithString("serviceBranch", mcp.Description(serviceBranchDesc)),
		mcp.WithString("serviceDir", mcp.Description(serviceDirDesc)),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpUpDetached(upArgs{
			ComposePath:   r.GetString("composePath", ""),
			Profile:       r.GetString("profile", ""),
			Omit:          r.GetString("omit", ""),
			Seed:          r.GetBool("seed", false),
			ServiceBranch: r.GetString("serviceBranch", ""),
			ServiceDir:    r.GetString("serviceDir", ""),
		}, mcpUpWait)
	}))

	s.AddTool(newCorgiTool("corgi_down",
		mcp.WithDescription("Stop the detached run: end the service processes, run each service's afterStart, bring db_service containers down, clear the run-state. Idempotent — nothing running is a clean no-op. Returns {stopped[], failed[]}."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpDown(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(newCorgiTool("corgi_logs",
		mcp.WithDescription("Read the last N lines of a service's newest captured log run (capture is on by default for detached runs). Returns {service, lines[], truncated}. Filters (grep, since, errorsOnly) apply before the tail, so lines counts matching lines — use errorsOnly or grep first and read the whole log only when they come back empty. Needs a prior corgi_up; a service that never spawned has no log. To wait for a specific line use corgi_wait_for_log instead of polling this. Read-only."),
		composeOpt,
		serviceOpt,
		mcp.WithNumber("lines", mcp.Description("Number of trailing (matching) lines (default 200)")),
		mcp.WithString("grep", mcp.Description("Only lines matching this regexp (a pattern that does not compile is matched as a literal substring)")),
		mcp.WithString("since", mcp.Description("Only lines newer than this: a duration like 10m, or an RFC3339 timestamp")),
		mcp.WithBoolean("errorsOnly", mcp.Description("Only lines that look like errors (error, panic, fatal — the same heuristic corgi logs --json uses for level)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpLogs(logsArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Lines:       r.GetInt("lines", 0),
			Grep:        r.GetString("grep", ""),
			Since:       r.GetString("since", ""),
			ErrorsOnly:  r.GetBool("errorsOnly", false),
		})
	}))

	s.AddTool(newCorgiTool("corgi_exec",
		mcp.WithDescription("Run a one-off command in a service's resolved env. Returns {exitCode, output, truncated, durationMs}; large output is capped head+tail."),
		composeOpt,
		serviceOpt,
		mcp.WithString("command", mcp.Required(), mcp.Description("Command line to run (via /bin/sh -c)")),
		mcp.WithBoolean("ensureDeps", mcp.Description("Wait for depends_on_db/services to be ready first")),
		mcp.WithString("serviceBranch", mcp.Description(serviceBranchDesc)),
		mcp.WithString("serviceDir", mcp.Description(serviceDirDesc)),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", dangerousToolBlockedMsg)
		}
		return mcpExec(execArgs{
			ComposePath:   r.GetString("composePath", ""),
			Service:       r.GetString("service", ""),
			Command:       r.GetString("command", ""),
			EnsureDeps:    r.GetBool("ensureDeps", false),
			ServiceBranch: r.GetString("serviceBranch", ""),
			ServiceDir:    r.GetString("serviceDir", ""),
		})
	}))

	s.AddTool(newCorgiTool("corgi_test",
		mcp.WithDescription("Run each selected service's `test` script in its resolved env. Returns {services[], passed, note?}. Does not start databases/services. changed narrows to services whose repo differs from base (uncommitted work counts) — the cheap default after editing a few repos. e2e runs the stack's e2e: block against the already-running stack instead, returning one \"e2e\" entry with the captured output in message."),
		composeOpt,
		mcp.WithString("service", mcp.Description("Only test this service")),
		mcp.WithString("profile", mcp.Description(profileDesc)),
		mcp.WithBoolean("ensureDeps", mcp.Description("Wait for depends_on_db/services to be ready first")),
		mcp.WithBoolean("changed", mcp.Description("Only services whose repo differs from base (same as corgi test --changed)")),
		mcp.WithString("base", mcp.Description("Branch to compare against for changed (default main; falls back to origin/<base>)")),
		mcp.WithBoolean("e2e", mcp.Description("Run the compose file's e2e: block against the running stack instead of per-service test scripts")),
		mcp.WithString("serviceBranch", mcp.Description(serviceBranchDesc)),
		mcp.WithString("serviceDir", mcp.Description(serviceDirDesc)),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpTest(testArgs{
			ComposePath:   r.GetString("composePath", ""),
			Service:       r.GetString("service", ""),
			Profile:       r.GetString("profile", ""),
			EnsureDeps:    r.GetBool("ensureDeps", false),
			ServiceBranch: r.GetString("serviceBranch", ""),
			ServiceDir:    r.GetString("serviceDir", ""),
			Changed:       r.GetBool("changed", false),
			Base:          r.GetString("base", ""),
			E2E:           r.GetBool("e2e", false),
		})
	}))

	s.AddTool(newCorgiTool("corgi_doctor",
		mcp.WithDescription("Preflight checks (required tools, Docker, port availability). Returns {ok, checks[]}. No side effects."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpDoctor(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(newCorgiTool("corgi_restart",
		mcp.WithDescription("corgi_down then corgi_up in one call; returns what corgi_up returns: {status, handle, next, state?}. Same cost and caveats as corgi_up: beforeStart re-runs in the child unless its cacheKey is warm, and returning is not a ready gate — poll corgi_status."),
		composeOpt,
		mcp.WithString("profile", mcp.Description(profileDesc)),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpRestart(restartArgs{
			ComposePath: r.GetString("composePath", ""),
			Profile:     r.GetString("profile", ""),
		})
	}))

	addStackTools(s, composeOpt, serviceOpt)

	s.AddTool(newCorgiTool("corgi_db_query",
		mcp.WithDescription("Run one non-interactive query inside a running db_service container through its driver's own client (psql, redis-cli, mongosh, …); write the query in that client's syntax. Returns {service, output, truncated}. The db_service must already be up (corgi_up). Writes are not blocked — a mutating statement runs, so call corgi_db_snapshot first and corgi_db_restore to undo. Disabled over a public tunnel unless CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1."),
		composeOpt,
		serviceOpt,
		mcp.WithString("query", mcp.Required(), mcp.Description("Query/command to run (e.g. SQL for psql)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", dangerousToolBlockedMsg)
		}
		return mcpDBQuery(dbQueryArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Query:       r.GetString("query", ""),
		})
	}))

	s.AddTool(newCorgiTool("corgi_db_snapshot",
		mcp.WithDescription("Physical snapshot of a postgres-family db_service's data (postgres, postgis, pgvector, timescaledb), the same as corgi db snapshot. Take one BEFORE a corgi_db_query that mutates data (UPDATE/DELETE/DROP/migrations) so corgi_db_restore can undo it. Returns {service, name, archive, sizeBytes, pgVersionMajor, image, arch}. Stops the db container briefly and restarts it if it was running; refused with E_ALREADY_RUNNING while a detached run is supervising service processes (bring only the databases up, or corgi_down first)."),
		composeOpt,
		mcp.WithString("service", mcp.Description("db_service name (optional when the stack has exactly one postgres-family db)")),
		mcp.WithString("name", mcp.Description("Snapshot name (default: UTC timestamp like 2026-09-10-1530)")),
		mcp.WithBoolean("force", mcp.Description("Overwrite a snapshot with the same name")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpDBSnapshot(dbSnapshotArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Name:        r.GetString("name", ""),
			Force:       r.GetBool("force", false),
		})
	}))

	s.AddTool(newCorgiTool("corgi_db_restore",
		mcp.WithDescription("DESTRUCTIVE: wipe a postgres-family db_service's data volume and restore a snapshot taken by corgi_db_snapshot (or an archive path), the same as corgi db restore --yes. No confirmation — the undo for a mutating corgi_db_query. Returns {service, archive}. Same E_ALREADY_RUNNING rule as corgi_db_snapshot. Disabled over a public tunnel unless CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1."),
		composeOpt,
		mcp.WithString("name", mcp.Required(), mcp.Description("Snapshot name from corgi_db_snapshot, or a path to a .tar.zst archive")),
		mcp.WithString("service", mcp.Description("db_service name (optional when the stack has exactly one postgres-family db)")),
		mcp.WithBoolean("force", mcp.Description("Restore even when the snapshot's pg major/arch differ from the running image")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		if !dangerousTunnelToolsAllowed(mcpPublicTunnelActive.Load()) {
			return nil, fmt.Errorf("%s", dangerousToolBlockedMsg)
		}
		return mcpDBRestore(dbRestoreArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Name:        r.GetString("name", ""),
			Force:       r.GetBool("force", false),
		})
	}))

	s.AddTool(newCorgiTool("corgi_schema",
		mcp.WithDescription("Return the JSON Schema (draft-07) for corgi-compose.yml."),
	), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mcpHandlerMu.Lock()
		defer mcpHandlerMu.Unlock()
		return mcp.NewToolResultText(mcpSchema()), nil
	})
}

func jsonHandler(core func(mcp.CallToolRequest) (any, error)) server.ToolHandlerFunc {
	return func(_ context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mcpHandlerMu.Lock()
		defer mcpHandlerMu.Unlock()
		out, err := core(r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, err := json.Marshal(out)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("%s: marshal result: %v", utils.ErrExecFailed, err)), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	}
}

func registerMCPResources(s *server.MCPServer) {
	s.AddResource(
		mcp.NewResource("corgi://schema", "corgi compose JSON Schema",
			mcp.WithResourceDescription("JSON Schema (draft-07) for corgi-compose.yml"),
			mcp.WithMIMEType(mimeJSON)),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			mcpHandlerMu.Lock()
			defer mcpHandlerMu.Unlock()
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: "corgi://schema", MIMEType: mimeJSON, Text: utils.ComposeJSONSchema(),
			}}, nil
		})

	s.AddResource(
		mcp.NewResource("corgi://drivers", "supported db drivers",
			mcp.WithResourceDescription("Supported db_services.driver values"),
			mcp.WithMIMEType(mimeJSON)),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			mcpHandlerMu.Lock()
			defer mcpHandlerMu.Unlock()
			b, err := json.Marshal(utils.KnownDrivers)
			if err != nil {
				return nil, fmt.Errorf("%s: marshal drivers: %v", utils.ErrExecFailed, err)
			}
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: "corgi://drivers", MIMEType: mimeJSON, Text: string(b),
			}}, nil
		})

	s.AddResource(
		mcp.NewResource("corgi://compose", "current corgi compose",
			mcp.WithResourceDescription("Resolved/interpolated corgi-compose.yml as JSON"),
			mcp.WithMIMEType(mimeJSON)),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			mcpHandlerMu.Lock()
			defer mcpHandlerMu.Unlock()
			corgi, err := loadComposeForMCP("")
			if err != nil {
				return nil, composeLoadError(err)
			}
			b, err := json.MarshalIndent(corgi, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("%s: marshal compose: %v", utils.ErrExecFailed, err)
			}
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: "corgi://compose", MIMEType: mimeJSON, Text: string(b),
			}}, nil
		})

	s.AddResource(
		mcp.NewResource("corgi://status", "live status snapshot",
			mcp.WithResourceDescription("Live health snapshot of declared services and db_services"),
			mcp.WithMIMEType(mimeJSON)),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			mcpHandlerMu.Lock()
			defer mcpHandlerMu.Unlock()
			out, err := mcpStatus(statusArgs{})
			if err != nil {
				return nil, err
			}
			b, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("%s: marshal status: %v", utils.ErrExecFailed, err)
			}
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: "corgi://status", MIMEType: mimeJSON, Text: string(b),
			}}, nil
		})
}
