package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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
var UseDockerInConfig = "useDocker"
var UseAwsVpnInConfig = "useAwsVpn"
var NameInConfig = "name"
var DescriptionInConfig = "description"

var RootDbServicesFolder = "corgi_services/db_services"
var RootServicesFolder = "corgi_services/services"
var ServicesItemsFromFlag []string
var DbServicesItemsFromFlag []string

type DatabaseService struct {
	ServiceName       string     `yaml:"service_name,omitempty"`
	Driver            string     `yaml:"driver,omitempty" options:"postgres,mongodb,mysql,mariadb,redis,redis-server,rabbitmq,sqs,s3,dynamodb,kafka,mssql,cassandra,cockroach,clickhouse,scylla,keydb,surrealdb,neo4j,dgraph,arangodb,elasticsearch,timescaledb,couchdb,meilisearch,faunadb,yugabytedb,skytable,dragonfly,redict,valkey❌skip"`
	Version           string     `yaml:"version,omitempty"`
	Host              string     `yaml:"host,omitempty"`
	User              string     `yaml:"user,omitempty"`
	Password          string     `yaml:"password,omitempty"`
	DatabaseName      string     `yaml:"databaseName,omitempty"`
	Port              int        `yaml:"port,omitempty"`
	Port2             int        `yaml:"port2,omitempty"`
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

type Script struct {
	Name                string   `yaml:"name,omitempty"`
	ManualRun           bool     `yaml:"manualRun,omitempty"`
	Commands            []string `yaml:"commands,omitempty"`
	CopyEnvFromFilePath string   `yaml:"copyEnvFromFilePath,omitempty"`
}

type Runner struct {
	Name string `yaml:"name,omitempty" options:"docker,"`
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
	LocalhostNameInEnv  string             `yaml:"localhostNameInEnv,omitempty"`
	Port                int                `yaml:"port,omitempty"`
	PortAlias           string             `yaml:"portAlias,omitempty"`
	DependsOnServices   []DependsOnService `yaml:"depends_on_services,omitempty"`
	DependsOnDb         []DependsOnDb      `yaml:"depends_on_db,omitempty"`
	BeforeStart         []string           `yaml:"beforeStart,omitempty"`
	Start               []string           `yaml:"start,omitempty"`
	AfterStart          []string           `yaml:"afterStart,omitempty"`
	Scripts             []Script           `yaml:"scripts,omitempty"`
	InteractiveInput    bool               `yaml:"interactiveInput,omitempty"`

	Runner Runner `yaml:"runner,omitempty"`

	AbsolutePath string
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
	// cannot combine from one common struct (yaml serialization), so have to repeat
	Init        []string `yaml:"init,omitempty"`
	BeforeStart []string `yaml:"beforeStart,omitempty"`
	Start       []string `yaml:"start,omitempty"`
	AfterStart  []string `yaml:"afterStart,omitempty"`

	UseDocker bool `yaml:"useDocker,omitempty"`
	UseAwsVpn bool `yaml:"useAwsVpn,omitempty"`

	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type CorgiComposeYaml struct {
	DatabaseServices map[string]DatabaseService `yaml:"db_services"`
	Services         map[string]Service         `yaml:"services"`
	Required         map[string]Required        `yaml:"required"`
	// cannot combine from one common struct (yaml serialization), so have to repeat
	Init        []string `yaml:"init,omitempty"`
	BeforeStart []string `yaml:"beforeStart,omitempty"`
	Start       []string `yaml:"start,omitempty"`
	AfterStart  []string `yaml:"afterStart,omitempty"`

	UseDocker bool `yaml:"useDocker,omitempty"`
	UseAwsVpn bool `yaml:"useAwsVpn,omitempty"`

	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
}

var CorgiComposePath string
var CorgiComposePathDir string
var CorgiComposeFileContent *CorgiCompose

// Get corgi-compose info from path to corgi-compose.yml file
func GetCorgiServices(cobra *cobra.Command) (*CorgiCompose, error) {
	pathToCorgiComposeFile, err := determineCorgiComposePath(cobra)
	if err != nil {
		return nil, err
	}

	pathToCorgiComposeFile, err = filepath.Abs(pathToCorgiComposeFile)
	if err != nil {
		return nil, fmt.Errorf("couldn't get absolute path for %s: %v", pathToCorgiComposeFile, err)
	}

	fmt.Println("Using corgi-compose file:", pathToCorgiComposeFile)
	CorgiComposePath = pathToCorgiComposeFile
	CorgiComposePathDir = filepath.Dir(pathToCorgiComposeFile)

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
	corgi.UseDocker = corgiYaml.UseDocker
	corgi.UseAwsVpn = corgiYaml.UseAwsVpn

	corgi.Name = corgiYaml.Name
	corgi.Description = corgiYaml.Description

	dbServicesData := corgiYaml.DatabaseServices

	if err := SaveExecPath(
		corgi.Name,
		corgi.Description,
		pathToCorgiComposeFile,
	); err != nil {
		fmt.Println("failed to save corgi-compose file path: ", err)
	}

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
			} else {
				if db.SeedFromDb.Host != "" {
					seedFromDb.Host = db.SeedFromDb.Host
				}
				if db.SeedFromDb.DatabaseName != "" {
					seedFromDb.DatabaseName = db.SeedFromDb.DatabaseName
				}
				if db.SeedFromDb.User != "" {
					seedFromDb.User = db.SeedFromDb.User
				}
				if db.SeedFromDb.Password != "" {
					seedFromDb.Password = db.SeedFromDb.Password
				}
				if db.SeedFromDb.Port != 0 {
					seedFromDb.Port = db.SeedFromDb.Port
				}
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
				Port2:             db.Port2,
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
			if service.Runner.Name == "docker" && service.Port == 0 {
				exposedPort, err := GetExposedPortFromDockerfile(service)
				if err != nil {
					fmt.Println("couldn't get exposed port from Dockerfile: ", err)
				}
				if exposedPort != "" {
					service.Port, err = strconv.Atoi(exposedPort)
					if err != nil {
						fmt.Println("error converting exposed port to integer:", err)
					}
				}
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
			var absolutePath string
			if strings.HasPrefix(service.Path, "./") {
				absolutePath = strings.Replace(service.Path, "./", CorgiComposePathDir+"/", 1)
			} else {
				absolutePath = CorgiComposePathDir + "/" + service.Path
			}

			serviceToAdd := Service{
				ServiceName:         indexName,
				Path:                service.Path,
				AbsolutePath:        absolutePath,
				IgnoreEnv:           service.IgnoreEnv,
				ManualRun:           service.ManualRun,
				CloneFrom:           service.CloneFrom,
				Branch:              service.Branch,
				DependsOnServices:   service.DependsOnServices,
				DependsOnDb:         service.DependsOnDb,
				Environment:         service.Environment,
				EnvPath:             service.EnvPath,
				CopyEnvFromFilePath: service.CopyEnvFromFilePath,
				LocalhostNameInEnv:  service.LocalhostNameInEnv,
				Port:                service.Port,
				PortAlias:           service.PortAlias,
				BeforeStart:         service.BeforeStart,
				AfterStart:          service.AfterStart,
				Start:               service.Start,
				Scripts:             service.Scripts,
				InteractiveInput:    service.InteractiveInput,
				Runner:              service.Runner,
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

	CorgiComposeFileContent = &corgi
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
	globalFlag, err := cobraCmd.Flags().GetBool("global")
	if err != nil {
		return "", fmt.Errorf("error checking global flag: %v", err)
	}

	if globalFlag {
		globalPath, err := selectGlobalExecPath()
		if err != nil || globalPath == "" {
			return "", fmt.Errorf("no global corgi path selected")
		} else {
			return globalPath, nil
		}
	}
	filenameFlag, err := cobraCmd.Root().Flags().GetString("filename")
	if err != nil {
		return "", err
	}
	fromTemplateFlag, err := cobraCmd.Root().Flags().GetString("fromTemplate")
	if err != nil {
		return "", err
	}

	if fromTemplateFlag != "" {
		privateTokenFlag, err := cobraCmd.Root().Flags().GetString("privateToken")
		if err != nil {
			return "", err
		}

		downloadedFile, err := DownloadFileFromURL(fromTemplateFlag, filenameFlag, privateTokenFlag)
		if err != nil {
			return "", fmt.Errorf("error downloading template: %v", err)
		}
		return downloadedFile, nil
	}

	templateNameFlag, err := cobraCmd.Root().Flags().GetString("fromTemplateName")
	if err != nil {
		return "", err
	}
	if templateNameFlag != "" {
		return DownloadExample(
			cobraCmd,
			templateNameFlag,
			filenameFlag,
		)
	}
	showExampleList, err := cobraCmd.Root().Flags().GetBool("exampleList")
	if err != nil {
		return "", err
	}

	if showExampleList {
		selectedPath, err := PickItemFromListPrompt(
			"Select corgi template to use",
			ExtractExamplePaths(ExampleProjects),
			"none",
		)

		if err != nil {
			return "", fmt.Errorf("error selecting path: %v", err)
		}
		return DownloadExample(
			cobraCmd,
			selectedPath,
			filenameFlag,
		)
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

func selectGlobalExecPath() (string, error) {
	executionPaths, err := ListExecPaths()
	if err != nil {
		return "", fmt.Errorf("error retrieving executed paths: %v", err)
	}
	if len(executionPaths) == 0 {
		return "", fmt.Errorf("no global corgi paths found")
	}

	displayPaths := make([]string, len(executionPaths))
	for i, executionPath := range executionPaths {
		displayString := ""
		if executionPath.Name != "" {
			displayString = fmt.Sprintf("%s%s%s, ", art.BlueColor, executionPath.Name, art.WhiteColor)
		}
		displayString += executionPath.Path
		displayPaths[i] = displayString
	}

	selectedDisplay, err := PickItemFromListPrompt(
		"Select a path from global corgi paths",
		displayPaths,
		"none",
	)
	if err != nil {
		return "", fmt.Errorf("error selecting path: %v", err)
	}
	fmt.Printf("Selected path: %s\n", selectedDisplay)

	for _, executionPath := range executionPaths {
		formattedDisplay := executionPath.Path
		if executionPath.Name != "" {
			formattedDisplay = fmt.Sprintf("%s%s%s, %s", art.BlueColor, executionPath.Name, art.WhiteColor, executionPath.Path)
		}
		if selectedDisplay == formattedDisplay {
			return executionPath.Path, nil
		}
	}

	return "", fmt.Errorf("selected path not found in the list")
}

func toMap(slice interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	val := reflect.ValueOf(slice)
	for i := 0; i < val.Len(); i++ {
		item := val.Index(i).Interface()
		key := reflect.ValueOf(item).FieldByName("ServiceName").String()
		result[key] = item
	}
	return result
}

func CompareCorgiFiles(c1, c2 *CorgiCompose) bool {
	if c1.Name != c2.Name ||
		c1.Description != c2.Description ||
		c1.UseDocker != c2.UseDocker ||
		c1.UseAwsVpn != c2.UseAwsVpn {
		return false
	}

	if !reflect.DeepEqual(toMap(c1.Services), toMap(c2.Services)) {
		return false
	}

	if !reflect.DeepEqual(toMap(c1.DatabaseServices), toMap(c2.DatabaseServices)) {
		return false
	}

	if !reflect.DeepEqual(c1.Required, c2.Required) {
		return false
	}

	if !reflect.DeepEqual(c1.Init, c2.Init) ||
		!reflect.DeepEqual(c1.BeforeStart, c2.BeforeStart) ||
		!reflect.DeepEqual(c1.Start, c2.Start) ||
		!reflect.DeepEqual(c1.AfterStart, c2.AfterStart) {
		return false
	}

	return true
}
