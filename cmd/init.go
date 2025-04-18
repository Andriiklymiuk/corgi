package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"

	"andriiklymiuk/corgi/utils"
	"andriiklymiuk/corgi/utils/art"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create db service",
	Long: `
This is used to create db service from template.	
	`,
	Run:     runInit,
	Aliases: []string{"initialize", "clone"},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Printf("couldn't get services config, error: %s\n", err)
		return
	}
	utils.CleanFromScratch(cmd, *corgi)

	CreateMissingEnvFiles(corgi.Services)
	CreateDatabaseServices(corgi.DatabaseServices)
	CreateServices(corgi.Services)
	CloneServices(corgi.Services)
	RunRequired(corgi.Required)

	utils.RunServiceCommands(
		utils.InitInConfig,
		"corgi",
		corgi.Init,
		"",
		false,
		false,
	)

	filesToIgnore := []string{
		"# Added by corgi cli",
		"corgi_services/*",
		".env*",
	}
	filesToIgnore = getGitignoreServicePath(corgi.Services, filesToIgnore)

	for _, fileToIgnore := range filesToIgnore {
		addFileToGitignore(fileToIgnore)
	}
}

func CreateMissingEnvFiles(services []utils.Service) {
	for _, service := range services {
		utils.CreateFileForPath(service.CopyEnvFromFilePath)
	}
}

// Generate database files for each database service
func CreateDatabaseServices(databaseServices []utils.DatabaseService) {
	if len(databaseServices) == 0 {
		fmt.Println(`
No db_services info provided -> no db_services created.
Provide them in corgi-compose.yml file`)
		return
	}

	for _, service := range databaseServices {
		filesToCreate := getFilesToCreate(service.Driver)
		var errDuringFileCreation bool
		for _, file := range filesToCreate {
			err := createFileFromTemplate(
				service,
				file.Name,
				file.Template,
				service.ServiceName,
				utils.RootDbServicesFolder,
			)

			if err != nil {
				errDuringFileCreation = true
				fmt.Printf(
					"error creating %s for service %s, error: %s\n",
					file.Name,
					service.ServiceName,
					err,
				)
				break
			}
		}
		if errDuringFileCreation {
			fmt.Print(art.RedColor, "❌ ", art.WhiteColor)
			fmt.Printf("Db service %s had error during creation\n", service.ServiceName)
		} else {
			fmt.Print(art.GreenColor, "✅ ", art.WhiteColor)
			fmt.Printf("Db service %s was successfully created\n", service.ServiceName)
		}
	}
}

func CreateServices(services []utils.Service) {
	if len(services) == 0 {
		return
	}

	for _, service := range services {
		if service.Runner.Name == "" {
			continue
		}
		if service.Runner.Name != "docker" {
			continue
		}
		if service.Port == 0 {
			fmt.Printf(
				"Service %s does not have port specified, skipping docker runner creation\n",
				service.ServiceName,
			)
			continue
		}
		dockerfileExists, err := utils.CheckIfFileExistsInDirectory(
			service.AbsolutePath,
			"Dockerfile",
		)
		if err != nil {
			fmt.Println(err)
		}
		if !dockerfileExists {
			fmt.Printf(
				"Service %s does not have Dockerfile in path %s\n",
				service.ServiceName,
				service.AbsolutePath,
			)
			continue
		}

		err = copyEnvFileWithSubstitutions(service)
		if err != nil {
			fmt.Printf(
				"Error copying .env file for service %s: %s\n",
				service.ServiceName,
				err,
			)
		} else {
			fmt.Printf(
				"Successfully copied .env file for service %s with substitutions\n",
				service.ServiceName,
			)
		}

		filesToCreate := getServiceFilesToCreate(service.Runner.Name)

		var errDuringFileCreation bool
		for _, file := range filesToCreate {
			err := createFileFromTemplate(
				service,
				file.Name,
				file.Template,
				service.ServiceName,
				utils.RootServicesFolder,
			)

			if err != nil {
				errDuringFileCreation = true
				fmt.Printf(
					"error creating %s for service %s, error: %s\n",
					file.Name,
					service.ServiceName,
					err,
				)
				break
			}
		}
		if errDuringFileCreation {
			fmt.Print(art.RedColor, "❌ ", art.WhiteColor)
			fmt.Printf("Service %s had error during creation\n", service.ServiceName)
		} else {
			fmt.Print(art.GreenColor, "✅ ", art.WhiteColor)
			fmt.Printf("Service %s was successfully created\n", service.ServiceName)
		}
	}
}

func copyEnvFileWithSubstitutions(service utils.Service) error {
	envPath := utils.GetPathToEnv(service)
	sourceEnvPath := fmt.Sprintf("%s/%s", service.AbsolutePath, envPath)

	_, err := os.Stat(sourceEnvPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s file does not exist at %s", envPath, sourceEnvPath)
		}
		return fmt.Errorf("error checking %s file at %s: %w", envPath, sourceEnvPath, err)
	}

	content, err := os.ReadFile(sourceEnvPath)
	if err != nil {
		return fmt.Errorf("error reading .env file at %s: %w", sourceEnvPath, err)
	}

	modifiedContent := strings.ReplaceAll(string(content), "localhost", "host.docker.internal")
	modifiedContent = strings.ReplaceAll(modifiedContent, "127.0.0.1", "host.docker.internal")

	destPath := fmt.Sprintf("%s/%s/%s/.env",
		utils.CorgiComposePathDir,
		utils.RootServicesFolder,
		service.ServiceName)

	destDir := fmt.Sprintf("%s/%s/%s",
		utils.CorgiComposePathDir,
		utils.RootServicesFolder,
		service.ServiceName)

	err = os.MkdirAll(destDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("error creating directory %s: %w", destDir, err)
	}

	err = os.WriteFile(destPath, []byte(modifiedContent), 0644)
	if err != nil {
		return fmt.Errorf("error writing modified .env file for docker to %s: %w", destPath, err)
	}

	return nil
}

func getFilesToCreate(driver string) []utils.FilenameForService {
	driverConfig, ok := utils.DriverConfigs[driver]
	if !ok {
		driverConfig = utils.DriverConfigs["default"]
	}

	return driverConfig.FilesToCreate
}

func getServiceFilesToCreate(driver string) []utils.FilenameForService {
	driverConfig, ok := utils.ServiceConfigs[driver]
	if !ok {
		return nil
	}

	return driverConfig.FilesToCreate
}

func CheckClonedReposExistence(services []utils.Service) bool {
	var someRepoShouldBeCloned bool
	for _, service := range services {
		if service.CloneFrom == "" {
			continue
		}
		if service.Path == "" || service.Path == "." {
			continue
		}
		if service.Branch != "" {
			someRepoShouldBeCloned = true
		}
		_, err := os.Stat(
			service.AbsolutePath,
		)
		if err != nil {
			fmt.Printf("Path %s does not exist for service %s. It should be cloned.\n", service.AbsolutePath, service.ServiceName)
			someRepoShouldBeCloned = true
			break
		}
	}
	return someRepoShouldBeCloned
}

func CloneServices(services []utils.Service) {
	for _, service := range services {
		if service.Path == "" {
			fmt.Println("\nNo path for", service.ServiceName, ". Using current directory")
			continue
		}

		_, err := os.Stat(service.AbsolutePath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				fmt.Println(err)
				continue
			}
			if service.CloneFrom == "" {
				fmt.Printf(
					"No directory %s, please provide cloneFrom url or create service in the path",
					service.CloneFrom,
				)
				continue
			}
			pathSlice := strings.Split(service.AbsolutePath, "/")
			pathWithoutLastFolder := strings.Join(pathSlice[:len(pathSlice)-1], "/")
			err := os.MkdirAll(pathWithoutLastFolder, os.ModePerm)
			if err != nil {
				fmt.Println(err)
				continue
			}

			err = utils.RunServiceCmd(
				service.ServiceName,
				fmt.Sprintf(
					"git clone %s %s",
					service.CloneFrom,
					service.AbsolutePath,
				),
				pathWithoutLastFolder,
				true,
			)
			if err != nil {
				if strings.Contains(err.Error(), "exit status 128") {
					fmt.Printf(
						"Repo %s already exists in %s, skipping clone",
						service.CloneFrom,
						service.AbsolutePath,
					)
					continue
				}
				fmt.Printf(
					`output error: %s, in path %s with git clone %s`,
					err,
					pathWithoutLastFolder,
					service.CloneFrom,
				)
				continue
			}
			if service.Branch != "" {
				err = utils.RunServiceCmd(
					service.ServiceName,
					fmt.Sprintf(
						"git checkout %s",
						service.Branch,
					),
					service.AbsolutePath,
					true,
				)
				if err != nil {
					fmt.Printf(`output error: %s, in path %s with git checkout %s
					`, err, service.AbsolutePath, service.Branch)
					continue
				}
				err = utils.RunServiceCmd(
					service.ServiceName,
					"corgi pull --silent",
					service.AbsolutePath,
					true,
				)
				if err != nil {
					fmt.Printf(`output error: %s, in path %s with git pull %s
					`, err, service.AbsolutePath, service.Branch)
					continue
				}
			}
		} else {
			if service.CloneFrom == "" {
				continue
			}
			if service.Branch != "" {
				err := CheckoutToPrimaryBranch(
					service.ServiceName,
					service.AbsolutePath,
					service.Branch,
					false,
				)
				if err != nil {
					fmt.Println(err)
				}
			}
		}
		corgiComposeExists, err := utils.CheckIfFileExistsInDirectory(
			service.AbsolutePath,
			utils.CorgiComposeDefaultName,
		)
		if err != nil {
			fmt.Println(err)
		}
		if corgiComposeExists && service.CloneFrom != "" {
			err = utils.RunServiceCmd(
				service.ServiceName,
				"corgi init --silent",
				service.AbsolutePath,
				true,
			)
			if err != nil {
				fmt.Printf(`output error: %s, in path %s with corgi init --silent %s
					`, err, service.AbsolutePath, service.Branch)
			}
		}
	}
}

func addFileToGitignore(fileToIgnore string) error {
	gitignorePath := fmt.Sprintf("%s/%s", utils.CorgiComposePathDir, ".gitignore")
	f, err := os.OpenFile(
		gitignorePath,
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return fmt.Errorf("couldn't open .gitignore file, error: %s", err)
	}
	defer f.Close()

	content, err := os.ReadFile(
		gitignorePath,
	)
	if err != nil {
		return fmt.Errorf("couldn't read .gitignore file, error: %s", err)
	}

	if !strings.Contains(string(content), fileToIgnore) {
		_, err := f.WriteString(fmt.Sprintf(`
%s`, fileToIgnore))
		if err != nil {
			return fmt.Errorf(
				"couldn't add %s to .gitignore, error: %s",
				fileToIgnore,
				err,
			)
		}
		defer f.Close()
	}
	return nil
}

func getGitignoreServicePath(
	services []utils.Service,
	filesToIgnore []string,
) []string {
	for _, service := range services {
		if service.CloneFrom == "" {
			continue
		}
		if service.Path == "" {
			continue
		}
		if strings.Contains(service.Path, "../") {
			continue
		}
		gitignorePath := strings.ReplaceAll(
			service.Path,
			"./",
			"",
		)
		if len(strings.Split(gitignorePath, "/")) > 1 {
			continue
		}
		filesToIgnore = append(filesToIgnore, gitignorePath)
	}
	return filesToIgnore
}

func createFileFromTemplate(
	service interface{},
	fileName string,
	fileTemplate string,
	serviceName string,
	serviceFolder string,
) error {
	fileName, pathToFileName := getPathToFileName(fileName)
	path := fmt.Sprintf(
		"%s/%s/%s/%s",
		utils.CorgiComposePathDir,
		serviceFolder,
		serviceName,
		pathToFileName,
	)

	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return fmt.Errorf("error of creating %s, error: %s", path, err)
	}

	filePath := fmt.Sprintf("%s/%s", path, fileName)
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("error of creating %s, error: %s", filePath, err)
	}
	defer f.Close()

	if sv, ok := service.(utils.Service); ok && fileName == "docker-compose.yml" {
		exposedPort, err := utils.GetExposedPortFromDockerfile(sv)
		if err != nil {
			fmt.Printf("Warning: %v\n", err)
			fmt.Println("To fix this, add an EXPOSE directive to your Dockerfile, e.g., EXPOSE 3020")
		} else {
			fileTemplate = strings.Replace(
				fileTemplate,
				"${DOCKERFILE_PORT}",
				exposedPort,
				-1,
			)
		}
	}

	tmp := template.Must(template.New("simple").Parse(fileTemplate))
	err = tmp.Execute(f, service)
	if err != nil {
		return fmt.Errorf(
			"error of creating template %s, error: %s",
			filePath,
			err,
		)
	}
	return nil
}

func getPathToFileName(file string) (string, string) {
	pathSlice := strings.Split(file, "/")
	if len(pathSlice) > 1 {
		fileName := pathSlice[len(pathSlice)-1]
		return fileName, strings.Join(pathSlice[:len(pathSlice)-1], "/") + "/"
	}
	return file, ""
}
