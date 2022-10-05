package cmd

import (
	"fmt"
	"log"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
)

// dbCmd represents the db command
var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Database action helpers",
	Long: `
This is database generator helper, that is accessible from cli.
You can do db commands with the help of Makefile directly in the folder of
each service, but this is much easier to do it here.

	`,
	Run: runDb,
}

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.PersistentFlags().BoolP("stopAll", "s", false, "Stop all services")
	dbCmd.PersistentFlags().BoolP("removeAll", "r", false, "Remove all services")
	dbCmd.PersistentFlags().BoolP("upAll", "u", false, "Up all services, start all")
	dbCmd.PersistentFlags().BoolP("downAll", "d", false, "Down all services, stop and remove all")
}

func runDb(cobra *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices("corgi-compose.yml")
	if err != nil {
		fmt.Println(err)
		return
	}

	err = utils.DockerInit()
	if err != nil {
		fmt.Println(err)
		return
	}

	utils.CheckForFlagAndExecuteMake(cobra, "stopAll", "stop")
	utils.CheckForFlagAndExecuteMake(cobra, "removeAll", "remove")
	utils.CheckForFlagAndExecuteMake(cobra, "downAll", "down")
	utils.CheckForFlagAndExecuteMake(cobra, "upAll", "up")

	targetService, err := utils.GetTargetService()
	if err != nil {
		log.Println("Getting target service failed", err)
		return
	}

	serviceInfo, err := utils.GetServiceInfo(targetService)
	if err != nil {
		fmt.Printf("Getting target service info failed: %s", err)
	}
	fmt.Print(serviceInfo)
	serviceIsRunning, err := utils.GetStatusOfService(targetService)
	if err != nil {
		fmt.Printf("Getting target service info failed: %s\n", err)
	}
	if serviceIsRunning {
		fmt.Printf("%s is running 🟢\n", targetService)
	} else {
		fmt.Printf("%s isn't running 🔴\n", targetService)
	}

	serviceConfig, err := utils.GetDbServiceByName(targetService, corgi.DatabaseServices)
	if err != nil {
		log.Println("Getting target service config failed", err)
		return
	}

	showMakeCommands(
		cobra,
		args,
		targetService,
		serviceConfig,
	)
}

func showMakeCommands(
	cobra *cobra.Command,
	args []string,
	targetService string,
	serviceConfig utils.DatabaseService,
) {

	makeFileCommandsList, err := utils.GetMakefileCommandsInDirectory(targetService)
	if err != nil {
		log.Println("Getting Makefile commands failed", err)
		return
	}

	backString := "⬅️  go back"
	makeCommand, err := utils.PickItemFromListPrompt(
		"Select command",
		makeFileCommandsList,
		backString,
	)

	if err != nil {
		if err.Error() == backString {
			runDb(cobra, args)
			return
		}
		log.Println(
			fmt.Errorf("failed to choose make command %s", err),
		)
	}

	switch makeCommand {
	case "id":
		containerId, err := utils.GetContainerId(targetService)
		if err != nil {
			log.Println(err)
			break
		}
		fmt.Println("Container id: ", containerId)

	case "seed":
		SeedDb(targetService)
	case "getDump":
		GetDump(serviceConfig)
	default:
		_, err := utils.ExecuteMakeCommand(targetService, makeCommand)
		if err != nil {
			fmt.Println("Make command failed", err)
		}
	}
}

func SeedDb(targetService string) {
	serviceIsRunning, err := utils.GetStatusOfService(targetService)
	if err != nil {
		fmt.Printf("Getting target service info failed: %s\n", err)
	}
	dumpFileExists, err := utils.CheckIfFileExistsInDirectory(
		fmt.Sprintf("./%s/%s", utils.RootDbServicesFolder, targetService),
		"dump.sql",
	)

	if err != nil {
		fmt.Printf("Couldn't check for db dump file, error %s\n", err)
		return
	}
	if !dumpFileExists {
		fmt.Printf(
			"Db dump file doesn't exist in %s. Please add one its directory\n",
			targetService,
		)
		return
	}
	if !serviceIsRunning {
		_, err := utils.ExecuteMakeCommand(targetService, "up")
		if err != nil {
			fmt.Println("Make command failed", err)
		}
		time.Sleep(time.Second * 3)
	}

	containerId, err := utils.GetContainerId(targetService)
	if err != nil {
		log.Println(err)
		return
	}

	s := spinner.New(spinner.CharSets[70], 100*time.Millisecond)
	s.Suffix = fmt.Sprintf(" seeding of database in service %s", targetService)
	s.Start()

	output, err := utils.ExecuteSeedMakeCommand(
		targetService,
		"seed",
		fmt.Sprintf("c=%s", containerId),
	)

	s.Stop()
	if err != nil {
		fmt.Println("Make command failed", err)
		return
	}
	fmt.Println(string(output))
}

func GetDump(serviceConfig utils.DatabaseService) {
	err := utils.ExecuteCommandRun(
		serviceConfig.ServiceName,
		"make",
		"getDump",
		fmt.Sprintf("p=%s", serviceConfig.SeedFromDb.Password),
	)
	if err != nil {
		fmt.Println("Make command failed", err)
		return
	}

	fmt.Printf("✅ Successfully added database dump to %s\n", serviceConfig.ServiceName)
}
