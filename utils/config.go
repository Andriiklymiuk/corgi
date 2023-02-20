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

var DbServicesInConfig = "db_services"
var ServicesInConfig = "services"
var RequiredInConfig = "required"
var RootDbServicesFolder = "corgi_services/db_services"
var ServicesItemsFromFlag []string
var DbServicesItemsFromFlag []string

type DatabaseService struct {
	ServiceName       string
	Driver            string     `yaml:"driver"`
	Host              string     `yaml:"host"`
	User              string     `yaml:"user"`
	Password          string     `yaml:"password"`
	DatabaseName      string     `yaml:"databaseName"`
	Port              int        `yaml:"port"`
	SeedFromDbEnvPath string     `yaml:"seedFromDbEnvPath"`
	SeedFromDb        SeedFromDb `yaml:"seedFromDb"`
	SeedFromFilePath  string     `yaml:"seedFromFilePath"`
}

type SeedFromDb struct {
	Host         string `yaml:"host"`
	DatabaseName string `yaml:"databaseName"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	Port         int    `yaml:"port"`
}

type DependsOnService struct {
	Name     string `yaml:"name"`
	EnvAlias string `yaml:"envAlias"`
	Suffix   string `yaml:"suffix"`
}

type DependsOnDb struct {
	Name     string `yaml:"name"`
	EnvAlias string `yaml:"envAlias"`
}

type Service struct {
	ServiceName         string
	Path                string             `yaml:"path"`
	IgnoreEnv           bool               `yaml:"ignore_env"`
	ManualRun           bool               `yaml:"manualRun"`
	CloneFrom           string             `yaml:"cloneFrom"`
	Branch              string             `yaml:"branch"`
	Environment         []string           `yaml:"environment"`
	EnvPath             string             `yaml:"envPath"`
	CopyEnvFromFilePath string             `yaml:"copyEnvFromFilePath"`
	Port                int                `yaml:"port"`
	DependsOnServices   []DependsOnService `yaml:"depends_on_services"`
	DependsOnDb         []DependsOnDb      `yaml:"depends_on_db"`
	BeforeStart         []string           `yaml:"beforeStart"`
	Start               []string           `yaml:"start"`
	AfterStart          []string           `yaml:"afterStart"`
}

type Required struct {
	Name     string
	Why      []string `yaml:"why"`
	Install  []string `yaml:"install"`
	Optional bool     `yaml:"optional"`
	CheckCmd string   `yaml:"checkCmd"`
}

type CorgiCompose struct {
	DatabaseServices []DatabaseService
	Services         []Service
	Required         []Required
}

// Get corgi-compose info from path to corgi-compose.yml file
func GetCorgiServices(cobra *cobra.Command) (*CorgiCompose, error) {
	filenameFlag, err := cobra.Root().Flags().GetString("filename")
	if err != nil {
		return nil, err
	}
	var pathToCorgiComposeFile string
	if filenameFlag != "" {
		pathToCorgiComposeFile = filenameFlag
	}
	if pathToCorgiComposeFile == "" {
		chosenPathToCorgiCompose, err := getCorgiConfigFilePath()
		if err != nil {
			return nil, err
		}
		pathToCorgiComposeFile = chosenPathToCorgiCompose
	}

	describeFlag, err := cobra.Root().Flags().GetBool("describe")
	if err != nil {
		return nil, err
	}
	file, err := os.ReadFile(pathToCorgiComposeFile)
	if err != nil {
		return nil, fmt.Errorf("couldn't read %s", pathToCorgiComposeFile)
	}

	var corgi CorgiCompose

	dbServicesData := make(map[string]map[string]DatabaseService)
	err = yaml.Unmarshal(file, &dbServicesData)
	if err != nil {
		return nil, fmt.Errorf("couldn't unmarshal dbServicesData %s", pathToCorgiComposeFile)
	}
	if len(dbServicesData[DbServicesInConfig]) == 0 || !servicesCanBeAdded(DbServicesItemsFromFlag) {
		fmt.Println("no db_services provided")
	} else {
		var dbServices []DatabaseService
		for indexName, db := range dbServicesData[DbServicesInConfig] {
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
				ServiceName:      indexName,
				Driver:           driver,
				Host:             host,
				DatabaseName:     db.DatabaseName,
				User:             db.User,
				Password:         db.Password,
				Port:             db.Port,
				SeedFromDb:       seedFromDb,
				SeedFromFilePath: db.SeedFromFilePath,
			}
			dbServices = append(dbServices, dbToAdd)

			if describeFlag {
				describeServiceInfo(dbToAdd)
			}
		}
		corgi.DatabaseServices = dbServices
	}

	servicesData := make(map[string]map[string]Service)
	err = yaml.Unmarshal(file, &servicesData)
	if err != nil {
		return nil, fmt.Errorf("couldn't unmarshal servicesData %s", pathToCorgiComposeFile)
	}
	if len(servicesData[ServicesInConfig]) == 0 || !servicesCanBeAdded(ServicesItemsFromFlag) {
		fmt.Println("no services provided")
	} else {
		var services []Service
		for indexName, service := range servicesData[ServicesInConfig] {
			if !IsServiceIncludedInFlag(ServicesItemsFromFlag, indexName) {
				continue
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
				BeforeStart:         service.BeforeStart,
				AfterStart:          service.AfterStart,
				Start:               service.Start,
			}
			services = append(services, serviceToAdd)

			if describeFlag {
				describeServiceInfo(serviceToAdd)
			}
		}
		corgi.Services = services
	}

	requiredData := make(map[string]map[string]Required)
	err = yaml.Unmarshal(file, &requiredData)
	if err != nil {
		return nil, fmt.Errorf("couldn't unmarshal required %s", pathToCorgiComposeFile)
	}
	if len(requiredData[RequiredInConfig]) == 0 {
		fmt.Println("no required instructions provided in file.")
		fmt.Println("Tip: It is useful to provide required to showcase what is used and how to install it")
		fmt.Println()
	} else {
		var requiredInstructions []Required
		for indexName, required := range requiredData[RequiredInConfig] {
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
	defaultCorgiConfigName := "corgi-compose.yml"
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
	if err != nil {
		return "", err
	}
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
