package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var DbServicesInConfig = "db_services"
var RootDbServicesFolder = "corgi_services/db_services"

type DatabaseService struct {
	ServiceName       string
	User              string       `yaml:"user"`
	Password          string       `yaml:"password"`
	DatabaseName      string       `yaml:"databaseName"`
	Port              int          `yaml:"port"`
	SeedFromDbEnvPath string       `yaml:"seedFromDbEnvPath"`
	SeedFromDb        SeedDbSource `yaml:"seedFromDb"`
	SeedFromFilePath  string       `yaml:"seedFromFilePath"`
}

type SeedDbSource struct {
	Host         string `yaml:"host"`
	DatabaseName string `yaml:"databaseName"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	Port         int    `yaml:"port"`
}

type DependsOnService struct {
	Name     string `yaml:"name"`
	EnvAlias string `yaml:"envAlias"`
}

type Service struct {
	ServiceName         string
	Path                string             `yaml:"path"`
	CloneFrom           string             `yaml:"cloneFrom"`
	DockerEnabled       bool               `yaml:"docker_enabled"`
	Environment         []string           `yaml:"environment"`
	EnvPath             string             `yaml:"envPath"`
	CopyEnvFromFilePath string             `yaml:"copyEnvFromFilePath"`
	Port                int                `yaml:"port"`
	DependsOnServices   []DependsOnService `yaml:"depends_on_services"`
	DependsOnDb         []string           `yaml:"depends_on_db"`
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
	pathToCorgiComposeFile := "corgi-compose.yml"
	if filenameFlag != "" {
		pathToCorgiComposeFile = filenameFlag
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
	if len(dbServicesData[DbServicesInConfig]) == 0 {
		fmt.Println("no db_services provided")
	} else {
		var dbServices []DatabaseService
		for indexName, service := range dbServicesData[DbServicesInConfig] {
			var seedFromDb SeedDbSource
			if service.SeedFromDbEnvPath != "" {
				seedFromDb = getDbSourceFromPath(service.SeedFromDbEnvPath)
			}

			if (seedFromDb == SeedDbSource{}) {
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
	if len(servicesData["services"]) == 0 {
		fmt.Println("no services provided")
	} else {
		var services []Service
		for indexName, service := range servicesData["services"] {
			serviceToAdd := Service{
				ServiceName:         indexName,
				Path:                service.Path,
				CloneFrom:           service.CloneFrom,
				DockerEnabled:       service.DockerEnabled,
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

func CleanCorgiServicesFolder(cmd *cobra.Command, corgi CorgiCompose) {
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
	err = os.RemoveAll("./corgi_services/")
	if err != nil {
		fmt.Println("couldn't delete corgi_services folder: ", err)
		return
	}
	fmt.Println("🗑️ Cleaned up corgi_services")
}

func getDbSourceFromPath(path string) SeedDbSource {
	var seedFromDb SeedDbSource
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
