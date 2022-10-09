package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

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
		"omitBeforeStart",
		"",
		false,
		"Omits all before start commands from corgi-compose config",
	)
	runCmd.PersistentFlags().BoolP(
		"seed",
		"s",
		false,
		"Seed all db_services that have seedSource or have dump.sql in their folder",
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

	utils.CleanCorgiServicesFolder(cmd, *corgi)

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
		if service.AfterStart != nil {
			fmt.Println("\nAfter start commands:")
			for _, afterStartCmd := range service.AfterStart {
				err := runServiceCmd(afterStartCmd, service.Path)
				if err != nil {
					fmt.Println(
						string("\033[31m"),
						"aborting all other afterStart commands for ", service, ", because of ", err,
						string("\033[0m"),
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
		for _, dbService := range databaseServices {
			err := DumpAndSeedDb(dbService)
			if err != nil {
				fmt.Println("Error dumping and seeding file", err)
			}
		}
	}

	utils.ExecuteForEachService("up")
}

func runService(service utils.Service, cobraCmd *cobra.Command) {
	fmt.Println(string("\n\033[34m"), "🐶 RUNNING SERVICE", service.ServiceName, string("\033[0m"))
	omitBeforeStart, err := cobraCmd.Flags().GetBool("omitBeforeStart")
	if err != nil {
		return
	}

	if service.BeforeStart != nil && !omitBeforeStart {
		fmt.Println("\nBefore start commands:")
		for _, beforeStartCmd := range service.BeforeStart {
			err := runServiceCmd(beforeStartCmd, service.Path)
			if err != nil {
				fmt.Println(
					string("\033[31m"),
					"aborting all other beforeStart commands for ", service, ", because of ", err,
					string("\033[0m"),
				)
				return
			}
		}
	}
	if service.Start != nil {
		fmt.Println("\nStart commands:")
		for _, startCmd := range service.Start {
			go func(startCmd string) {
				err := runServiceCmd(startCmd, service.Path)
				if err != nil {
					fmt.Println(
						string("\033[31m"),
						"aborting all other start commands for ", service, ", because of ", err,
						string("\033[0m"),
					)
					return
				}
			}(startCmd)
		}
	}
}

func runServiceCmd(serviceCommand string, path string) error {
	fmt.Println("\n🚀 🤖 Executing command: ", string("\033[32m"), serviceCommand, string("\033[0m"))

	commandSlice := strings.Fields(serviceCommand)
	cmd := exec.Command(commandSlice[0], commandSlice[1:]...)

	cmd.Dir = path
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Adds env variables to each service, including dependent db_services and services
func generateEnvForServices(corgiCompose *utils.CorgiCompose) {
	for _, service := range corgiCompose.Services {

		envForService := strings.Join(service.Environment[:], "\n")

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
								"%s\n%s=http://localhost:%s",
								envForService,
								envNameToUse,
								fmt.Sprint(s.Port),
							)
							continue
						}
						for _, envLine := range s.Environment {
							if strings.Split(envLine, "=")[0] == "PORT" {
								envForService = fmt.Sprintf(
									"%s\n%s=http://localhost:%s",
									envForService,
									envNameToUse,
									strings.Split(envLine, "=")[1],
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
					if db.ServiceName == dependingDb {
						var serviceNameInEnv string

						// add name of db, if there are more than 2 dependent service
						if len(service.DependsOnDb) > 1 {
							serviceNameInEnv = splitStringForEnv(db.ServiceName) + "_"
						}
						envForService = fmt.Sprintf(
							"%s%s%s%s%s%s",
							envForService,
							fmt.Sprintf("\n\nDB_%sHOST=http://localhost", serviceNameInEnv),
							fmt.Sprintf("\nDB_%sUSER=%s", serviceNameInEnv, db.User),
							fmt.Sprintf("\nDB_%sNAME=%s", serviceNameInEnv, db.DatabaseName),
							fmt.Sprintf("\nDB_%sPORT=%d", serviceNameInEnv, db.Port),
							fmt.Sprintf("\nDB_%sPASSWORD=%s", serviceNameInEnv, db.Password),
						)
					}
				}
			}
		}

		pathToEnvFile := getPathToEnv(service)

		corgiGeneratedMessage := "# 🐶 Auto generated vars by corgi"
		var corgiEnvPosition []int
		envFileContent := getFileContent(pathToEnvFile)

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

func getFileContent(fileName string) []string {
	f, err := os.Open(fileName)
	if err != nil {
		fmt.Println(err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	result := []string{}

	for scanner.Scan() {
		line := scanner.Text()
		result = append(result, line)
	}
	return result
}

func removeIndexesFromSlice(s []string, from int, to int) []string {
	return append(s[:from], s[to+1:]...)
}
