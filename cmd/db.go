package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
)

// runUpAll mirrors the --upAll path of CheckForFlagAndExecuteMake, adding an
// optional --wait readiness gate before the --runOnce exit.
func runUpAll(cmd *cobra.Command, dbs []utils.DatabaseService) {
	if up, _ := cmd.Flags().GetBool("upAll"); !up {
		return
	}
	utils.ExecuteForEachService("up")

	if wait, _ := cmd.Flags().GetBool("wait"); wait {
		ctx, cancel := context.WithTimeout(context.Background(), defaultReadyTimeout)
		defer cancel()
		// Explicit --wait gates: a timeout is a hard failure (useful in CI).
		if err := waitForDbsReady(ctx, dbs, utils.WaitForDBReady); err != nil {
			if utils.JSONOutput {
				utils.JSONError(utils.ErrReadinessTimeout, err.Error())
			} else {
				fmt.Fprintln(os.Stderr, "❌", err)
			}
			os.Exit(1)
		}
	}

	if once, _ := cmd.Root().Flags().GetBool("runOnce"); once {
		utils.PrintFinalMessage()
		os.Exit(0)
	}
}

// Wait for each db with a port to accept connections. ready is injected for tests.
func waitForDbsReady(ctx context.Context, dbs []utils.DatabaseService, ready func(context.Context, utils.DatabaseService) error) error {
	for _, db := range dbs {
		if db.Port == 0 {
			continue
		}
		if err := ready(ctx, db); err != nil {
			return fmt.Errorf("%s not ready: %w", db.ServiceName, err)
		}
	}
	return nil
}

const errMakeCommandFailed = "Make command failed"

// seedReady waits for a db to accept connections before seeding. Indirected
// through a var so tests can stub the readiness probe.
var seedReady = utils.WaitForDBReady

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
	dbCmd.PersistentFlags().Bool("wait", false, "With --upAll: block until each db with a port accepts connections")
	dbCmd.PersistentFlags().BoolP("downAll", "d", false, "Down all database services, stop and remove all")
	dbCmd.PersistentFlags().BoolP("seedAll", "", false, "Seed all database services")
	dbShellCmd.Flags().StringP("exec", "e", "",
		"Run a single query/command non-interactively and exit.\n"+
			"For redis-family drivers the query is split on whitespace, so quoted\n"+
			"arguments containing spaces (e.g. SET foo \"hello world\") are not\n"+
			"preserved — wrap them in a script file or use the interactive shell.")
	dbCmd.AddCommand(dbShellCmd)

	dbSnapshotCmd.Flags().BoolVar(&snapList, "list", false, "List snapshots for the service")
	dbSnapshotCmd.Flags().StringVar(&snapRM, "rm", "", "Delete a snapshot by name (its .tar.zst + .meta.json)")
	dbSnapshotCmd.Flags().BoolVar(&snapForce, "force", false, "Overwrite an existing snapshot")
	dbRestoreCmd.Flags().BoolVarP(&restoreYes, "yes", "y", false, "Skip the destructive-wipe confirmation")
	dbRestoreCmd.Flags().BoolVar(&restoreForce, "force", false, "Override a version/arch/image mismatch")
	dbCmd.AddCommand(dbSnapshotCmd)
	dbCmd.AddCommand(dbRestoreCmd)
}

// dbShellCmd opens an interactive shell inside the running container for a
// db_service. Credentials are read from the generated env so the user never
// has to copy-paste passwords.
var dbShellCmd = &cobra.Command{
	Use:   "shell [service-name]",
	Short: "Open an interactive shell for a db_service",
	Long: `Open an interactive shell inside the running container for a db_service.

Credentials are sourced from the corgi-compose config so you don't have to
copy-paste passwords. The shell command is chosen per driver:
  postgres / postgis / pgvector / timescaledb → psql
  redis / keydb / dragonfly / redict / valkey  → redis-cli
  mongodb                                       → mongosh
  mysql / mariadb                               → mysql
  mssql                                         → sqlcmd
  cassandra / scylla                            → cqlsh

The container must already be running (start it with: corgi run).

Examples:
  corgi db shell                                      # interactive picker
  corgi db shell postgres                             # open psql for "postgres"
  corgi db shell postgres -e "SELECT count(*) FROM u" # run one query, exit`,
	Run: runDbShell,
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
	runUpAll(cobra, corgi.DatabaseServices)
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

	serviceConfig, err := utils.GetDbServiceByName(
		targetService,
		corgi.DatabaseServices,
	)
	if err != nil {
		log.Println("Getting target service config failed", err)
		return
	}
	serviceToCheck := fmt.Sprintf(
		"%s-%s",
		serviceConfig.Driver,
		serviceConfig.ServiceName,
	)
	serviceIsRunning, err := utils.IsServiceRunning(
		serviceToCheck,
	)
	if err != nil {
		fmt.Printf("Getting target service status failed: %s\n", err)
	}
	if serviceIsRunning {
		fmt.Printf("%s is running 🟢\n", targetService)
	} else {
		fmt.Printf("%s isn't running 🔴\n", targetService)
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
		err = SeedDb(serviceConfig)
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
			fmt.Println(errMakeCommandFailed, err)
		}
	}
}

func SeedDb(dbService utils.DatabaseService) error {
	targetService := dbService.ServiceName
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
		if err := utils.ExecuteCommandRun(targetService, "make", "up"); err != nil {
			return fmt.Errorf("%s: %w", errMakeCommandFailed, err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultReadyTimeout)
		defer cancel()
		if err := seedReady(ctx, utils.DatabaseService{ServiceName: targetService}); err != nil {
			return fmt.Errorf("%s not ready for seeding: %w", targetService, err)
		}
	}

	containerId, err := utils.GetContainerId(targetService)
	if err != nil {
		return err
	}

	s := spinner.New(spinner.CharSets[70], 100*time.Millisecond)
	s.Suffix = fmt.Sprintf(" seeding of database in service %s", targetService)
	if !utils.CIMode {
		s.Start()
	}

	if dbService.Driver == "postgres" {
		// Postgres seed runs via exec.Command argv so the container id is never
		// re-expanded through the `make`/`/bin/sh` recipe.
		serviceDir, perr := utils.GetPathToDbService(targetService)
		if perr != nil {
			if !utils.CIMode {
				s.Stop()
			}
			return perr
		}
		err = utils.RunPgSeed(serviceDir, "dump.sql", containerId, dbService)
		if !utils.CIMode {
			s.Stop()
		}
		if err != nil {
			return fmt.Errorf("seed failed: %s", err)
		}
		return nil
	}

	output, err := utils.ExecuteSeedMakeCommand(
		targetService,
		"seed",
		fmt.Sprintf("c=%s", containerId),
	)

	if !utils.CIMode {
		s.Stop()
	}
	if err != nil {
		return fmt.Errorf("make command failed: %s", err)
	}
	fmt.Println(string(output))
	return nil
}

// Get dump either from seedDb or from self, if isSelf true, that dump is from current db
func GetDump(serviceConfig utils.DatabaseService, isSelf bool) {
	if serviceConfig.Driver == "postgres" {
		// Postgres dumps run via exec.Command argv with PGPASSWORD in cmd.Env,
		// so the password never lands on argv or in a re-expanding make recipe.
		source := serviceConfig
		if !isSelf {
			s := serviceConfig.SeedFromDb
			source = utils.DatabaseService{
				Host: s.Host, Port: s.Port, User: s.User,
				Password: s.Password, DatabaseName: s.DatabaseName,
				Driver: "postgres",
			}
		}
		serviceDir, err := utils.GetPathToDbService(serviceConfig.ServiceName)
		if err != nil {
			fmt.Println(errMakeCommandFailed, err)
			return
		}
		if err := utils.RunPgDump(source, serviceDir, "dump.sql"); err != nil {
			fmt.Println(errMakeCommandFailed, err)
			return
		}
		fmt.Printf("✅ Successfully added database dump to %s\n", serviceConfig.ServiceName)
		return
	}

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
		fmt.Sprintf("p='%s'", password),
	)
	if err != nil {
		fmt.Println(errMakeCommandFailed, err)
		return
	}

	fmt.Printf("✅ Successfully added database dump to %s\n", serviceConfig.ServiceName)
}

func DumpAndSeedDb(dbService utils.DatabaseService) error {
	if dbService.SeedFromFilePath != "" {
		src := dbService.SeedFromFilePath
		// A compose-relative seedFromFilePath must stay under the compose dir; an
		// absolute path is taken as-is (existing behavior).
		if !filepath.IsAbs(src) {
			resolved, err := utils.JoinUnderComposeDir(src)
			if err != nil {
				return err
			}
			src = resolved
		}
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

		err = os.WriteFile(dest, bytesRead, 0o600)

		if err != nil {
			return err
		}
		fmt.Println(art.BlueColor, "⛅ DATABASE DUMP COPIED for", dbService.ServiceName, art.WhiteColor)
	}

	if (dbService.SeedFromDb != utils.SeedFromDb{} && dbService.SeedFromFilePath == "") {
		fmt.Println(art.BlueColor, "⛅ GETTING DATABASE DUMP for", dbService.ServiceName, art.WhiteColor)
		GetDump(dbService, false)
	}

	err := SeedDb(dbService)
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

func requireServiceForDBShell(service string, nonInteractive bool, available []string) error {
	if service != "" || !nonInteractive {
		return nil
	}
	return fmt.Errorf("no terminal for the db service picker; pass the service name as an argument (available: %s)",
		strings.Join(available, ", "))
}

func runDbShell(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		utils.Info(err)
		return
	}

	if len(corgi.DatabaseServices) == 0 {
		utils.Info("No db_services defined in corgi-compose.yml")
		return
	}

	var targetName string
	if len(args) > 0 {
		targetName = args[0]
	} else {
		if utils.NonInteractive {
			available := make([]string, len(corgi.DatabaseServices))
			for i, db := range corgi.DatabaseServices {
				available[i] = db.ServiceName
			}
			if err := requireServiceForDBShell(targetName, true, available); err != nil {
				if utils.JSONOutput {
					utils.JSONError(utils.ErrInteractiveReq, err.Error())
				} else {
					fmt.Fprintln(os.Stderr, err)
				}
				os.Exit(2)
			}
		}
		labels := make([]string, len(corgi.DatabaseServices))
		labelToName := make(map[string]string, len(corgi.DatabaseServices))
		for i, db := range corgi.DatabaseServices {
			label := fmt.Sprintf("%s (%s)", db.ServiceName, db.Driver)
			labels[i] = label
			labelToName[label] = db.ServiceName
		}
		chosen, err := utils.PickItemFromListPrompt("Select db_service", labels, "⬅️  cancel")
		if err != nil {
			fmt.Println(err)
			return
		}
		targetName = labelToName[chosen]
	}

	dbService, err := utils.GetDbServiceByName(targetName, corgi.DatabaseServices)
	if err != nil {
		utils.Infof("db_service %q not found: %v\n", targetName, err)
		return
	}

	query, _ := cmd.Flags().GetString("exec")
	if query != "" {
		if utils.JSONOutput {
			out, qerr := utils.ExecDBQueryCapture(dbService, query)
			if qerr != nil {
				utils.JSONError(utils.ErrExecFailed, qerr.Error())
				os.Exit(1)
			}
			utils.PrintJSON(dbQueryResult{Service: dbService.ServiceName, Output: out})
			return
		}
		if err := utils.ExecDBQuery(dbService, query); err != nil {
			fmt.Printf("%s❌ Query failed: %v%s\n", art.RedColor, err, art.WhiteColor)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("%s🐚 Opening %s shell for %s...%s\n",
		art.CyanColor, dbService.Driver, dbService.ServiceName, art.WhiteColor)

	if err := utils.OpenDBShell(dbService); err != nil {
		fmt.Printf("%s❌ Shell error: %v%s\n", art.RedColor, err, art.WhiteColor)
	}
}
