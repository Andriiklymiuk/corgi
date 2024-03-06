package utils

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var CorgiComposeDefaultName = "corgi-compose.yml"
var DbServicesInConfig = "db_services"
var ServicesInConfig = "services"
var RequiredInConfig = "required"
var InitInConfig = "init"
var StartInConfig = "start"
var BeforeStartInConfig = "beforeStart"
var AfterStartInConfig = "afterStart"

var RootDbServicesFolder = "corgi_services/db_services"
var ServicesItemsFromFlag []string
var DbServicesItemsFromFlag []string

type DatabaseService struct {
	ServiceName       string     `yaml:"service_name,omitempty"`
	Driver            string     `yaml:"driver,omitempty" options:"postgres,mongodb,mysql,mariadb,redis,redis-server,rabbitmq,sqs,dynamodb,kafka,mssql,cassandra,cockroach,clickhouse,scylla,keydb,surrealdb,neo4j,dgraph,arangodb,elasticsearch,timescaledb,couchdb,meilisearch,faunadb,yugabytedb❌skip"`
	Version           string     `yaml:"version,omitempty"`
	Host              string     `yaml:"host,omitempty"`
	User              string     `yaml:"user,omitempty"`
	Password          string     `yaml:"password,omitempty"`
	DatabaseName      string     `yaml:"databaseName,omitempty"`
	Port              int        `yaml:"port,omitempty"`
	ManualRun         bool       `yaml:"manualRun,omitempty"`
	SeedFromDbEnvPath string     `yaml:"seedFromDbEnvPath,omitempty"`
	SeedFromFilePath  string     `yaml:"seedFromFilePath,omitempty"`
	SeedFromDb        SeedFromDb `yaml:"seedFromDb,omitempty"`
}

type SeedFromDb struct {
	Host         string `yaml:"host,omitempty"`
	DatabaseName string `yaml:"databaseName,omitempty"`
	User         string `yaml:"user,omitempty"`
	Password     string `yaml:"password,omitempty"`
	Port         int    `yaml:"port,omitempty"`
}

type DependsOnService struct {
	Name        string `yaml:"name,omitempty"`
	EnvAlias    string `yaml:"envAlias,omitempty"`
	Suffix      string `yaml:"suffix,omitempty"`
	ForceUseEnv bool   `yaml:"forceUseEnv,omitempty"`
}

type DependsOnDb struct {
	Name        string `yaml:"name,omitempty"`
	EnvAlias    string `yaml:"envAlias,omitempty"`
	ForceUseEnv bool   `yaml:"forceUseEnv,omitempty"`
}

type TestService struct {
	Name      string   `yaml:"name,omitempty"`
	ManualRun bool     `yaml:"manualRun,omitempty"`
	Command   []string `yaml:"command,omitempty"`
}

type Service struct {
	ServiceName         string             `yaml:"service_name,omitempty"`
	Path                string             `yaml:"path,omitempty"`
	IgnoreEnv           bool               `yaml:"ignore_env,omitempty"`
	ManualRun           bool               `yaml:"manualRun,omitempty"`
	CloneFrom           string             `yaml:"cloneFrom,omitempty"`
	Branch              string             `yaml:"branch,omitempty"`
	Environment         []string           `yaml:"environment,omitempty"`
	EnvPath             string             `yaml:"envPath,omitempty"`
	CopyEnvFromFilePath string             `yaml:"copyEnvFromFilePath,omitempty"`
	Port                int                `yaml:"port,omitempty"`
	PortAlias           string             `yaml:"portAlias,omitempty"`
	DependsOnServices   []DependsOnService `yaml:"depends_on_services,omitempty"`
	DependsOnDb         []DependsOnDb      `yaml:"depends_on_db,omitempty"`
	BeforeStart         []string           `yaml:"beforeStart,omitempty"`
	Start               []string           `yaml:"start,omitempty"`
	AfterStart          []string           `yaml:"afterStart,omitempty"`
	Test                []TestService      `yaml:"test,omitempty"`
}

type Required struct {
	Name     string   `yaml:"name,omitempty"`
	Why      []string `yaml:"why,omitempty"`
	Install  []string `yaml:"install,omitempty"`
	Optional bool     `yaml:"optional,omitempty"`
	CheckCmd string   `yaml:"checkCmd,omitempty"`
}

type CorgiCompose struct {
	DatabaseServices []DatabaseService
	Services         []Service
	Required         []Required
	// cannot combine from one commands struct, so have to repeated
	Init        []string `yaml:"init,omitempty"`
	BeforeStart []string `yaml:"beforeStart,omitempty"`
	Start       []string `yaml:"start,omitempty"`
	AfterStart  []string `yaml:"afterStart,omitempty"`
}

type CorgiComposeYaml struct {
	DatabaseServices map[string]DatabaseService `yaml:"db_services"`
	Services         map[string]Service         `yaml:"services"`
	Required         map[string]Required        `yaml:"required"`
	// cannot combine from one commands struct, so have to repeated
	Init        []string `yaml:"init,omitempty"`
	BeforeStart []string `yaml:"beforeStart,omitempty"`
	Start       []string `yaml:"start,omitempty"`
	AfterStart  []string `yaml:"afterStart,omitempty"`
}

var CorgiComposePath string

// Get corgi-compose info from path to corgi-compose.yml file
func GetCorgiServices(cobra *cobra.Command) (*CorgiCompose, error) {
	pathToCorgiComposeFile, err := determineCorgiComposePath(cobra)
	if err != nil {
		return nil, err
	}
	CorgiComposePath = pathToCorgiComposeFile

	describeFlag, err := cobra.Root().Flags().GetBool("describe")
	if err != nil {
		return nil, err
	}
	file, err := os.ReadFile(pathToCorgiComposeFile)
	if err != nil {
		return nil, fmt.Errorf("couldn't read %s", pathToCorgiComposeFile)
	}

	var corgi CorgiCompose

	var corgiYaml CorgiComposeYaml
	err = yaml.Unmarshal(file, &corgiYaml)
	if err != nil {
		return nil, fmt.Errorf("couldn't unmarshal file %s: %v", pathToCorgiComposeFile, err)
	}

	corgi.Init = corgiYaml.Init
	corgi.BeforeStart = corgiYaml.BeforeStart
	corgi.Start = corgiYaml.Start
	corgi.AfterStart = corgiYaml.AfterStart

	dbServicesData := corgiYaml.DatabaseServices

	if len(dbServicesData) == 0 || !servicesCanBeAdded(DbServicesItemsFromFlag) {
		fmt.Println("no db_services provided")
	} else {
		var dbServices []DatabaseService
		for indexName, db := range dbServicesData {
			if !IsServiceIncludedInFlag(DbServicesItemsFromFlag, indexName) {
				continue
			}
			var seedFromDb SeedFromDb
			if db.SeedFromDbEnvPath != "" {
				seedFromDb = getDbSourceFromPath(db.SeedFromDbEnvPath)
			}

			if (seedFromDb == SeedFromDb{}) {
				seedFromDb = db.SeedFromDb
			}

			var driver string
			if db.Driver == "" {
				driver = "postgres"
			} else {
				driver = db.Driver
			}

			var host string
			if db.Host == "" {
				host = "localhost"
			} else {
				host = db.Host
			}

			dbToAdd := DatabaseService{
				ServiceName:       indexName,
				Driver:            driver,
				Version:           db.Version,
				Host:              host,
				DatabaseName:      db.DatabaseName,
				User:              db.User,
				Password:          db.Password,
				Port:              db.Port,
				ManualRun:         db.ManualRun,
				SeedFromDb:        seedFromDb,
				SeedFromDbEnvPath: db.SeedFromDbEnvPath,
				SeedFromFilePath:  db.SeedFromFilePath,
			}
			dbServices = append(dbServices, dbToAdd)

			if describeFlag {
				describeServiceInfo(dbToAdd)
			}
		}
		corgi.DatabaseServices = dbServices
	}

	servicesData := corgiYaml.Services
	if len(servicesData) == 0 || !servicesCanBeAdded(ServicesItemsFromFlag) {
		fmt.Println("no services provided")
	} else {
		var services []Service
		for indexName, service := range servicesData {
			if !IsServiceIncludedInFlag(ServicesItemsFromFlag, indexName) {
				continue
			}
			if service.Path == "" && service.CloneFrom != "" {
				if strings.HasSuffix(service.CloneFrom, ".git") {
					splitURL := strings.Split(service.CloneFrom, "/")
					repoName := strings.TrimSuffix(splitURL[len(splitURL)-1], ".git")
					service.Path = "./" + repoName
				}
			}

			if !strings.HasPrefix(service.Path, "./") && service.Path != "" {
				service.Path = "./" + service.Path
			}

			if service.Path == "." {
				service.Path = ""
			}

			serviceToAdd := Service{
				ServiceName:         indexName,
				Path:                service.Path,
				IgnoreEnv:           service.IgnoreEnv,
				ManualRun:           service.ManualRun,
				CloneFrom:           service.CloneFrom,
				Branch:              service.Branch,
				DependsOnServices:   service.DependsOnServices,
				DependsOnDb:         service.DependsOnDb,
				Environment:         service.Environment,
				EnvPath:             service.EnvPath,
				CopyEnvFromFilePath: service.CopyEnvFromFilePath,
				Port:                service.Port,
				PortAlias:           service.PortAlias,
				BeforeStart:         service.BeforeStart,
				AfterStart:          service.AfterStart,
				Start:               service.Start,
				Test:                service.Test,
			}
			services = append(services, serviceToAdd)

			if describeFlag {
				describeServiceInfo(serviceToAdd)
			}
		}
		corgi.Services = services
	}

	requiredData := corgiYaml.Required
	if len(requiredData) == 0 {
		fmt.Println("no required instructions provided in file.")
		fmt.Println("Tip: It is useful to provide required to showcase what is used and how to install it")
		fmt.Println()
	} else {
		var requiredInstructions []Required
		for indexName, required := range requiredData {
			requiredToAdd := Required{
				Name:     indexName,
				Why:      required.Why,
				Install:  required.Install,
				Optional: required.Optional,
				CheckCmd: required.CheckCmd,
			}
			requiredInstructions = append(requiredInstructions, requiredToAdd)

			if describeFlag {
				describeServiceInfo(requiredToAdd)
			}
		}
		corgi.Required = requiredInstructions
	}

	return &corgi, nil
}

func GetDbServiceByName(databaseServiceName string, databaseServices []DatabaseService) (DatabaseService, error) {
	for _, db := range databaseServices {
		if db.ServiceName == databaseServiceName {
			return db, nil
		}
	}
	return DatabaseService{}, fmt.Errorf("db_service %s is not found", databaseServiceName)
}

func CleanFromScratch(cmd *cobra.Command, corgi CorgiCompose) {
	isFromScratch, err := cmd.Root().Flags().GetBool("fromScratch")
	if err != nil {
		fmt.Println(err)
		return
	}
	if !isFromScratch {
		return
	}
	if len(corgi.DatabaseServices) != 0 {
		ExecuteForEachService("remove")
	}
	CleanCorgiServicesFolder()
}

func CleanCorgiServicesFolder() {
	err := os.RemoveAll("./corgi_services/")
	if err != nil {
		fmt.Println("couldn't delete corgi_services folder: ", err)
		return
	}
	fmt.Println("🗑️ Cleaned up corgi_services")
}

func getDbSourceFromPath(path string) SeedFromDb {
	var seedFromDb SeedFromDb
	for _, envLine := range GetFileContent(path) {
		envLineValues := strings.Split(envLine, "=")
		switch strings.ToUpper(envLineValues[0]) {
		case "DB_HOST":
			seedFromDb.Host = envLineValues[1]
		case "DB_NAME":
			seedFromDb.DatabaseName = envLineValues[1]
		case "DB_PASSWORD":
			seedFromDb.Password = envLineValues[1]
		case "DB_USER":
			seedFromDb.User = envLineValues[1]
		case "DB_PORT":
			intVar, err := strconv.Atoi(envLineValues[1])
			if err != nil {
				fmt.Println(err)
				continue
			}
			seedFromDb.Port = intVar
		}
	}
	return seedFromDb
}

func describeServiceInfo(service any) {
	data, err := json.MarshalIndent(service, "", "\t")
	if err != nil {
		fmt.Println(err)
	} else {
		fmt.Println(string(data))
	}
}

func servicesCanBeAdded(services []string) bool {
	for _, service := range services {
		if service == "none" {
			return false
		}
	}
	return true
}

func IsServiceIncludedInFlag(services []string, serviceName string) bool {
	if len(services) == 0 {
		return true
	}
	var isIncluded bool
	for _, service := range services {
		if service == serviceName {
			isIncluded = true
		}
	}
	return isIncluded
}

func getCorgiConfigFilePath() (string, error) {
	defaultCorgiConfigName := CorgiComposeDefaultName
	corgiComposeExists, err := CheckIfFileExistsInDirectory(
		".",
		defaultCorgiConfigName,
	)
	if err != nil {
		return "", err
	}
	if corgiComposeExists {
		return defaultCorgiConfigName, nil
	}

	chosenCorgiPath, err := getCorgiConfigFromAlert()
	if err != nil || chosenCorgiPath == "" {
		return "", err
	}
	return chosenCorgiPath, nil
}

func getCorgiConfigFromAlert() (string, error) {
	var files []string
	err := filepath.WalkDir(".", func(path string, directory fs.DirEntry, err error) error {
		if err != nil {
			fmt.Println(err)
			return nil
		}
		if directory.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml" {
			return nil
		}
		if !strings.Contains(directory.Name(), "corgi") {
			return nil
		}

		files = append(files, path)

		return nil
	})

	if err != nil {
		fmt.Println(err)
		return "", err
	}

	file, err := PickItemFromListPrompt(
		"Select corgi config file to use",
		files,
		"none",
	)
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	return file, nil
}

func determineCorgiComposePath(cobraCmd *cobra.Command) (string, error) {
	filenameFlag, err := cobraCmd.Root().Flags().GetString("filename")
	if err != nil {
		return "", err
	}
	fromTemplateFlag, err := cobraCmd.Root().Flags().GetString("fromTemplate")
	if err != nil {
		return "", err
	}

	if fromTemplateFlag != "" {
		downloadedFile, err := DownloadFileFromURL(fromTemplateFlag, filenameFlag)
		if err != nil {
			return "", fmt.Errorf("error downloading template: %v", err)
		}
		return downloadedFile, nil
	}

	if filenameFlag != "" {
		return filenameFlag, nil
	}
	chosenPathToCorgiCompose, err := getCorgiConfigFilePath()
	if err != nil {
		return "", err
	}
	return chosenPathToCorgiCompose, nil

}
