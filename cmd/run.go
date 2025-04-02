package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
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
}

func runRun(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println(err)
		return
	}

	if CheckClonedReposExistence(corgi.Services) {
		CloneServices(corgi.Services)
	}

	closeSignal := make(chan os.Signal, 1)
	signal.Notify(closeSignal, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	go func(cmd *cobra.Command) {
		for {
			s := <-closeSignal
			switch s {
			case syscall.SIGHUP:
				fmt.Println("🔄 Reloading corgi, because of corgi-compose file changes")

				utils.KillAllStoredProcesses()
				cmd.Run(cmd, nil)
			default:
				fmt.Println("👋 Exiting corgi", s)
				corgiLatestVersion, err := utils.GetCorgiServices(cmd)
				if err != nil {
					fmt.Println(err)
					return
				}
				cleanup(corgiLatestVersion)
				utils.KillAllStoredProcesses()
				utils.PrintFinalMessage()
				os.Exit(0)
			}
		}
	}(cmd)

	isNoWatch, err := cmd.Flags().GetBool("no-watch")
	if err != nil {
		return
	}

	if !isNoWatch {
		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			fmt.Println("Error initializing watcher:", err)
			return
		}
		defer watcher.Close()
		watchCorgiCompose(watcher, cmd)
	}

	utils.CleanFromScratch(cmd, *corgi)

	if corgi.UseAwsVpn {
		err = utils.AwsVpnInit()
		if err != nil {
			fmt.Println("AWS VPN init failed", err)
		}
	}

	if corgi.UseDocker || func() bool {
		for _, s := range corgi.Services {
			if s.Runner.Name == "docker" {
				return true
			}
		}
		return false
	}() {
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
		false,
	)

	CreateDatabaseServices(corgi.DatabaseServices)

	runDatabaseServices(cmd, corgi.DatabaseServices)

	utils.GenerateEnvForServices(corgi)

	CreateServices(corgi.Services)

	var serviceWaitGroup sync.WaitGroup
	serviceWaitGroup.Add(len(corgi.Services))
	var startCmdPresent bool
	for _, service := range corgi.Services {
		go runService(service, cmd, &serviceWaitGroup)
		if len(service.Start) != 0 {
			startCmdPresent = true
		}
	}

	for startCmdPresent {
		time.Sleep(5 * 60 * time.Second)
		fmt.Println("😉 corgi is still running")
	}
	fmt.Println("No service or start command to run")
	serviceWaitGroup.Wait()
}

func cleanup(corgi *utils.CorgiCompose) {
	if len(corgi.DatabaseServices) != 0 {
		utils.ExecuteForEachService("stop")
	}

	for _, service := range corgi.Services {
		if service.AfterStart != nil && !omitServiceCmd("afterStart") {
			fmt.Println("\nAfter start commands:")
			utils.RunServiceCommands(
				"afterStart",
				service.ServiceName,
				service.AfterStart,
				service.AbsolutePath,
				false,
				false,
			)
		}
	}

	utils.RunServiceCommands(
		utils.AfterStartInConfig,
		"corgi afterStart",
		corgi.AfterStart,
		"",
		false,
		false,
	)

	fmt.Println("\n👋 Exiting corgi")
}

func runDatabaseServices(cmd *cobra.Command, databaseServices []utils.DatabaseService) {
	isThereDatabaseToRun := false
	for _, dbService := range databaseServices {
		if !dbService.ManualRun {
			isThereDatabaseToRun = true
			break
		}
	}

	if !isThereDatabaseToRun || len(databaseServices) == 0 {
		fmt.Println("No database service to run")
		return
	}

	err := utils.DockerInit(cmd)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, dbService := range databaseServices {
		if dbService.ManualRun {
			continue
		}

		serviceIsRunning, err := utils.IsServiceRunning(
			fmt.Sprintf(
				"%s-%s",
				dbService.Driver,
				dbService.ServiceName,
			),
		)
		if err != nil {
			fmt.Printf("Getting target service info failed: %s\n", err)
		}
		if !serviceIsRunning {
			fmt.Println(art.BlueColor, "\n🤖 Starting database", dbService.ServiceName, art.WhiteColor)
			err := utils.ExecuteCommandRun(dbService.ServiceName, "make", "up")
			if err != nil {
				fmt.Println("Starting service failed", err)
			}
			time.Sleep(time.Second * 3)
		}
	}

	isSeed, err := cmd.Flags().GetBool("seed")
	if err != nil {
		return
	}
	if isSeed {
		SeedAllDatabases((databaseServices))
	}

}

func runService(service utils.Service, cobraCmd *cobra.Command, serviceWaitGroup *sync.WaitGroup) {
	defer serviceWaitGroup.Done()
	if service.ManualRun {
		if len(utils.ServicesItemsFromFlag) == 0 {
			fmt.Println(service.ServiceName, "is not run, because it should be run manually (manualRun)")
			return
		}
		if !utils.IsServiceIncludedInFlag(utils.ServicesItemsFromFlag, service.ServiceName) {
			fmt.Println(service.ServiceName, "is not run, because it should be added manually")
			return
		}
	}
	isPull, err := cobraCmd.Flags().GetBool("pull")
	if err != nil {
		return
	}
	if isPull {
		err = utils.RunServiceCmd(
			service.ServiceName,
			"corgi pull --silent",
			service.AbsolutePath,
			true,
		)
		if err != nil {
			fmt.Println("corgi pull failed for", service.ServiceName, "error:", err)
		}
	}
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
		)
	}

	if service.Runner.Name == "docker" && service.Port != 0 {
		go func() {
			fmt.Println(art.BlueColor, "\n🤖 Starting service", service.ServiceName, art.WhiteColor)
			err = utils.ExecuteServiceCommandRun(service.ServiceName, "make", "up")
			if err != nil {
				fmt.Println("Starting service failed", err)
			}
		}()
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
		)
	}
}

func omitServiceCmd(cmdName string) bool {
	for _, s := range omitItems {
		if cmdName == s {
			return true
		}
	}
	return false
}

func watchCorgiCompose(
	watcher *fsnotify.Watcher,
	cmd *cobra.Command,
) {
	fmt.Println("👀 Watching for changes in corgi-compose file")
	err := watcher.Add(utils.CorgiComposePath)
	if err != nil {
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
				if event.Op&fsnotify.Write == fsnotify.Write {
					oldCorgi := utils.CorgiComposeFileContent
					corgi, err := utils.GetCorgiServices(cmd)
					if err != nil {
						fmt.Println(err)
						return
					}
					if utils.CompareCorgiFiles(corgi, oldCorgi) {
						continue
					}
					fmt.Println("Detected corgi compose change in", event.Name)
					watcher.Remove(utils.CorgiComposePath)
					utils.SendRestart()
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
