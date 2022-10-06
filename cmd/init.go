package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"

	"andriiklymiuk/corgi/templates"
	"andriiklymiuk/corgi/utils"

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
	filesToIgnore := []string{
		"# Added by corgi cli",
		utils.RootDbServicesFolder,
		"corgi-compose.yml",
	}
	for _, fileToIgnore := range filesToIgnore {
		addFileToGitignore(fileToIgnore)
	}

	services, err := utils.GetCorgiServices("corgi-compose.yml")
	if err != nil {
		fmt.Printf("couldn't get services config, error: %s\n", err)
		return
	}

	CreateDatabaseServices(services.DatabaseServices)
	CreateServicesFolders(services.Services)
}

type FilenameForService struct {
	Name     string
	Template string
}

// Generate database files for each database service
func CreateDatabaseServices(databaseServices []utils.DatabaseService) {
	if len(databaseServices) == 0 {
		fmt.Println(`
No services info provided.
Please provide them in corgi-compose.yml file`)
		return
	}

	filesToCreate := []FilenameForService{
		{"docker-compose.yml", templates.DockerComposePostgres},
		{"Makefile", templates.MakefilePostgres},
	}

	for _, service := range databaseServices {
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
		fmt.Print(string("\033[32m"), "✅ ", string("\033[0m"))
		fmt.Printf("Db service %s was successfully created\n", service.ServiceName)
	}
}

func CreateServicesFolders(services []utils.Service) {
	for _, service := range services {
		if service.Path != "" {
			_, err := os.Stat(service.Path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
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

					cmd := exec.Command("git", "clone", service.CloneFrom)
					cmd.Dir = pathWithoutLastFolder
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					err = cmd.Run()
					if err != nil {
						fmt.Printf(`output error: %s, in path %s with git clone %s
					`, err, pathWithoutLastFolder, service.CloneFrom)
					}
					continue
				}
				fmt.Println(err)
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
