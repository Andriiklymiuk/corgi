package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

// mcpCmd runs corgi as a long-lived MCP server over stdio. MCP clients
// (Claude Code, Claude Desktop) spawn `corgi mcp` as a subprocess and talk
// JSON-RPC over stdin/stdout, so stdout must stay a pure protocol channel.
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
	rootCmd.AddCommand(mcpCmd)
}

// mcpHandlerMu serializes all MCP tool/resource work. mcp-go's stdio server
// runs a worker pool, so handlers can be invoked concurrently; corgi's handlers
// mutate shared global state (os.Stdout swap in withStdoutToStderr, plus
// rootCmd flags and utils.CorgiComposePath* in loadComposeCtx) that is not safe
// to touch from multiple goroutines. Serializing is fine here: corgi mcp is a
// single-user dev tool and handlers are short. Hold this across the ENTIRE
// handler body so the stdout swap and compose/flag mutation never overlap.
var mcpHandlerMu sync.Mutex

func runMCP(_ *cobra.Command, _ []string) {
	// stdout is the JSON-RPC channel: force non-interactive (no prompts) and
	// route corgi's own human/JSON logging away from stdout. JSONOutput=true
	// makes utils.Info* write to stderr, keeping stdout protocol-pure.
	utils.NonInteractive = true
	utils.JSONOutput = true

	s := server.NewMCPServer("corgi", APP_VERSION)
	registerMCPTools(s)
	registerMCPResources(s)

	// WithWorkerPoolSize(1) is defense-in-depth: it makes the stdio server
	// process tool calls one-at-a-time. The mcpHandlerMu mutex is the real
	// guard (it also covers resource handlers, which the pool size doesn't).
	if err := server.ServeStdio(s, server.WithWorkerPoolSize(1)); err != nil {
		fmt.Fprintln(os.Stderr, "mcp server error:", err)
		os.Exit(1)
	}
}

// composeContext bundles a loaded compose with the throwaway cobra command it
// was loaded through (so callers that need cobra flags — e.g. the run path —
// can pass it) plus a cleanup to detach the command from rootCmd.
type composeContext struct {
	corgi   *utils.CorgiCompose
	cmd     *cobra.Command
	cleanup func()
}

// loadComposeCtx loads and interpolates the compose the same way commands do,
// reusing utils.GetCorgiServices. MCP runs outside cobra's flag context, so we
// attach a throwaway command to rootCmd (inheriting -f/--filename etc.) and set
// the optional composePath as the filename. Empty path => default resolution
// from cwd. Caller must defer ctx.cleanup().
func loadComposeCtx(composePath string) (composeContext, error) {
	// GetCorgiServices reads filename/fromTemplate/etc. off cmd.Root().Flags().
	// Those are rootCmd's persistent flags, which cobra only merges into Flags()
	// during Execute(). MCP never runs Execute, so merge them explicitly here.
	rootCmd.Flags().AddFlagSet(rootCmd.PersistentFlags())

	tmp := &cobra.Command{Use: "mcp-load"}
	rootCmd.AddCommand(tmp)
	// determineCorgiComposePath reads --global off the command directly (not
	// Root), so it must be a local flag here.
	tmp.Flags().Bool("global", false, "")
	// runDatabaseServices reads --seed off the command (used by corgi_up).
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

// loadComposeForMCP is the common case: just the parsed compose.
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

// mcpUp always starts DETACHED so the tool returns promptly. It mirrors the
// foreground run prelude (clone, docker preflight, beforeStart, create+start
// databases, generate env) and then runDetached's state machine
// (CreateServices -> spawn -> write .state.json), returning the run-state.
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
		return utils.RunState{}, fmt.Errorf("ALREADY_RUNNING: corgi is already running for this project — call corgi_down first")
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
		return utils.RunState{}, fmt.Errorf("%s: %v", utils.ErrExecFailed, envErr)
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
		return logsResult{}, fmt.Errorf("%s: %v", utils.ErrExecFailed, err)
	}
	return logsResult{Service: args.Service, Lines: lines}, nil
}

// tailLogFile returns the last n lines of a log file, stripping the leading
// timestamp prefix when present (same convention as `corgi logs`).
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

// mcpExec runs a one-off command in a service's resolved env, capturing
// combined stdout+stderr into a buffer (so nothing leaks to the protocol
// channel) and returning the child exit code.
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
		if err := ensureServiceDeps(corgi, *service, 60*time.Second); err != nil {
			return execResult{}, fmt.Errorf("%s: %v", utils.ErrReadinessTimeout, err)
		}
	}

	var (
		buf  bytes.Buffer
		code int
		err2 error
	)
	start := time.Now()
	// Capture combined child output into buf; run with stdout redirected so any
	// incidental prints from the env/runner setup stay off the JSON-RPC channel.
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

func mcpSchema() string { return utils.ComposeJSONSchema() }

// filterByProfile narrows corgi to a profile's selection (members + their
// transitive depends_on closure), mirroring applyProfileFilter without cobra.
func filterByProfile(corgi *utils.CorgiCompose, profile string) {
	services, dbs := utils.SelectByProfile(corgi, profile)
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
	return fmt.Errorf("%s: %v", utils.ErrComposeNotFound, err)
}

// withStdoutToStderr runs fn with os.Stdout temporarily pointed at os.Stderr.
// The side-effecting run/stop paths print progress via fmt.Println to stdout;
// the MCP stdio server captured the real stdout at ServeStdio time, so this
// keeps that incidental output off the JSON-RPC channel without losing it.
func withStdoutToStderr(fn func()) {
	orig := os.Stdout
	os.Stdout = os.Stderr
	defer func() { os.Stdout = orig }()
	fn()
}

// --- MCP tool registration (thin wrappers over the cores above) ---

func registerMCPTools(s *server.MCPServer) {
	composeOpt := mcp.WithString("composePath", mcp.Description("Path to corgi-compose.yml (default: resolve from cwd)"))

	s.AddTool(mcp.NewTool("corgi_validate",
		mcp.WithDescription("Statically validate corgi-compose.yml (no side effects). Returns {ok, errors[], warnings[]}."),
		composeOpt,
	), jsonHandler(func(r mcp.CallToolRequest) (any, error) {
		return mcpValidate(validateArgs{ComposePath: r.GetString("composePath", "")})
	}))

	s.AddTool(mcp.NewTool("corgi_plan",
		mcp.WithDescription("Compute the dry-run plan: start order, databases, services, validation. No side effects."),
		composeOpt,
		mcp.WithString("profile", mcp.Description("Run only this profile's services/db_services")),
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
		mcp.WithString("profile", mcp.Description("Run only this profile's services/db_services")),
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
		mcp.WithString("service", mcp.Required(), mcp.Description("Service name")),
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
		mcp.WithString("service", mcp.Required(), mcp.Description("Service name")),
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

	s.AddTool(mcp.NewTool("corgi_schema",
		mcp.WithDescription("Return the JSON Schema (draft-07) for corgi-compose.yml."),
	), func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		mcpHandlerMu.Lock()
		defer mcpHandlerMu.Unlock()
		return mcp.NewToolResultText(mcpSchema()), nil
	})
}

// jsonHandler wraps a typed core into an MCP tool handler: it marshals the
// result to JSON text, and converts a returned error into an MCP tool error
// carrying the stable code already embedded in the message.
func jsonHandler(core func(mcp.CallToolRequest) (any, error)) server.ToolHandlerFunc {
	return func(_ context.Context, r mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Serialize the whole handler: the cores below mutate global state
		// (stdout swap, compose/flag globals) that isn't concurrency-safe.
		mcpHandlerMu.Lock()
		defer mcpHandlerMu.Unlock()
		out, err := core(r)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b, merrr := json.Marshal(out)
		if merrr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("%s: marshal result: %v", utils.ErrExecFailed, merrr)), nil
		}
		return mcp.NewToolResultText(string(b)), nil
	}
}

// --- MCP resources ---

func registerMCPResources(s *server.MCPServer) {
	s.AddResource(
		mcp.NewResource("corgi://schema", "corgi compose JSON Schema",
			mcp.WithResourceDescription("JSON Schema (draft-07) for corgi-compose.yml"),
			mcp.WithMIMEType("application/json")),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			mcpHandlerMu.Lock()
			defer mcpHandlerMu.Unlock()
			return []mcp.ResourceContents{mcp.TextResourceContents{
				URI: "corgi://schema", MIMEType: "application/json", Text: utils.ComposeJSONSchema(),
			}}, nil
		})

	s.AddResource(
		mcp.NewResource("corgi://compose", "current corgi compose",
			mcp.WithResourceDescription("Resolved/interpolated corgi-compose.yml as JSON"),
			mcp.WithMIMEType("application/json")),
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
				URI: "corgi://compose", MIMEType: "application/json", Text: string(b),
			}}, nil
		})

	s.AddResource(
		mcp.NewResource("corgi://status", "live status snapshot",
			mcp.WithResourceDescription("Live health snapshot of declared services and db_services"),
			mcp.WithMIMEType("application/json")),
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
				URI: "corgi://status", MIMEType: "application/json", Text: string(b),
			}}, nil
		})
}
