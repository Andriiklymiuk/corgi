package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"andriiklymiuk/corgi/utils"
)

var createCmd = &cobra.Command{
	Use:   "init",
	Short: "Create db service",
	Long: `
This is used to create db service from template.	
	`,
	Run: runCreate,
}

type FilenameForService struct {
	Name     string
	Template string
}

func runCreate(cmd *cobra.Command, args []string) {
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
	}

	createDatabaseServices(services.DatabaseServices)
}

func createDatabaseServices(databaseServices []utils.DatabaseService) {
	if len(databaseServices) == 0 {
		fmt.Println(`
No services info provided.
Please provided them in corgi-compose.yml file`)
		return
	}

	filesToCreate := []FilenameForService{
		{"docker-compose.yml", dockerComposeTemplate},
		{"Makefile", makefileTemplate},
	}

	for _, service := range databaseServices {
		for _, file := range filesToCreate {
			err := createFileFromTemplate(
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

func createFileFromTemplate(
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

var dockerComposeTemplate = `version: "3.8"

services:
  postgres:
    image: postgres:11.5-alpine
    container_name: postgres-{{.ServiceName}}
    logging:
      driver: none
    environment:
      - POSTGRES_USER={{.User}}
      - POSTGRES_PASSWORD={{.Password}}
      - POSTGRES_DB={{.DatabaseName}}
    ports:
      - "{{.Port}}:5432"
`

var makefileTemplate = `up:
	docker compose up -d
down:
	docker compose down    
stop:
	docker stop postgres-{{.ServiceName}}
id:
	docker ps -aqf "name=postgres-{{.ServiceName}}" | awk '{print $1}'
seed:
	cat dump.sql | docker exec -i $(c)  psql -U {{.User}} -d {{.DatabaseName}}
help:
	make -qpRr | egrep -e '^[a-z].*:$$' | sed -e 's~:~~g' | sort

.PHONY: up down stop id seed help
`

func init() {
	rootCmd.AddCommand(createCmd)
}
