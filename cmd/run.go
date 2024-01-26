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

	"github.com/spf13/cobra"
)

var omitItems []string

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run all databases and services",
	Long:  `This command helps to run all services and their dependent services.`,
	Run:   runRun,
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
	signal.Notify(closeSignal, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-closeSignal
		cleanup(corgi)
		utils.PrintFinalMessage()
		os.Exit(0)
	}()

	utils.CleanFromScratch(cmd, *corgi)

	CreateDatabaseServices(corgi.DatabaseServices)

	runDatabaseServices(cmd, corgi.DatabaseServices)

	utils.GenerateEnvForServices(corgi)

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
	fmt.Println("no Start cmd to run")
	serviceWaitGroup.Wait()
}

func cleanup(corgi *utils.CorgiCompose) {
	if len(corgi.DatabaseServices) != 0 {
		utils.ExecuteForEachService("stop")
	}
	for _, service := range corgi.Services {
		if service.AfterStart != nil && !omitServiceCmd("afterStart") {
			fmt.Println("\nAfter start commands:")
			for _, afterStartCmd := range service.AfterStart {
				err := utils.RunServiceCmd(service.ServiceName, afterStartCmd, service.Path)
				if err != nil {
					fmt.Println(
						art.RedColor,
						"aborting all other afterStart commands for ", service.ServiceName, ", because of ", err,
						art.WhiteColor,
					)
					break
				}
			}
		}
	}
	fmt.Println("\n👋 Exiting cli")
}

func runDatabaseServices(cmd *cobra.Command, databaseServices []utils.DatabaseService) {
	if len(databaseServices) == 0 {
		return
	}
	isSeed, err := cmd.Flags().GetBool("seed")
	if err != nil {
		return
	}

	err = utils.DockerInit(cmd)
	if err != nil {
		fmt.Println(err)
		return
	}

	if isSeed {
		SeedAllDatabases((databaseServices))
	}

	utils.ExecuteForEachService("up")
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
		err = utils.RunServiceCmd(service.ServiceName, "corgi pull --silent", service.Path)
		if err != nil {
			fmt.Println("corgi pull failed for", service.ServiceName, "error:", err)
		}
	}
	fmt.Println(art.BlueColor, "🐶 RUNNING SERVICE", service.ServiceName, art.WhiteColor)

	if service.BeforeStart != nil && !omitServiceCmd("beforeStart") {
		fmt.Println("\nBefore start commands:")
		for _, beforeStartCmd := range service.BeforeStart {
			err := utils.RunServiceCmd(service.ServiceName, beforeStartCmd, service.Path)
			if err != nil {
				fmt.Println(
					art.RedColor,
					"aborting all other beforeStart commands for ", service.ServiceName, ", because of ", err,
					art.WhiteColor,
				)
				return
			}
		}
	}
	if service.Start != nil {
		fmt.Println("\nStart commands:")
		for _, startCmd := range service.Start {
			go func(startCmd string) {
				err := utils.RunServiceCmd(service.ServiceName, startCmd, service.Path)
				if err != nil {
					fmt.Println(
						art.RedColor,
						"aborting ", service.ServiceName, "cmd ", startCmd, ", because of ", err,
						art.WhiteColor,
					)
					return
				}
			}(startCmd)
		}
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
