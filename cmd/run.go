package cmd

import (
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
	runCmd.PersistentFlags().Bool(
		"notify",
		true,
		`Send a desktop notification when a service crashes unexpectedly.
Requires notifications to be enabled (answer yes in: corgi doctor).
Pass --notify=false to disable for a single run.`,
	)
}

// exitInProgress guards the terminal-exit path. Reset on cleanup-setup
// error so the next signal can retry.
var exitInProgress atomic.Bool

func handleRunSignal(cmd *cobra.Command, s os.Signal) {
	if s == syscall.SIGHUP {
		fmt.Println("🔄 Reloading corgi, because of corgi-compose file changes")
		stopRunTunnels()
		utils.KillAllStoredProcesses()
		utils.CloseAllLogWriters()
		utils.ResetShutdown()
		cmd.Run(cmd, nil)
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

func startAllServices(corgi *utils.CorgiCompose, cmd *cobra.Command) {
	var serviceWaitGroup sync.WaitGroup
	serviceWaitGroup.Add(len(corgi.Services))
	var startCmdPresent bool
	for _, service := range corgi.Services {
		go runService(service, cmd, &serviceWaitGroup)
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
		fmt.Println(art.BlueColor, "🌐 --host auto resolved to", ip, art.WhiteColor)
		return nil
	}
	utils.HostOverride = raw
	fmt.Println(art.BlueColor, "🌐 --host override:", raw, art.WhiteColor)
	return nil
}

func runRun(cmd *cobra.Command, _ []string) {
	if ci, _ := cmd.Flags().GetBool("ci"); ci {
		utils.SetCIMode(true)
	}

	if notifyEnabled, _ := cmd.Flags().GetBool("notify"); notifyEnabled {
		utils.SetOnServiceCrash(func(serviceName string) {
			utils.Notify("corgi 🐶", fmt.Sprintf("Service %q crashed", serviceName))
		})
	} else {
		utils.SetOnServiceCrash(nil)
	}

	if err := resolveHostFlag(cmd); err != nil {
		fmt.Println(art.RedColor, "host flag:", err, art.WhiteColor)
		return
	}

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println(err)
		return
	}

	if CheckClonedReposExistence(corgi.Services) {
		CloneServices(corgi.Services)
	}

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

	utils.CleanFromScratch(cmd, *corgi)

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

	utils.RunServiceCommands(
		utils.BeforeStartInConfig,
		"corgi beforeStart",
		corgi.BeforeStart,
		"",
		false,
		true,
	)

	CreateDatabaseServices(corgi.DatabaseServices)
	runDatabaseServices(cmd, corgi.DatabaseServices)

	if err := utils.GenerateEnvForServices(corgi); err != nil {
		fmt.Println(art.RedColor, "aborting corgi run:", err, art.WhiteColor)
		os.Exit(1)
	}

	if logsEnabled, _ := cmd.Flags().GetBool("logs"); logsEnabled {
		setupLogWriters(corgi)
	}

	CreateServices(corgi.Services)
	// Bail if shutdown fired mid-init: spawning start cmds now would
	// orphan them past KillAllStoredProcesses.
	if utils.ShutdownRequested() {
		return
	}
	startAllServices(corgi, cmd)
}

func cleanup(corgi *utils.CorgiCompose) {
	if len(corgi.DatabaseServices) != 0 {
		utils.ExecuteForEachService("stop")
	}

	for _, service := range corgi.Services {
		if service.AfterStart != nil && !omitServiceCmd("afterStart") {
			fmt.Println("\nAfter start commands:")
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

	fmt.Println("\n👋 Exiting corgi")
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
	fmt.Printf("\n%s💡 Tip: get a desktop alert when a service crashes — run: corgi notifications on%s\n",
		art.CyanColor, art.WhiteColor)
}

func runDatabaseServices(cmd *cobra.Command, databaseServices []utils.DatabaseService) {
	if !hasDatabaseToRun(databaseServices) {
		fmt.Println("No database service to run")
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
	fmt.Println(art.BlueColor, "\n🤖 Starting database", dbService.ServiceName, art.WhiteColor)
	if err := utils.ExecuteCommandRun(dbService.ServiceName, "make", "up"); err != nil {
		fmt.Println("Starting service failed", err)
	}
	time.Sleep(time.Second * 3)
}

func shouldSkipManualRun(service utils.Service) bool {
	if !service.ManualRun {
		return false
	}
	if len(utils.ServicesItemsFromFlag) == 0 {
		fmt.Println(service.ServiceName, "is not run, because it should be run manually (manualRun)")
		return true
	}
	if !utils.IsServiceIncludedInFlag(utils.ServicesItemsFromFlag, service.ServiceName) {
		fmt.Println(service.ServiceName, "is not run, because it should be added manually")
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
		fmt.Println(art.BlueColor, "\n🤖 Starting service", service.ServiceName, art.WhiteColor)
		if err := utils.ExecuteServiceCommandRun(service.ServiceName, "make", "up"); err != nil {
			fmt.Println("Starting service failed", err)
		}
		return
	}
	if service.Start != nil {
		fmt.Println("\nStart commands:")
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

func runService(service utils.Service, cobraCmd *cobra.Command, serviceWaitGroup *sync.WaitGroup) {
	defer serviceWaitGroup.Done()
	if utils.ShutdownRequested() {
		return
	}
	if shouldSkipManualRun(service) {
		return
	}

	runServicePullIfRequested(cobraCmd, service)

	fmt.Println(art.BlueColor, "🐶 RUNNING SERVICE", service.ServiceName, art.WhiteColor)

	if service.BeforeStart != nil && !omitServiceCmd("beforeStart") {
		fmt.Println("\nBefore start commands:")
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
	startServiceProcess(service)
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
		registerLog(db.ServiceName)
	}
}
