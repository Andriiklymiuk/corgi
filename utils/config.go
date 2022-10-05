package utils

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var DbServicesInConfig = "db_services"
var RootDbServicesFolder = "corgi_services/db_services"

type DatabaseService struct {
	ServiceName  string
	User         string       `yaml:"user"`
	Password     string       `yaml:"password"`
	DatabaseName string       `yaml:"databaseName"`
	Port         int          `yaml:"port"`
	SeedFromDb   SeedDbSource `yaml:"seedFromDb"`
}

type SeedDbSource struct {
	Host         string `yaml:"host"`
	DatabaseName string `yaml:"databaseName"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	Port         int    `yaml:"port"`
}

type Service struct {
	ServiceName       string
	Path              string   `yaml:"path"`
	DockerEnabled     bool     `yaml:"docker_enabled"`
	Environment       []string `yaml:"environment"`
	Port              int      `yaml:"port"`
	DependsOnServices []string `yaml:"depends_on_services"`
	DependsOnDb       []string `yaml:"depends_on_db"`
	BeforeStart       []string `yaml:"beforeStart"`
	Start             []string `yaml:"start"`
	AfterStart        []string `yaml:"afterStart"`
}

type CorgiCompose struct {
	DatabaseServices []DatabaseService
	Services         []Service
}

// Get corgi-compose info from path to corgi-compose.yml file
func GetCorgiServices(pathToCorgiComposeFile string) (*CorgiCompose, error) {
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
			dbServices = append(dbServices, DatabaseService{
				ServiceName:  indexName,
				DatabaseName: service.DatabaseName,
				User:         service.User,
				Password:     service.Password,
				Port:         service.Port,
				SeedFromDb:   service.SeedFromDb,
			})
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
			services = append(services, Service{
				ServiceName:       indexName,
				Path:              service.Path,
				DockerEnabled:     service.DockerEnabled,
				DependsOnServices: service.DependsOnServices,
				DependsOnDb:       service.DependsOnDb,
				Environment:       service.Environment,
				Port:              service.Port,
				BeforeStart:       service.BeforeStart,
				Start:             service.Start,
			})
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
