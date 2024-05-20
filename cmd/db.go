package cmd

import (
	"fmt"
	"log"
	"os"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"

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
	Run:     runDb,
	Aliases: []string{"database"},
}

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.PersistentFlags().BoolP("stopAll", "s", false, "Stop all database services")
	dbCmd.PersistentFlags().BoolP("removeAll", "r", false, "Remove all database services")
	dbCmd.PersistentFlags().BoolP("upAll", "u", false, "Up all database services, start all")
	dbCmd.PersistentFlags().BoolP("downAll", "d", false, "Down all database services, stop and remove all")
	dbCmd.PersistentFlags().BoolP("seedAll", "", false, "Seed all database services")
}

func runDb(cobra *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cobra)
	if err != nil {
		fmt.Println(err)
		return
	}

	CreateDatabaseServices(corgi.DatabaseServices)

	err = utils.DockerInit(cobra)
	if err != nil {
		fmt.Println(err)
		return
	}

	utils.CheckForFlagAndExecuteMake(cobra, "stopAll", "stop")
	utils.CheckForFlagAndExecuteMake(cobra, "removeAll", "remove")
	utils.CheckForFlagAndExecuteMake(cobra, "downAll", "down")
	utils.CheckForFlagAndExecuteMake(cobra, "upAll", "up")
	checkForSeedAllFlag(cobra, corgi.DatabaseServices)

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
		fmt.Printf("Getting target service status failed: %s\n", err)
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
		err = SeedDb(targetService)
		if err != nil {
			fmt.Println(err)
		}
	case "getDump":
		GetDump(serviceConfig, false)
	case "getSelfDump":
		GetDump(serviceConfig, true)
	default:
		err := utils.ExecuteCommandRun(targetService, "make", makeCommand)
		if err != nil {
			fmt.Println("Make command failed", err)
		}
	}
}

func SeedDb(targetService string) error {
	dumpFileExists, err := utils.CheckIfFilesExistsInDirectory(
		fmt.Sprintf("%s/%s/%s",
			utils.CorgiComposePathDir,
			utils.RootDbServicesFolder,
			targetService,
		),
		"dump.*",
	)
	if err != nil {
		return fmt.Errorf("error in checking dump file: %s", err)
	}
	if !dumpFileExists {
		return fmt.Errorf(
			"db dump file doesn't exist in %s. Please add one its directory",
			targetService,
		)
	}
	serviceIsRunning, err := utils.GetStatusOfService(targetService)
	if err != nil {
		fmt.Printf("Getting target service info failed: %s\n", err)
	}
	if !serviceIsRunning {
		err := utils.ExecuteCommandRun(targetService, "make", "up")
		if err != nil {
			fmt.Println("Make command failed", err)
		}
		time.Sleep(time.Second * 12)
	}

	containerId, err := utils.GetContainerId(targetService)
	if err != nil {
		return err
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
		return fmt.Errorf("make command failed: %s", err)
	}
	fmt.Println(string(output))
	return nil
}

// Get dump either from seedDb or from self, if isSelf true, that dump is from current db
func GetDump(serviceConfig utils.DatabaseService, isSelf bool) {
	var password string
	var cmdName string

	if isSelf {
		password = serviceConfig.Password
		cmdName = "getSelfDump"
	} else {
		password = serviceConfig.SeedFromDb.Password
		cmdName = "getDump"
	}

	err := utils.ExecuteCommandRun(
		serviceConfig.ServiceName,
		"make",
		cmdName,
		fmt.Sprintf("p=%s", password),
	)
	if err != nil {
		fmt.Println("Make command failed", err)
		return
	}

	fmt.Printf("✅ Successfully added database dump to %s\n", serviceConfig.ServiceName)
}

func DumpAndSeedDb(dbService utils.DatabaseService) error {
	if dbService.SeedFromFilePath != "" {
		src := dbService.SeedFromFilePath
		path, err := utils.GetPathToDbService(dbService.ServiceName)
		if err != nil {
			return fmt.Errorf("path to target service is not found: %s", err)
		}
		dumpFileName := utils.GetDumpFilename(dbService.Driver)

		dest := path + "/" + dumpFileName

		bytesRead, err := os.ReadFile(src)

		if err != nil {
			return err
		}

		err = os.WriteFile(dest, bytesRead, 0644)

		if err != nil {
			return err
		}
		fmt.Println(art.BlueColor, "⛅ DATABASE DUMP COPIED for", dbService.ServiceName, art.WhiteColor)
	}

	if (dbService.SeedFromDb != utils.SeedFromDb{} && dbService.SeedFromFilePath == "") {
		fmt.Println(art.BlueColor, "⛅ GETTING DATABASE DUMP for", dbService.ServiceName, art.WhiteColor)
		GetDump(dbService, false)
	}

	err := SeedDb(dbService.ServiceName)
	if err != nil {
		return err
	}
	fmt.Println(art.BlueColor, "🎉 ", dbService.ServiceName, " IS SEEDED", art.WhiteColor)
	return nil
}

func checkForSeedAllFlag(cmd *cobra.Command, databaseServices []utils.DatabaseService) {
	shouldSeedAllDatabases, err := cmd.Flags().GetBool("seedAll")
	if err != nil {
		return
	}

	if !shouldSeedAllDatabases {
		return
	}
	SeedAllDatabases(databaseServices)
}

func SeedAllDatabases(databaseServices []utils.DatabaseService) {
	for _, dbService := range databaseServices {
		err := DumpAndSeedDb(dbService)
		if err != nil {
			fmt.Println("Error dumping and seeding file", err)
		}
	}
}
