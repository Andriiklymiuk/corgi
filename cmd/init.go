package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"

	"andriiklymiuk/corgi/templates"
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
	Run: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) {

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		fmt.Printf("couldn't get services config, error: %s\n", err)
		return
	}
	utils.CleanFromScratch(cmd, *corgi)

	CreateDatabaseServices(corgi.DatabaseServices)
	CloneServices(corgi.Services)
	RunRequired(corgi.Required)

	filesToIgnore := []string{
		"# Added by corgi cli",
		utils.RootDbServicesFolder,
		"corgi-compose*.yml",
		".env*",
	}
	filesToIgnore = getGitignoreServicePath(corgi.Services, filesToIgnore)

	for _, fileToIgnore := range filesToIgnore {
		addFileToGitignore(fileToIgnore)
	}
}

type FilenameForService struct {
	Name     string
	Template string
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
		for _, file := range filesToCreate {
			err := createDbFileFromTemplate(
				service,
				file.Name,
				file.Template,
			)

			if err != nil {
				fmt.Printf(
					"error creating %s for service %s, error: %s",
					file.Name,
					service.ServiceName,
					err,
				)
				break
			}
		}
		fmt.Print(art.GreenColor, "✅ ", art.WhiteColor)
		fmt.Printf("Db service %s was successfully created\n", service.ServiceName)
	}
}

func getFilesToCreate(driver string) []FilenameForService {
	switch driver {
	case "rabbitmq":
		return []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeRabbitMQ},
			{"Makefile", templates.MakefileRabbitMQ},
		}
	default:
		return []FilenameForService{
			{"docker-compose.yml", templates.DockerComposePostgres},
			{"Makefile", templates.MakefilePostgres},
		}
	}
}

func CloneServices(services []utils.Service) {
	for _, service := range services {
		if service.Path == "" {
			fmt.Println("\nNo path for", service.ServiceName, ". Using current directory")
			continue
		}

		_, err := os.Stat(service.Path)
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
			pathSlice := strings.Split(service.Path, "/")
			pathWithoutLastFolder := strings.Join(pathSlice[:len(pathSlice)-1], "/")
			err := os.MkdirAll(pathWithoutLastFolder, os.ModePerm)
			if err != nil {
				fmt.Println(err)
				continue
			}

			err = utils.RunServiceCmd(
				service.ServiceName,
				fmt.Sprintf("git clone %s", service.CloneFrom),
				pathWithoutLastFolder,
			)
			if err != nil {
				fmt.Printf(`output error: %s, in path %s with git clone %s
					`, err, pathWithoutLastFolder, service.CloneFrom)
				continue
			}
			if service.Branch != "" {
				err = utils.RunServiceCmd(
					service.ServiceName,
					fmt.Sprintf("git checkout %s", service.Branch),
					service.Path,
				)
				if err != nil {
					fmt.Printf(`output error: %s, in path %s with git checkout %s
					`, err, service.Path, service.Branch)
					continue
				}
				err = utils.RunServiceCmd(
					service.ServiceName,
					"git pull",
					service.Path,
				)
				if err != nil {
					fmt.Printf(`output error: %s, in path %s with git pull %s
					`, err, service.Path, service.Branch)
					continue
				}
			}
		}
	}
}

func addFileToGitignore(fileToIgnore string) error {
	f, err := os.OpenFile(
		".gitignore",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		return fmt.Errorf("couldn't open .gitignore file, error: %s", err)
	}
	defer f.Close()

	content, err := os.ReadFile(".gitignore")
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

func getGitignoreServicePath(services []utils.Service, filesToIgnore []string) []string {
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

func createDbFileFromTemplate(
	dbService utils.DatabaseService,
	fileName string,
	fileTemplate string,
) error {
	path := fmt.Sprintf(
		"%s/%s",
		utils.RootDbServicesFolder,
		dbService.ServiceName,
	)

	err := os.MkdirAll(path, os.ModePerm)

	if err != nil {
		return fmt.Errorf("error of creating %s, error: %s", path, err)
	}

	dockerComposeFile := fmt.Sprintf("%s/%s", path, fileName)

	f, err := os.Create(dockerComposeFile)

	if err != nil {
		return fmt.Errorf("error of creating %s, error: %s", dockerComposeFile, err)
	}

	defer f.Close()

	tmp := template.Must(template.New("simple").Parse(fileTemplate))

	err = tmp.Execute(f, dbService)

	if err != nil {
		return fmt.Errorf(
			"error of creating template %s, error: %s",
			dockerComposeFile,
			err,
		)
	}
	return nil
}
