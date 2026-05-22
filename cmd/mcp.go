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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/tunnel"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

// mcpCmd runs corgi as an MCP server over stdio. stdout is the JSON-RPC channel.
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
	rootCmd.AddCommand(mcpCmd)
}

const (
	errFmt   = "%s: %v"
	mimeJSON = "application/json"
)

// mcpHandlerMu serializes all MCP tool/resource work. mcp-go can invoke handlers
// concurrently, but handlers mutate global state (os.Stdout swap, rootCmd flags,
// utils.CorgiComposePath*) that isn't concurrency-safe. Held across the entire
// handler body so the stdout swap and compose/flag mutation never overlap.
var mcpHandlerMu sync.Mutex

func runMCP(cmd *cobra.Command, _ []string) {
	// Route corgi's own logging to stderr so stdout stays the JSON-RPC channel.
	utils.NonInteractive = true
	utils.JSONOutput = true

	s := server.NewMCPServer("corgi", APP_VERSION)
	registerMCPTools(s)
	registerMCPResources(s)

	httpAddr, _ := cmd.Flags().GetString("http")
	opts := mcpHTTPOptsFromFlags(cmd)
	if opts.tunnel && httpAddr == "" {
		fmt.Fprintln(os.Stderr, "corgi mcp --tunnel requires --http (the local addr to expose).")
		os.Exit(2)
	}
	if httpAddr != "" {
		serveMCPHTTP(s, httpAddr, resolveMCPToken(opts), opts)
		return
	}
	serveMCPStdio(s)
}

type mcpHTTPOpts struct {
	tunnel         bool
	tunnelProvider string
	tunnelHostname string
	tunnelName     string
	token          string
	insecure       bool
}

func mcpHTTPOptsFromFlags(cmd *cobra.Command) mcpHTTPOpts {
	o := mcpHTTPOpts{}
	o.tunnel, _ = cmd.Flags().GetBool("tunnel")
	o.tunnelProvider, _ = cmd.Flags().GetString("tunnel-provider")
	o.tunnelHostname, _ = cmd.Flags().GetString("tunnel-hostname")
	o.tunnelName, _ = cmd.Flags().GetString("tunnel-name")
	o.token, _ = cmd.Flags().GetString("token")
	o.insecure, _ = cmd.Flags().GetBool("insecure")
	return o
}

// resolveMCPToken applies the token rules. Plain --http with no --token and no
// --tunnel stays no-auth (token=="") so existing users are unaffected. A token
// is auto-generated only for a public tunnel without an explicit one.
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

// generateMCPToken returns a url-safe bearer token prefixed corgi_mcp_.
func generateMCPToken() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is fatal: a weak token would defeat the auth.
		fmt.Fprintln(os.Stderr, "could not generate token:", err)
		os.Exit(1)
	}
	return "corgi_mcp_" + base64.RawURLEncoding.EncodeToString(b)
}

// bearerAuth wraps next with a constant-time Bearer-token check. token=="" is
// no-auth and returns next unchanged.
func bearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			w.Header().Set("Content-Type", mimeJSON)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// buildMCPTunnelConfig selects a provider and builds a NamedConfig from the mcp
// tunnel flags, expanding ${VAR} in the hostname. A named config is returned
// only when a hostname (or name) is set; otherwise nil => quick tunnel.
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
	// WithWorkerPoolSize(1) is defense-in-depth; mcpHandlerMu is the real guard.
	if err := server.ServeStdio(s, server.WithWorkerPoolSize(1)); err != nil {
		fmt.Fprintln(os.Stderr, "mcp server error:", err)
		os.Exit(1)
	}
}

func serveMCPHTTP(s *server.MCPServer, addr, token string, opts mcpHTTPOpts) {
	httpSrv := server.NewStreamableHTTPServer(s)
	// httpSrv.ServeHTTP serves /mcp; mount it on a mux so other paths 404.
	mux := http.NewServeMux()
	mux.Handle("/mcp", httpSrv)
	srv := &http.Server{Addr: addr, Handler: bearerAuth(token, mux)}

	if token == "" {
		fmt.Fprintln(os.Stderr, "⚠️  corgi mcp --http has no auth; bind to localhost or put it behind an authenticated proxy.")
	} else {
		fmt.Fprintf(os.Stderr, "corgi mcp bearer token: %s\n", token)
	}
	fmt.Fprintf(os.Stderr, "corgi mcp serving Streamable HTTP on %s/mcp\n", addr)
	printMCPClientConfig(os.Stderr, "http://"+localURL(addr)+"/mcp", token)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if opts.tunnel {
		startMCPTunnel(ctx, addr, token, opts)
	}

	// Cancel the tunnel ctx on signal so its subprocess dies with the server.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() {
		<-sig
		cancel()
		_ = srv.Close()
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "mcp server error:", err)
		os.Exit(1)
	}
}

// startMCPTunnel opens one tunnel to addr's local port using the shared
// tunnel.Run runner, bound to ctx so it dies with the server.
func startMCPTunnel(ctx context.Context, addr, token string, opts mcpHTTPOpts) {
	provider, named, err := buildMCPTunnelConfig(opts.tunnelProvider, opts.tunnelHostname, opts.tunnelName)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tunnel:", err)
		os.Exit(2)
	}
	port, err := mcpAddrPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "tunnel:", err)
		os.Exit(2)
	}
	if token == "" {
		fmt.Fprintln(os.Stderr, "⚠️⚠️⚠️  --tunnel --insecure exposes corgi control (corgi_up/corgi_exec ⇒ arbitrary command execution) to ANYONE with the URL. Do not use on untrusted networks.")
	}

	events := make(chan tunnel.Event, 32)
	go func() {
		tunnel.Run(ctx, provider, "mcp", port, named, events)
		close(events) // terminate the consumer below when the tunnel exits
	}()
	go func() {
		for ev := range events {
			switch {
			case ev.Err != nil:
				fmt.Fprintf(os.Stderr, "🌐 ✗ tunnel: %s\n", ev.Err)
			case ev.URL != "":
				fmt.Fprintf(os.Stderr, "🌐 ✓ public MCP endpoint: %s/mcp\n", ev.URL)
				printMCPClientConfig(os.Stderr, ev.URL+"/mcp", token)
			}
		}
	}()
}

// mcpAddrPort extracts the numeric port from a listen addr like ":8765" or
// "127.0.0.1:8765".
func mcpAddrPort(addr string) (int, error) {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, fmt.Errorf("cannot parse port from %q: %w", addr, err)
	}
	return strconv.Atoi(portStr)
}

// localURL renders addr as a dialable host:port, defaulting an empty host to
// 127.0.0.1 for the printed local URL.
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

// printMCPClientConfig prints a ready-to-paste mcpServers JSON block, including
// the Authorization header only when a token is set.
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

// composeContext bundles a loaded compose with the throwaway cobra command it
// was loaded through, plus a cleanup to detach that command from rootCmd.
type composeContext struct {
	corgi   *utils.CorgiCompose
	cmd     *cobra.Command
	cleanup func()
}

// loadComposeCtx loads the compose via utils.GetCorgiServices. MCP runs outside
// cobra's flag context, so it attaches a throwaway command to rootCmd. Empty
// path => default resolution from cwd. Caller must defer ctx.cleanup().
func loadComposeCtx(composePath string) (composeContext, error) {
	// rootCmd's persistent flags are only merged into Flags() during Execute(),
	// which MCP never runs, so merge them explicitly.
	rootCmd.Flags().AddFlagSet(rootCmd.PersistentFlags())

	tmp := &cobra.Command{Use: "mcp-load"}
	rootCmd.AddCommand(tmp)
	// determineCorgiComposePath reads --global off the command directly, not Root.
	tmp.Flags().Bool("global", false, "")
	tmp.Flags().Bool("seed", false, "")

	cleanup := func() {
		rootCmd.RemoveCommand(tmp)
		_ = tmp.Root().PersistentFlags().Set("filename", "")
	}

	if composePath != "" {
		if err := tmp.Root().PersistentFlags().Set("filename", composePath); err != nil {
			cleanup()
			return composeContext{}, err
		}
	}
	corgi, err := utils.GetCorgiServices(tmp)
	if err != nil {
		cleanup()
		return composeContext{}, err
	}
	return composeContext{corgi: corgi, cmd: tmp, cleanup: cleanup}, nil
}

// loadComposeForMCP loads just the parsed compose.
func loadComposeForMCP(composePath string) (*utils.CorgiCompose, error) {
	ctx, err := loadComposeCtx(composePath)
	if err != nil {
		return nil, err
	}
	ctx.cleanup()
	return ctx.corgi, nil
}

// --- typed handler cores (testable without a JSON-RPC pipe) ---

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
	return computeDryRunPlan(corgi), nil
}

type statusEntry struct {
	Label   string `json:"label"`
	Port    int    `json:"port"`
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	Healthy bool   `json:"healthy"`
	Detail  string `json:"detail"`
}

func mcpStatus(args validateArgs) ([]statusEntry, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return nil, composeLoadError(err)
	}
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
	return out, nil
}

func mcpPs(args validateArgs) ([]psRow, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return nil, composeLoadError(err)
	}
	return buildPsRows(corgi, utils.IsPortListening), nil
}

type upArgs struct {
	ComposePath string `json:"composePath"`
	Profile     string `json:"profile"`
	Seed        bool   `json:"seed"`
}

// mcpUp always starts DETACHED so the tool returns promptly: it mirrors the
// foreground run prelude, then runDetached's state machine.
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
		state  utils.RunState
		envErr error
	)
	withStdoutToStderr(func() {
		if CheckClonedReposExistence(corgi.Services) {
			CloneServices(corgi.Services)
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
	if envErr != nil {
		return utils.RunState{}, fmt.Errorf(errFmt, utils.ErrExecFailed, envErr)
	}
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
	os.Remove(statePath)
	return summary, nil
}

type logsArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Lines       int    `json:"lines"`
}

type logsResult struct {
	Service string   `json:"service"`
	Lines   []string `json:"lines"`
}

func mcpLogs(args logsArgs) (logsResult, error) {
	if strings.TrimSpace(args.Service) == "" {
		return logsResult{}, fmt.Errorf("%s: service is required", utils.ErrUsage)
	}
	// Load compose only to resolve CorgiComposePathDir for the log base.
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
		return logsResult{}, fmt.Errorf("%s: no logs found for %q (run with corgi run --logs)", utils.ErrServiceNotFound, args.Service)
	}
	lines, err := tailLogFile(runs[0], n)
	if err != nil {
		return logsResult{}, fmt.Errorf(errFmt, utils.ErrExecFailed, err)
	}
	return logsResult{Service: args.Service, Lines: lines}, nil
}

// tailLogFile returns the last n lines of a log file, stripping the timestamp prefix.
func tailLogFile(path string, n int) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	stripPrefix := looksLikeStampedLog(path)
	raw := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(raw) == 1 && raw[0] == "" {
		return []string{}, nil
	}
	if len(raw) > n {
		raw = raw[len(raw)-n:]
	}
	out := make([]string, len(raw))
	for i, line := range raw {
		if stripPrefix && len(line) >= utils.LogTimestampLen {
			out[i] = line[utils.LogTimestampLen:]
		} else {
			out[i] = line
		}
	}
	return out, nil
}

type execArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Command     string `json:"command"`
	EnsureDeps  bool   `json:"ensureDeps"`
}

type execResult struct {
	ExitCode   int    `json:"exitCode"`
	Output     string `json:"output"`
	DurationMs int64  `json:"durationMs"`
}

// mcpExec runs a one-off command in a service's resolved env, capturing combined
// output into a buffer so nothing leaks to the JSON-RPC channel.
func mcpExec(args execArgs) (execResult, error) {
	if strings.TrimSpace(args.Service) == "" || strings.TrimSpace(args.Command) == "" {
		return execResult{}, fmt.Errorf("%s: service and command are required", utils.ErrUsage)
	}
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return execResult{}, composeLoadError(err)
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
	withStdoutToStderr(func() {
		code, err2 = utils.RunServiceCommandExitCode(
			args.Command,
			service.AbsolutePath,
			false, // never interactive under MCP
			&buf,
			&buf,
			getServiceEnv(*service),
		)
	})
	durationMs := time.Since(start).Milliseconds()
	if err2 != nil {
		return execResult{}, fmt.Errorf("%s: failed to run command for %s: %v", utils.ErrExecFailed, args.Service, err2)
	}
	return execResult{ExitCode: code, Output: buf.String(), DurationMs: durationMs}, nil
}

type testArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Profile     string `json:"profile"`
	EnsureDeps  bool   `json:"ensureDeps"`
}

type testRunResult struct {
	Services []testResult `json:"services"`
	Passed   bool         `json:"passed"`
}

// mcpTest runs each selected service's test script, mirroring `corgi test`.
// Test scripts execute commands; their child stdout is routed to stderr so the
// JSON-RPC channel stays clean.
func mcpTest(args testArgs) (testRunResult, error) {
	corgi, err := loadComposeForMCP(args.ComposePath)
	if err != nil {
		return testRunResult{}, composeLoadError(err)
	}
	sel, err := resolveSelection(corgi, args.Service, args.Profile)
	if err != nil {
		return testRunResult{}, fmt.Errorf(errFmt, utils.ErrServiceNotFound, err)
	}
	var (
		results []testResult
		passed  bool
	)
	withStdoutToStderr(func() {
		results, passed = runTests(corgi, sel, args.EnsureDeps, defaultReadyTimeout)
	})
	return testRunResult{Services: results, Passed: passed}, nil
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

// mcpRestart stops the detached stack then starts it again detached, returning
// the new run-state. Down/up already route their progress prints to stderr.
func mcpRestart(args restartArgs) (utils.RunState, error) {
	if _, err := mcpDown(validateArgs{ComposePath: args.ComposePath}); err != nil {
		return utils.RunState{}, err
	}
	return mcpUp(upArgs{ComposePath: args.ComposePath, Profile: args.Profile})
}

type dbQueryArgs struct {
	ComposePath string `json:"composePath"`
	Service     string `json:"service"`
	Query       string `json:"query"`
}

type dbQueryResult struct {
	Service string `json:"service"`
	Output  string `json:"output"`
}

// mcpDBQuery runs a single non-interactive query against a db_service container,
// capturing the tool's output instead of streaming it to stdout.
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
	if err != nil {
		return dbQueryResult{Service: args.Service, Output: out}, fmt.Errorf(errFmt, utils.ErrExecFailed, err)
	}
	return dbQueryResult{Service: args.Service, Output: out}, nil
}

func mcpSchema() string { return utils.ComposeJSONSchema() }

// filterByProfile narrows corgi to the selection for the given comma-separated
// profiles (members plus their transitive depends_on closure).
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

// composeLoadError prefixes the stable error code so agents can branch on it.
func composeLoadError(err error) error {
	return fmt.Errorf(errFmt, utils.ErrComposeNotFound, err)
}

// withStdoutToStderr runs fn with os.Stdout pointed at os.Stderr, keeping the
// run/stop paths' progress prints off the JSON-RPC channel.
func withStdoutToStderr(fn func()) {
	orig := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = orig }()
	fn()
}

// --- MCP tool registration (thin wrappers over the cores above) ---

const profileDesc = "Run only these profiles' services/db_services (comma-separated for a union, e.g. backend,worker)"

func registerMCPTools(s *server.MCPServer) {
	composeOpt := mcp.WithString("composePath", mcp.Description("Path to corgi-compose.yml (default: resolve from cwd)"))
	serviceOpt := mcp.WithString("service", mcp.Required(), mcp.Description("Service name"))

	s.AddTool(mcp.NewTool("corgi_validate",
		mcp.WithDescription("Statically validate corgi-compose.yml (no side effects). Returns {ok, errors[], warnings[]}."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpValidate(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(mcp.NewTool("corgi_plan",
		mcp.WithDescription("Compute the dry-run plan: start order, databases, services, validation. No side effects."),
		composeOpt,
		mcp.WithString("profile", mcp.Description(profileDesc)),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpPlan(planArgs{ComposePath: r.GetString("composePath", ""), Profile: r.GetString("profile", "")})
	}))

	s.AddTool(mcp.NewTool("corgi_status",
		mcp.WithDescription("Live health snapshot of declared services and db_services (TCP/HTTP probe)."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpStatus(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(mcp.NewTool("corgi_ps",
		mcp.WithDescription("Runtime snapshot of declared topology with a cheap port-listening probe."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpPs(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(mcp.NewTool("corgi_up",
		mcp.WithDescription("Start all databases and services DETACHED and return promptly with the run-state."),
		composeOpt,
		mcp.WithString("profile", mcp.Description(profileDesc)),
		mcp.WithBoolean("seed", mcp.Description("Seed db_services that have a dump/seed source")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpUp(upArgs{
			ComposePath: r.GetString("composePath", ""),
			Profile:     r.GetString("profile", ""),
			Seed:        r.GetBool("seed", false),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_down",
		mcp.WithDescription("Stop detached services and bring db_service containers down. Idempotent."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpDown(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(mcp.NewTool("corgi_logs",
		mcp.WithDescription("Read the last N lines of a service's newest captured log run."),
		composeOpt,
		serviceOpt,
		mcp.WithNumber("lines", mcp.Description("Number of trailing lines (default 200)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpLogs(logsArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Lines:       r.GetInt("lines", 0),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_exec",
		mcp.WithDescription("Run a one-off command in a service's resolved env. Returns {exitCode, output, durationMs}."),
		composeOpt,
		serviceOpt,
		mcp.WithString("command", mcp.Required(), mcp.Description("Command line to run (via /bin/sh -c)")),
		mcp.WithBoolean("ensureDeps", mcp.Description("Wait for depends_on_db/services to be ready first")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpExec(execArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Command:     r.GetString("command", ""),
			EnsureDeps:  r.GetBool("ensureDeps", false),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_test",
		mcp.WithDescription("Run each selected service's `test` script in its resolved env. Returns {services[], passed}. Does not start databases/services."),
		composeOpt,
		mcp.WithString("service", mcp.Description("Only test this service")),
		mcp.WithString("profile", mcp.Description(profileDesc)),
		mcp.WithBoolean("ensureDeps", mcp.Description("Wait for depends_on_db/services to be ready first")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpTest(testArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Profile:     r.GetString("profile", ""),
			EnsureDeps:  r.GetBool("ensureDeps", false),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_doctor",
		mcp.WithDescription("Preflight checks (required tools, Docker, port availability). Returns {ok, checks[]}. No side effects."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpDoctor(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(mcp.NewTool("corgi_restart",
		mcp.WithDescription("Stop the detached stack then start it again DETACHED. Returns the new run-state."),
		composeOpt,
		mcp.WithString("profile", mcp.Description(profileDesc)),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpRestart(restartArgs{
			ComposePath: r.GetString("composePath", ""),
			Profile:     r.GetString("profile", ""),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_db_query",
		mcp.WithDescription("Run a single non-interactive query against a running db_service container. Returns {service, output}."),
		composeOpt,
		serviceOpt,
		mcp.WithString("query", mcp.Required(), mcp.Description("Query/command to run (e.g. SQL for psql)")),
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpDBQuery(dbQueryArgs{
			ComposePath: r.GetString("composePath", ""),
			Service:     r.GetString("service", ""),
			Query:       r.GetString("query", ""),
		})
	}))

	s.AddTool(mcp.NewTool("corgi_schema",
		mcp.WithDescription("Return the JSON Schema (draft-07) for corgi-compose.yml."),
	), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mcpHandlerMu.Lock()
		defer mcpHandlerMu.Unlock()
		return mcp.NewToolResultText(mcpSchema()), nil
	})
}

// jsonHandler wraps a typed core into an MCP tool handler, marshaling the result
// to JSON text and converting a returned error into an MCP tool error.
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

// --- MCP resources ---

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
			out, err := mcpStatus(validateArgs{})
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
