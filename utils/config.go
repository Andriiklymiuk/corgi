package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var DbServicesInConfig = "db_services"
var RootDbServicesFolder = "corgi_services/db_services"
var ServicesItemsFromFlag []string
var DbServicesItemsFromFlag []string

type DatabaseService struct {
	ServiceName       string
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
	ManualRun           bool               `yaml:"manualRun"`
	CloneFrom           string             `yaml:"cloneFrom"`
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

type CorgiCompose struct {
	DatabaseServices []DatabaseService
	Services         []Service
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
		chosenCorgiPath, err := getCorgiConfigFromAlert()
		if err != nil || chosenCorgiPath == "" {
			pathToCorgiComposeFile = "corgi-compose.yml"
		} else {
			pathToCorgiComposeFile = chosenCorgiPath
		}
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
		for indexName, service := range dbServicesData[DbServicesInConfig] {
			if !IsServiceIncludedInFlag(DbServicesItemsFromFlag, indexName) {
				continue
			}
			var seedFromDb SeedFromDb
			if service.SeedFromDbEnvPath != "" {
				seedFromDb = getDbSourceFromPath(service.SeedFromDbEnvPath)
			}

			if (seedFromDb == SeedFromDb{}) {
				seedFromDb = service.SeedFromDb
			}

			dbToAdd := DatabaseService{
				ServiceName:      indexName,
				DatabaseName:     service.DatabaseName,
				User:             service.User,
				Password:         service.Password,
				Port:             service.Port,
				SeedFromDb:       seedFromDb,
				SeedFromFilePath: service.SeedFromFilePath,
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
	if len(servicesData["services"]) == 0 || !servicesCanBeAdded(ServicesItemsFromFlag) {
		fmt.Println("no services provided")
	} else {
		var services []Service
		for indexName, service := range servicesData["services"] {
			if !IsServiceIncludedInFlag(ServicesItemsFromFlag, indexName) {
				continue
			}
			serviceToAdd := Service{
				ServiceName:         indexName,
				Path:                service.Path,
				ManualRun:           service.ManualRun,
				CloneFrom:           service.CloneFrom,
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

func getCorgiConfigFromAlert() (string, error) {
	var files []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Println(err)
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yml" && filepath.Ext(path) != ".yaml" {
			return nil
		}
		if !strings.Contains(info.Name(), "corgi") {
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
