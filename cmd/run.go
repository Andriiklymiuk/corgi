package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
)

var omitItems []string

type runSummary struct {
	Started []runSummaryItem `json:"started"`
	Failed  []runSummaryItem `json:"failed"`
}

type runSummaryItem struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"` // "service" | "db_service"
	Port  int    `json:"port,omitempty"`
	Error string `json:"error,omitempty"`
}

// buildRunSummary lists what corgi attempts to launch, applying the same
// per-item skip rules as the launcher (manualRun db_services and services
// are excluded; manual services explicitly named in --services are kept).
func buildRunSummary(corgi *utils.CorgiCompose) runSummary {
	s := runSummary{Started: []runSummaryItem{}, Failed: []runSummaryItem{}}
	for _, db := range corgi.DatabaseServices {
		if db.ManualRun {
			continue
		}
		s.Started = append(s.Started, runSummaryItem{
			Name: db.ServiceName,
			Kind: "db_service",
			Port: db.Port,
		})
	}
	for _, svc := range corgi.Services {
		if shouldSkipManualRun(svc) {
			continue
		}
		s.Started = append(s.Started, runSummaryItem{
			Name: svc.ServiceName,
			Kind: "service",
			Port: svc.Port,
		})
	}
	return s
}

type detachedProc struct {
	name    string
	command string
	logFile string
	port    int
	pid     int
	pgid    int
	status  string
}

func buildDetachState(composePath string, procs []detachedProc, dbs []utils.RunStateEntry) utils.RunState {
	now := time.Now().UTC()
	services := make([]utils.RunStateEntry, 0, len(procs))
	for _, p := range procs {
		status := p.status
		if status == "" {
			status = "running"
		}
		services = append(services, utils.RunStateEntry{
			Name:            p.name,
			Kind:            "service",
			PID:             p.pid,
			PGID:            p.pgid,
			Port:            p.port,
			Command:         p.command,
			LogFile:         p.logFile,
			Status:          status,
			StartedAt:       now,
			StatusChangedAt: now,
		})
	}
	return utils.RunState{
		ComposePath: composePath,
		StartedAt:   now,
		Services:    services,
		DBServices:  dbs,
	}
}

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:     "run",
	Short:   "Run all databases and services",
	Long:    `This command helps to run all services and their dependent services.`,
	Run:     runRun,
	Aliases: []string{"start", "r"},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.PersistentFlags().BoolP(
		"seed",
		"s",
		false,
		"Seed all db_services that have seedSource or have dump.sql / dump.bak or other dump file in their folder",
	)
	runCmd.PersistentFlags().StringSliceVarP(
		&omitItems,
		"omit",
		"",
		[]string{},
		`Slice of parts of service to omit.

beforeStart - beforeStart in services is omitted.
afterStart - afterStart in services is omitted.

By default nothing is omitted
		`,
	)

	runCmd.PersistentFlags().StringSliceVarP(
		&utils.ServicesItemsFromFlag,
		"services",
		"",
		[]string{},
		`Slice of services to choose from.

If you provide at least 1 services here, than corgi will choose only this service, while ignoring all others.
none - will ignore all services run.
(--services app,server)

By default all services are included and run.
		`,
	)

	runCmd.PersistentFlags().StringSliceVarP(
		&utils.DbServicesItemsFromFlag,
		"dbServices",
		"",
		[]string{},
		`Slice of db_services to choose from.

If you provide at least 1 db_service here, than corgi will choose only this db_service, while ignoring all others.
none - will ignore all db_services run.
(--dbServices db,db1,db2)

By default all db_services are included and run.
		`,
	)
	runCmd.PersistentFlags().String(
		"profile",
		"",
		`Run only the named profile(s): services/db_services whose `+"`profiles:`"+`
list contains a requested value, plus the transitive depends_on closure (so a
profile still brings up the databases its services need, even if those
databases have no profiles tag). Accepts a comma-separated list for the union,
e.g. --profile backend,worker. Items with no profiles run only when no
--profile is passed (docker-compose behavior). An unknown profile starts
nothing. Composes with --services/--omit/--dbServices as an intersection
(profile narrows first). By default (no --profile) everything runs.`,
	)
	runCmd.PersistentFlags().BoolP(
		"pull",
		"",
		false,
		"Pull services repo changes",
	)
	runCmd.PersistentFlags().BoolP(
		"no-watch",
		"",
		false,
		"Dusable watch for changes in corgi-compose file",
	)
	runCmd.PersistentFlags().String(
		"host",
		"",
		`IP to use instead of "localhost" in service URL env vars (so a phone
on the LAN can hit your dev API). Pass an explicit IP or "auto" to
detect the first non-loopback IPv4. db_services stay on localhost.
		`,
	)
	runCmd.PersistentFlags().Bool(
		"tunnel",
		false,
		`Open public HTTPS tunnels alongside the stack for every service that
declares a tunnel: block in corgi-compose.yml. Services whose tunnel
hostname env vars (e.g. ${API_TUNNEL_HOST}) are unset are skipped with
a warning — corgi run keeps going. Equivalent to running corgi tunnel
in a second terminal, but bundled into one process. Auth still
required per provider (e.g. ngrok config add-authtoken).`,
	)
	runCmd.PersistentFlags().Bool(
		"ci",
		false,
		`CI mode: suppress spinners, banners, and color output.
Plain log lines only. Implies --silent. Auto-enabled when CI=true env is set.
Pair with --once for CI pipeline use: corgi run --once --ci`,
	)
	runCmd.PersistentFlags().Bool(
		"logs",
		false,
		`Persist stdout/stderr of every service and db_service to
corgi_services/.logs/<name>/<timestamp>.log.
Keeps the last 10 runs per service; older logs are pruned automatically.
Read them afterwards with: corgi logs`,
	)
	runCmd.PersistentFlags().BoolP(
		"detach",
		"d",
		false,
		`Start every service as a detached process group that survives corgi
exiting, persist run-state to corgi_services/.state.json, print a JSON
startup summary, and return immediately (no streaming, no watch).`,
	)
	runCmd.PersistentFlags().Bool(
		"force",
		false,
		`With --detach: ignore an existing run-state and start anyway,
removing the stale state file first.`,
	)
	runCmd.PersistentFlags().Bool(
		"notify",
		true,
		`Send a desktop notification when a service crashes unexpectedly.
Requires notifications to be enabled (answer yes in: corgi doctor).
Pass --notify=false to disable for a single run.`,
	)
	runCmd.PersistentFlags().Bool(
		"gate-deps",
		false,
		`Gate service startup on dependency readiness for every depends_on edge,
even ones without an explicit condition:. By default only edges that set
condition: ready|started are gated; without this flag (and without
condition:) services start in parallel as before.`,
	)
	runCmd.PersistentFlags().Bool(
		"dry-run",
		false,
		`Compute and print the start plan without any side effects: no make up,
no git clone, no process spawn, no .env writes. Runs validation first, then
reports the resolved start order and each service's port, dependencies,
generated env keys, and whether it would be cloned. Pair with --json for a
machine-readable plan. Exit 0 if valid, 1 if validation finds errors.`,
	)
	runCmd.PersistentFlags().Duration(
		"ready-timeout",
		defaultReadyTimeout,
		`Max time to wait for a database or dependency service to become ready
before proceeding anyway (non-fatal). Applies to readiness gating and the
database readiness probe.`,
	)
}

// defaultReadyTimeout bounds the wait for a db/dependency to become reachable
// before proceeding anyway. Shared by run, exec, test, and the mcp server.
const defaultReadyTimeout = 15 * time.Second

// Resolved --gate-deps / --ready-timeout for the current run, set by applyRunFlags.
var (
	gateDepsFlag bool
	readyTimeout = defaultReadyTimeout
)

// exitInProgress guards the terminal-exit path. Reset on cleanup-setup
// error so the next signal can retry.
var exitInProgress atomic.Bool

// runReloading is true while runRun is re-entered from a SIGHUP reload, so a
// config-load failure returns gracefully instead of exiting the whole process.
var runReloading atomic.Bool

func handleRunSignal(cmd *cobra.Command, s os.Signal) {
	if s == syscall.SIGHUP {
		fmt.Println("🔄 Reloading corgi, because of corgi-compose file changes")
		stopRunTunnels()
		utils.KillAllStoredProcesses()
		stopDockerRunners(utils.CorgiComposeFileContent)
		utils.CloseAllLogWriters()
		utils.ResetShutdown()
		runReloading.Store(true)
		cmd.Run(cmd, nil)
		runReloading.Store(false)
		return
	}
	if !exitInProgress.CompareAndSwap(false, true) {
		return
	}
	utils.RequestShutdown()
	fmt.Println("👋 Exiting corgi", s)
	stopRunTunnels()
	corgiLatestVersion, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println(err)
		exitInProgress.Store(false)
		return
	}
	// Kill start commands first so afterStart runs on a clean process
	// table — avoids races with mid-flight cleanup.
	utils.KillAllStoredProcesses()
	cleanup(corgiLatestVersion)
	utils.PrintFinalMessage()
	os.Exit(0)
}

func installSignalHandler(cmd *cobra.Command) func() {
	closeSignal := make(chan os.Signal, 1)
	signal.Notify(closeSignal, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for s := range closeSignal {
			handleRunSignal(cmd, s)
		}
	}()
	var stopOnce sync.Once
	return func() {
		stopOnce.Do(func() {
			signal.Stop(closeSignal)
			close(closeSignal)
			<-done
		})
	}
}

func setupComposeWatcher(cmd *cobra.Command) (*fsnotify.Watcher, error) {
	isNoWatch, err := cmd.Flags().GetBool("no-watch")
	if err != nil {
		return nil, err
	}
	if isNoWatch {
		return nil, nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("error initializing watcher: %w", err)
	}
	watchCorgiCompose(watcher, cmd)
	return watcher, nil
}

func usesDocker(corgi *utils.CorgiCompose) bool {
	if corgi.UseDocker {
		return true
	}
	for _, s := range corgi.Services {
		if s.Runner.Name == "docker" {
			return true
		}
	}
	return false
}

// readySignal carries two startup milestones a dependent may wait on: started
// (producer launched) and ready (readiness probe passed or timed out — closed
// either way so dependents never hang). The sync.Once guards make every close
// idempotent, since multiple goroutines may try to close the same channel.
type readySignal struct {
	started     chan struct{}
	ready       chan struct{}
	startedOnce sync.Once
	readyOnce   sync.Once
}

func (s *readySignal) markStarted() { s.startedOnce.Do(func() { close(s.started) }) }
func (s *readySignal) markReady()   { s.readyOnce.Do(func() { close(s.ready) }) }

func startAllServices(corgi *utils.CorgiCompose, cmd *cobra.Command) {
	var serviceWaitGroup sync.WaitGroup
	serviceWaitGroup.Add(len(corgi.Services))

	// Build the registry before launching any goroutine so every dependent can
	// find its producers' channels regardless of start order.
	signals := make(map[string]*readySignal, len(corgi.Services))
	for _, s := range corgi.Services {
		signals[s.ServiceName] = &readySignal{
			started: make(chan struct{}),
			ready:   make(chan struct{}),
		}
	}

	var startCmdPresent bool
	for _, service := range corgi.Services {
		go runService(service, cmd, &serviceWaitGroup, signals)
		if len(service.Start) != 0 {
			startCmdPresent = true
		}
	}

	if tunnelFlag, _ := cmd.Flags().GetBool("tunnel"); tunnelFlag {
		startTunnelsForRun(corgi.Services)
	}

	for startCmdPresent {
		time.Sleep(5 * 60 * time.Second)
		fmt.Println("😉 corgi is still running")
	}
	fmt.Println("No service or start command to run")
	serviceWaitGroup.Wait()
}

func resolveHostFlag(cmd *cobra.Command) error {
	raw, err := cmd.Flags().GetString("host")
	if err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		utils.HostOverride = ""
		return nil
	}
	if raw == "auto" {
		ip, err := utils.DetectHostIP()
		if err != nil {
			return fmt.Errorf("auto-detect: %w", err)
		}
		utils.HostOverride = ip
		utils.Info(art.BlueColor, "🌐 --host auto resolved to", ip, art.WhiteColor)
		return nil
	}
	utils.HostOverride = raw
	utils.Info(art.BlueColor, "🌐 --host override:", raw, art.WhiteColor)
	return nil
}

func runRun(cmd *cobra.Command, _ []string) {
	applyRunFlags(cmd)

	if err := resolveHostFlag(cmd); err != nil {
		fmt.Println(art.RedColor, "host flag:", err, art.WhiteColor)
		return
	}

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError(utils.ErrConfig, err.Error())
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		if runReloading.Load() {
			return
		}
		os.Exit(1)
	}

	// Single filter point: narrow services/db_services before anything reads them.
	// The --services/--omit/--dbServices filters then intersect this narrowed set.
	applyProfileFilter(cmd, corgi)

	// --dry-run branches before any side effect: plan only, then exit.
	if dryRun, _ := cmd.Flags().GetBool("dry-run"); dryRun {
		os.Exit(emitDryRunPlan(computeDryRunPlan(corgi)))
	}

	if CheckClonedReposExistence(corgi.Services) {
		CloneServices(corgi.Services)
	}

	detach, _ := cmd.Flags().GetBool("detach")

	if !detach {
		stopSignalHandler := installSignalHandler(cmd)
		defer stopSignalHandler()

		watcher, err := setupComposeWatcher(cmd)
		if err != nil {
			fmt.Println(err)
			return
		}
		if watcher != nil {
			defer watcher.Close()
		}
	}

	utils.CleanFromScratch(cmd, *corgi)
	runPreflight(cmd, corgi)
	runBeforeStart(corgi)

	CreateDatabaseServices(corgi.DatabaseServices)
	runDatabaseServices(cmd, corgi.DatabaseServices)

	if err := utils.GenerateEnvForServices(corgi); err != nil {
		fmt.Println(art.RedColor, "aborting corgi run:", err, art.WhiteColor)
		os.Exit(1)
	}

	if detach {
		runDetached(cmd, corgi)
		return
	}

	if logsEnabled, _ := cmd.Flags().GetBool("logs"); logsEnabled {
		setupLogWriters(corgi)
	}

	CreateServices(corgi.Services)
	if utils.ShutdownRequested() {
		return
	}
	if utils.JSONOutput {
		utils.PrintJSON(buildRunSummary(corgi))
	}
	startAllServices(corgi, cmd)
}

func runDetached(cmd *cobra.Command, corgi *utils.CorgiCompose) {
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	force, _ := cmd.Flags().GetBool("force")
	if blocked := detachAlreadyRunning(statePath, force); blocked {
		return
	}

	setupLogWriters(corgi)
	CreateServices(corgi.Services)
	if utils.ShutdownRequested() {
		return
	}

	procs := spawnDetachedServices(corgi)
	settleDetached(procs)
	dbs := detachedDBEntries(corgi)
	state := buildDetachState(utils.CorgiComposePath, procs, dbs)
	if err := utils.WriteRunState(statePath, state); err != nil {
		killDetached(procs)
		msg := "could not write run-state: " + err.Error()
		if utils.JSONOutput {
			utils.JSONError(utils.ErrExecFailed, msg)
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(1)
	}

	if utils.JSONOutput {
		utils.PrintJSON(state)
	} else {
		utils.Infof("🐶 corgi running detached — %d service(s), state: %s\n", len(procs), statePath)
	}
}

func detachAlreadyRunning(statePath string, force bool) bool {
	if _, err := os.Stat(statePath); err != nil {
		return false
	}
	prev, err := utils.ReadRunState(statePath)
	if err != nil {
		return false
	}
	prev = utils.ReconcileRunState(prev, utils.PidAlive, utils.ContainerRunning)
	if force {
		for _, s := range prev.Services {
			if s.Status == "running" {
				_ = stopProcessGroup(s)
			}
		}
		os.Remove(statePath)
		return false
	}
	for _, s := range prev.Services {
		if s.Status == "running" {
			msg := "corgi is already running for this project — stop or restart first (use --force to override)"
			if utils.JSONOutput {
				utils.JSONError(utils.ErrAlreadyRunning, msg)
			} else {
				fmt.Fprintln(os.Stderr, msg)
			}
			os.Exit(1)
		}
	}
	return false
}

func spawnDetachedServices(corgi *utils.CorgiCompose) []detachedProc {
	procs := []detachedProc{}
	for _, svc := range corgi.Services {
		if shouldSkipManualRun(svc) {
			continue
		}
		runDetachedBeforeStart(svc)

		// docker-runner services run as containers (no tracked pid); reconcile
		// and stop key off pid==0 and let cleanup bring them down.
		if svc.Runner.Name == "docker" && svc.Port != 0 {
			if err := utils.ExecuteServiceCommandRun(svc.ServiceName, "make", "up"); err != nil {
				fmt.Fprintln(os.Stderr, "failed to start", svc.ServiceName, ":", err)
				continue
			}
			procs = append(procs, detachedProc{
				name:    svc.ServiceName,
				command: "make up",
				logFile: utils.LogFilePath(svc.ServiceName),
				port:    svc.Port,
			})
			continue
		}

		if len(svc.Start) == 0 {
			continue
		}
		command := strings.Join(svc.Start, " && ")
		proc, err := utils.StartDetached(svc.ServiceName, command, svc.AbsolutePath, getServiceEnv(svc))
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to start", svc.ServiceName, ":", err)
			continue
		}
		procs = append(procs, detachedProc{
			name:    svc.ServiceName,
			command: command,
			logFile: utils.LogFilePath(svc.ServiceName),
			port:    svc.Port,
			pid:     proc.Pid,
			pgid:    proc.Pid,
		})
	}
	return procs
}

func runDetachedBeforeStart(svc utils.Service) {
	if svc.BeforeStart == nil || omitServiceCmd("beforeStart") {
		return
	}
	utils.RunServiceCommands(
		"beforeStart",
		svc.ServiceName,
		svc.BeforeStart,
		svc.AbsolutePath,
		false,
		false,
		getServiceEnv(svc),
	)
}

// settleDetached gives freshly spawned services a moment to crash, then records
// each one's real status so the state file doesn't claim a dead service is running.
func settleDetached(procs []detachedProc) {
	if len(procs) == 0 {
		return
	}
	time.Sleep(300 * time.Millisecond)
	for i := range procs {
		if procs[i].pid == 0 {
			continue
		}
		if utils.PidAlive(procs[i].pid, procs[i].command) {
			procs[i].status = "running"
		} else {
			procs[i].status = "crashed"
		}
	}
}

func killDetached(procs []detachedProc) {
	for _, p := range procs {
		if p.pgid > 0 {
			_ = utils.KillProcessGroup(p.pgid)
		}
	}
}

func detachedDBEntries(corgi *utils.CorgiCompose) []utils.RunStateEntry {
	dbs := []utils.RunStateEntry{}
	for _, db := range corgi.DatabaseServices {
		if db.ManualRun {
			continue
		}
		dbs = append(dbs, utils.RunStateEntry{
			Name:      db.ServiceName,
			Kind:      "db_service",
			Container: fmt.Sprintf("%s-%s", db.Driver, db.ServiceName),
			Port:      db.Port,
			Status:    "running",
		})
	}
	return dbs
}

// applyProfileFilter narrows services/db_services to those selected by --profile.
// No-op when empty. When nothing matches it selects nothing and warns, so a typo'd
// profile starts nothing rather than everything.
func applyProfileFilter(cmd *cobra.Command, corgi *utils.CorgiCompose) {
	raw, _ := cmd.Flags().GetString("profile")
	profiles := utils.ParseProfiles(raw)
	if len(profiles) == 0 {
		return
	}

	services, dbs := utils.SelectByProfiles(corgi, profiles)
	if len(services) == 0 && len(dbs) == 0 {
		// Select nothing — don't fall through to "select all".
		utils.Infof("⚠️  [%s] profile %q matches no services or db_services; nothing to run\n", utils.ErrUnknownProfile, raw)
	}

	filteredSvcs := corgi.Services[:0]
	for _, s := range corgi.Services {
		if services[s.ServiceName] {
			filteredSvcs = append(filteredSvcs, s)
		} else {
			utils.SkippedServices[s.ServiceName] = true
		}
	}
	corgi.Services = filteredSvcs

	filteredDbs := corgi.DatabaseServices[:0]
	for _, db := range corgi.DatabaseServices {
		if dbs[db.ServiceName] {
			filteredDbs = append(filteredDbs, db)
		} else {
			utils.SkippedDbServices[db.ServiceName] = true
		}
	}
	corgi.DatabaseServices = filteredDbs
}

func applyRunFlags(cmd *cobra.Command) {
	if ci, _ := cmd.Flags().GetBool("ci"); ci {
		utils.SetCIMode(true)
	}
	gateDepsFlag, _ = cmd.Flags().GetBool("gate-deps")
	if d, err := cmd.Flags().GetDuration("ready-timeout"); err == nil && d > 0 {
		readyTimeout = d
	}
	if notifyEnabled, _ := cmd.Flags().GetBool("notify"); notifyEnabled {
		utils.SetOnServiceCrash(func(serviceName string) {
			utils.Notify("corgi 🐶", fmt.Sprintf("Service %q crashed", serviceName))
		})
	} else {
		utils.SetOnServiceCrash(nil)
	}
}

func runPreflight(cmd *cobra.Command, corgi *utils.CorgiCompose) {
	if corgi.UseAwsVpn {
		if err := utils.AwsVpnInit(); err != nil {
			fmt.Println("AWS VPN init failed", err)
		}
	}
	if usesDocker(corgi) {
		if err := utils.DockerInit(cmd); err != nil {
			fmt.Println("Docker init failed:", err)
		}
	}
}

func runBeforeStart(corgi *utils.CorgiCompose) {
	utils.RunServiceCommands(
		utils.BeforeStartInConfig,
		"corgi beforeStart",
		corgi.BeforeStart,
		"",
		false,
		true,
	)
}

// stopDockerRunners brings down docker-runner containers so none outlives its
// config on shutdown or hot reload. Safe on nil.
func stopDockerRunners(corgi *utils.CorgiCompose) {
	if corgi == nil {
		return
	}
	utils.StopDockerRunnerServices(utils.DockerRunnerServiceNames(corgi.Services))
}

func cleanup(corgi *utils.CorgiCompose) {
	if len(corgi.DatabaseServices) != 0 {
		utils.ExecuteForEachService("stop")
	}

	stopDockerRunners(corgi)

	for _, service := range corgi.Services {
		if service.AfterStart != nil && !omitServiceCmd("afterStart") {
			utils.Info("\nAfter start commands:")
			utils.RunCleanupCommands(
				"afterStart",
				service.ServiceName,
				service.AfterStart,
				service.AbsolutePath,
				getServiceEnv(service),
			)
		}
	}

	utils.RunCleanupCommands(
		utils.AfterStartInConfig,
		"corgi afterStart",
		corgi.AfterStart,
		"",
		"",
	)

	utils.Info("\n👋 Exiting corgi")
	utils.CloseAllLogWriters()
	maybeHintNotifications()
}

// maybeHintNotifications nudges users to turn on desktop crash alerts.
// Stays quiet in CI and when notifications are already enabled.
func maybeHintNotifications() {
	if utils.CIMode {
		return
	}
	cfg, err := utils.LoadUserConfig()
	if err != nil || cfg.Notifications {
		return
	}
	utils.Infof("\n%s💡 Tip: get a desktop alert when a service crashes — run: corgi notifications on%s\n",
		art.CyanColor, art.WhiteColor)
}

func runDatabaseServices(cmd *cobra.Command, databaseServices []utils.DatabaseService) {
	if !hasDatabaseToRun(databaseServices) {
		utils.Info("No database service to run")
		return
	}

	if err := utils.DockerInit(cmd); err != nil {
		fmt.Println(err)
		return
	}

	for _, dbService := range databaseServices {
		startDatabaseIfNeeded(dbService)
	}

	isSeed, err := cmd.Flags().GetBool("seed")
	if err != nil {
		return
	}
	if isSeed {
		SeedAllDatabases(databaseServices)
	}
}

func hasDatabaseToRun(databaseServices []utils.DatabaseService) bool {
	if len(databaseServices) == 0 {
		return false
	}
	for _, dbService := range databaseServices {
		if !dbService.ManualRun {
			return true
		}
	}
	return false
}

func startDatabaseIfNeeded(dbService utils.DatabaseService) {
	if dbService.ManualRun {
		return
	}
	containerName := fmt.Sprintf("%s-%s", dbService.Driver, dbService.ServiceName)
	serviceIsRunning, err := utils.IsServiceRunning(containerName)
	if err != nil {
		fmt.Printf("Getting target service info failed: %s\n", err)
	}
	if serviceIsRunning {
		return
	}
	utils.Info(art.BlueColor, "\n🤖 Starting database", dbService.ServiceName, art.WhiteColor)
	if err := utils.ExecuteCommandRun(dbService.ServiceName, "make", "up"); err != nil {
		fmt.Println("Starting service failed", err)
	}
	// Bounded readiness probe (non-fatal on timeout so services still get a chance).
	ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
	defer cancel()
	if err := utils.WaitForDBReady(ctx, dbService); err != nil {
		utils.Infof("⚠️  %s\n", err)
	}
}

func shouldSkipManualRun(service utils.Service) bool {
	if !service.ManualRun {
		return false
	}
	if len(utils.ServicesItemsFromFlag) == 0 {
		utils.Info(service.ServiceName, "is not run, because it should be run manually (manualRun)")
		return true
	}
	if !utils.IsServiceIncludedInFlag(utils.ServicesItemsFromFlag, service.ServiceName) {
		utils.Info(service.ServiceName, "is not run, because it should be added manually")
		return true
	}
	return false
}

func runServicePullIfRequested(cobraCmd *cobra.Command, service utils.Service) {
	isPull, err := cobraCmd.Flags().GetBool("pull")
	if err != nil || !isPull {
		return
	}
	if err := utils.RunServiceCmd(
		service.ServiceName,
		"corgi pull --silent",
		service.AbsolutePath,
		true,
	); err != nil {
		fmt.Println("corgi pull failed for", service.ServiceName, "error:", err)
	}
}

func startServiceProcess(service utils.Service) {
	if service.Runner.Name == "docker" && service.Port != 0 {
		utils.Info(art.BlueColor, "\n🤖 Starting service", service.ServiceName, art.WhiteColor)
		if err := utils.ExecuteServiceCommandRun(service.ServiceName, "make", "up"); err != nil {
			fmt.Println("Starting service failed", err)
		}
		return
	}
	if service.Start != nil {
		utils.Info("\nStart commands:")
		utils.RunServiceCommands(
			"start",
			service.ServiceName,
			service.Start,
			service.AbsolutePath,
			true,
			service.InteractiveInput,
			getServiceEnv(service),
		)
	}
}

func runService(service utils.Service, cobraCmd *cobra.Command, serviceWaitGroup *sync.WaitGroup, signals map[string]*readySignal) {
	defer serviceWaitGroup.Done()

	sig := signals[service.ServiceName]
	// Close own milestones on any early return so dependents never hang.
	defer func() {
		if sig != nil {
			sig.markStarted()
			sig.markReady()
		}
	}()

	if utils.ShutdownRequested() {
		return
	}
	if shouldSkipManualRun(service) {
		return
	}

	waitForServiceDeps(service, signals)

	runServicePullIfRequested(cobraCmd, service)

	utils.Info(art.BlueColor, "🐶 RUNNING SERVICE", service.ServiceName, art.WhiteColor)

	if service.BeforeStart != nil && !omitServiceCmd("beforeStart") {
		utils.Info("\nBefore start commands:")
		utils.RunServiceCommands(
			"beforeStart",
			service.ServiceName,
			service.BeforeStart,
			service.AbsolutePath,
			false,
			false,
			getServiceEnv(service),
		)
	}

	if utils.ShutdownRequested() {
		return
	}

	// Mark started, then probe readiness in the background since
	// startServiceProcess blocks on the start command.
	if sig != nil {
		sig.markStarted()
		if service.Port == 0 {
			// Nothing to probe — dependents waiting on `ready` proceed at once.
			sig.markReady()
		} else {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
				defer cancel()
				_ = utils.WaitForServiceReady(ctx, service) // close even on timeout
				sig.markReady()
			}()
		}
	}

	startServiceProcess(service)
}

// waitForServiceDeps blocks until this service's gated dependencies reach their
// condition's milestone. An edge is gated only when it sets condition: or
// --gate-deps is passed; ungated edges keep the default parallel start. Bounded
// by readyTimeout.
func waitForServiceDeps(service utils.Service, signals map[string]*readySignal) {
	for _, dep := range service.DependsOnServices {
		gated := dep.Condition != "" || gateDepsFlag
		if !gated {
			continue
		}
		producer, ok := signals[dep.Name]
		if !ok {
			// Unknown dependency — `corgi validate` already flags these.
			continue
		}
		// condition: started waits only until corgi launched the producer;
		// "ready" (or empty under --gate-deps) waits for the readiness probe.
		ch := producer.ready
		if dep.Condition == "started" {
			ch = producer.started
		}
		select {
		case <-ch:
			emitDepReady(service.ServiceName, dep.Name, dep.Condition)
		case <-time.After(readyTimeout):
			emitDepTimeout(service.ServiceName, dep.Name)
		}
	}
}

func emitDepReady(service, dep, condition string) {
	if condition == "" {
		condition = "ready"
	}
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{
			"event":     "dep_ready",
			"service":   service,
			"dependsOn": dep,
			"condition": condition,
		})
		return
	}
	utils.Infof("⏳ %s dependency %s satisfied (%s)\n", service, dep, condition)
}

func emitDepTimeout(service, dep string) {
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{
			"event":     "dep_timeout",
			"code":      utils.ErrReadinessTimeout,
			"service":   service,
			"dependsOn": dep,
		})
		return
	}
	utils.Infof("⚠️  %s: %s waiting on %s — proceeding anyway\n", utils.ErrReadinessTimeout, service, dep)
}

func getServiceEnv(service utils.Service) string {
	if service.AutoSourceEnv != nil && !*service.AutoSourceEnv {
		return utils.SkipAutoSourceEnv
	}
	return service.EnvPath
}

func omitServiceCmd(cmdName string) bool {
	for _, s := range omitItems {
		if cmdName == s {
			return true
		}
	}
	return false
}

func handleComposeWriteEvent(watcher *fsnotify.Watcher, cmd *cobra.Command, eventName string) bool {
	oldCorgi := utils.CorgiComposeFileContent
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println(err)
		return true // stop watching on read error
	}
	if utils.CompareCorgiFiles(corgi, oldCorgi) {
		return false
	}
	fmt.Println("Detected corgi compose change in", eventName)
	watcher.Remove(utils.CorgiComposePath)
	utils.SendRestart()
	return false
}

func watchCorgiCompose(watcher *fsnotify.Watcher, cmd *cobra.Command) {
	fmt.Println("👀 Watching for changes in corgi-compose file")
	if err := watcher.Add(utils.CorgiComposePath); err != nil {
		fmt.Println("Error adding CorgiCompose to watcher:", err)
		return
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write != fsnotify.Write {
					continue
				}
				if handleComposeWriteEvent(watcher, cmd, event.Name) {
					return
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Println("Watcher error:", err)
			}
		}
	}()
}

// setupLogWriters creates per-service log files under corgi_services/.logs/
// and registers each writer in utils.ServiceLogWriters so that runManaged
// tees stdout/stderr to the file. Also ensures .gitignore excludes the dir.
// Closes any previously registered writers first so re-entry on SIGHUP
// reload does not leak file descriptors.
func setupLogWriters(corgi *utils.CorgiCompose) {
	utils.CloseAllLogWriters()
	base := filepath.Join(utils.CorgiComposePathDir, "corgi_services")
	if err := os.MkdirAll(base, 0o755); err != nil {
		fmt.Printf("⚠ logs: could not create %s: %v\n", base, err)
		return
	}
	utils.EnsureLogsGitignore(base)

	registerLog := func(name string) {
		w, err := utils.OpenLogWriter(base, name)
		if err != nil {
			fmt.Printf("⚠ logs: could not open log for %s: %v\n", name, err)
			return
		}
		if w != nil {
			utils.SetLogWriter(name, w)
		}
	}

	for _, svc := range corgi.Services {
		registerLog(svc.ServiceName)
	}
	for _, db := range corgi.DatabaseServices {
		if db.ManualRun {
			continue
		}
		registerLog(db.ServiceName)
		utils.FollowDatabaseLogs(db.Driver, db.ServiceName)
	}
}
