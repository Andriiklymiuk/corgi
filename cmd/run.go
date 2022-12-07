package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

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
		"Seed all db_services that have seedSource or have dump.sql in their folder",
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
		"Git pull services repo changes",
	)
}

func runRun(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Println(err)
		return
	}

	closeSignal := make(chan os.Signal, 1)
	signal.Notify(closeSignal, syscall.SIGINT, syscall.SIGTERM)
	runCmdDone := make(chan bool, 1)

	go func() {
		<-closeSignal
		cleanup(corgi)
		runCmdDone <- true
	}()

	utils.CleanFromScratch(cmd, *corgi)

	CreateDatabaseServices(corgi.DatabaseServices)

	runDatabaseServices(cmd, corgi.DatabaseServices)

	generateEnvForServices(corgi)

	for _, service := range corgi.Services {
		go runService(service, cmd)
	}

	<-runCmdDone
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

	err = utils.DockerInit()
	if err != nil {
		fmt.Println(err)
		return
	}

	if isSeed {
		SeedAllDatabases((databaseServices))
	}

	utils.ExecuteForEachService("up")
}

func runService(service utils.Service, cobraCmd *cobra.Command) {
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
		err = utils.RunServiceCmd(service.ServiceName, "git pull", service.Path)
		if err != nil {
			fmt.Println("pull failed for", service.ServiceName, "error:", err)
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

// Adds env variables to each service, including dependent db_services and services
func generateEnvForServices(corgiCompose *utils.CorgiCompose) {
	for _, service := range corgiCompose.Services {

		if service.IgnoreEnv {
			fmt.Println(
				art.RedColor,
				"Ignoring env file for",
				service.ServiceName,
				art.WhiteColor,
			)
			continue
		}

		var envForService string

		if service.CopyEnvFromFilePath != "" {
			envForService = strings.Join(
				utils.GetFileContent(service.CopyEnvFromFilePath),
				"\n",
			) + "\n"
		}

		// add url for dependent service
		if service.DependsOnServices != nil {
			for _, dependingService := range service.DependsOnServices {
				for _, s := range corgiCompose.Services {
					if s.ServiceName == dependingService.Name {
						var envNameToUse string
						if dependingService.EnvAlias != "" {
							envNameToUse = dependingService.EnvAlias
						} else {
							envNameToUse = splitStringForEnv(s.ServiceName) + "_URL"
						}
						if s.Port != 0 {
							envForService = fmt.Sprintf(
								"%s%s=http://localhost:%s%s\n",
								envForService,
								envNameToUse,
								fmt.Sprint(s.Port),
								dependingService.Suffix,
							)
							continue
						}
						for _, envLine := range s.Environment {
							if strings.Split(envLine, "=")[0] == "PORT" {
								envForService = fmt.Sprintf(
									"%s%s=http://localhost:%s%s\n",
									envForService,
									envNameToUse,
									strings.Split(envLine, "=")[1],
									dependingService.Suffix,
								)
								continue
							}
						}
					}
				}
			}
		}

		if service.DependsOnDb != nil {
			for _, dependingDb := range service.DependsOnDb {
				for _, db := range corgiCompose.DatabaseServices {
					if db.ServiceName == dependingDb.Name {
						var serviceNameInEnv string

						if len(service.DependsOnDb) > 1 {
							serviceNameInEnv = splitStringForEnv(db.ServiceName) + "_"
						}
						if dependingDb.EnvAlias != "" {
							if dependingDb.EnvAlias == "none" {
								serviceNameInEnv = ""
							} else {
								serviceNameInEnv = dependingDb.EnvAlias + "_"
							}
						}
						if db.Driver == "rabbitmq" {
							serviceNameInEnv = serviceNameInEnv + "RABBITMQ_"
						}
						host := fmt.Sprintf("\n%sDB_HOST=%s", serviceNameInEnv, db.Host)
						user := fmt.Sprintf("\n%sDB_USER=%s", serviceNameInEnv, db.User)
						name := fmt.Sprintf("\n%sDB_NAME=%s", serviceNameInEnv, db.DatabaseName)
						port := fmt.Sprintf("\n%sDB_PORT=%d", serviceNameInEnv, db.Port)
						password := fmt.Sprintf("\n%sDB_PASSWORD=%s\n", serviceNameInEnv, db.Password)
						switch db.Driver {
						case "rabbitmq":
							envForService = fmt.Sprintf(
								"%s%s%s%s%s", envForService, host, user, port, password)
						default:
							envForService = fmt.Sprintf(
								"%s%s%s%s%s%s", envForService, host, user, name, port, password)
						}
					}
				}
			}
		}

		if len(service.Environment[:]) > 0 {
			envForService =
				envForService + "\n" +
					strings.Join(service.Environment[:], "\n") +
					"\n"
		}

		if service.Port != 0 {
			envForService = fmt.Sprintf(
				"%s%s",
				envForService,
				fmt.Sprintf("\nPORT=%d", service.Port),
			)
		}

		pathToEnvFile := getPathToEnv(service)

		corgiGeneratedMessage := "# 🐶 Auto generated vars by corgi"
		var corgiEnvPosition []int
		envFileContent := utils.GetFileContent(pathToEnvFile)

		for index, line := range envFileContent {
			if line == corgiGeneratedMessage {
				corgiEnvPosition = append(corgiEnvPosition, index)
			}
		}

		if len(corgiEnvPosition) == 2 {
			envFileContent = removeIndexesFromSlice(
				envFileContent,
				corgiEnvPosition[0],
				corgiEnvPosition[1],
			)
		}

		envFileContentString := strings.Join(envFileContent, "\n")

		if len(envForService) != 0 {
			envForService := fmt.Sprintf(
				"\n%s\n%s\n%s\n",
				corgiGeneratedMessage,
				envForService,
				corgiGeneratedMessage,
			)
			envFileContentString = envFileContentString + envForService
		}
		if envFileContentString == "" {
			continue
		}
		f, err := os.OpenFile(pathToEnvFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			fmt.Println(err)
			continue
		}

		defer f.Close()
		if _, err = f.WriteString(envFileContentString); err != nil {
			fmt.Println(err)
			continue
		}
	}
}

func splitStringForEnv(s string) string {
	if strings.Contains(s, "/") {
		return strings.ToUpper(
			strings.Join(strings.Split(s, "/"), "_"),
		)
	}
	if strings.Contains(s, "-") {
		fmt.Println("here", s)
		return strings.ToUpper(
			strings.Join(strings.Split(s, "-"), "_"),
		)
	}
	re := regexp.MustCompile(`[^A-Z][^A-Z]*`)
	stringSlice := re.FindAllString(s, -1)

	for i := range stringSlice {
		if i == 0 {
			continue
		}
		characterIndex := strings.Index(s, stringSlice[i])
		stringSlice[i] = string(s[characterIndex-1]) + stringSlice[i]
	}
	return strings.ToUpper(
		strings.Join(stringSlice, "_"),
	)
}

func getPathToEnv(service utils.Service) string {
	envName := ".env"
	if service.EnvPath != "" {
		service.EnvPath = strings.Replace(
			service.EnvPath,
			service.Path,
			"",
			-1,
		)
		if strings.Contains(service.EnvPath, "/") {
			if service.EnvPath[:1] == "." {
				service.EnvPath = service.EnvPath[1:]
			}
			if service.EnvPath[:1] == "/" {
				service.EnvPath = service.EnvPath[1:]
			}
		}
		envName = service.EnvPath
	}

	if len(service.Path) <= 1 {
		return envName
	}
	if service.Path[len(service.Path)-1:] != "/" {
		return service.Path + "/" + envName
	} else {
		return service.Path + envName
	}
}

func removeIndexesFromSlice(s []string, from int, to int) []string {
	return append(s[:from], s[to+1:]...)
}

func omitServiceCmd(cmdName string) bool {
	for _, s := range omitItems {
		if cmdName == s {
			return true
		}
	}
	return false
}
