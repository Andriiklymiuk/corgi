package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func getEnvFromFile(filePath, corgiGeneratedMessage string) string {
	envFileContent := GetFileContent(filePath)
	var envFileNormalizedContent []string
	for _, content := range envFileContent {
		if content != corgiGeneratedMessage {
			envFileNormalizedContent = append(envFileNormalizedContent, content)
		}
	}

	return strings.Join(envFileNormalizedContent, "\n") + "\n"
}

func createEnvString(envForService, envName, host, port, suffix string) string {
	return fmt.Sprintf("%s%s=http://%s:%s%s\n", envForService, envName, host, port, suffix)
}

func findServiceByName(services []Service, serviceName string) *Service {
	for _, s := range services {
		if s.ServiceName == serviceName {
			return &s
		}
	}
	return nil
}

func handleDependentServices(service Service, corgiCompose CorgiCompose) string {
	envForService := ""

	if service.DependsOnServices == nil {
		return envForService
	}

	for _, dependingService := range service.DependsOnServices {
		s := findServiceByName(corgiCompose.Services, dependingService.Name)

		if s == nil {
			continue
		}

		var envNameToUse string
		if dependingService.EnvAlias != "" {
			envNameToUse = dependingService.EnvAlias
		} else {
			envNameToUse = splitStringForEnv(s.ServiceName) + "_URL"
		}

		if s.Port != 0 {
			envForService = createEnvString(envForService, envNameToUse, "localhost", fmt.Sprint(s.Port), dependingService.Suffix)
			continue
		}

		for _, envLine := range s.Environment {
			if strings.Split(envLine, "=")[0] == "PORT" {
				envForService = createEnvString(envForService, envNameToUse, "localhost", strings.Split(envLine, "=")[1], dependingService.Suffix)
				continue
			}
		}
	}

	return envForService
}

func handleDependsOnDb(service Service, corgiCompose CorgiCompose) string {
	var envForService string

	if service.DependsOnDb != nil {
		for _, dependingDb := range service.DependsOnDb {
			for _, db := range corgiCompose.DatabaseServices {
				if db.ServiceName == dependingDb.Name {
					envForService += generateEnvForDbDependentService(service, dependingDb, db)
				}
			}
		}
	}

	return envForService
}

func generateEnvForDbDependentService(service Service, dependingDb DependsOnDb, db DatabaseService) string {
	var serviceNameInEnv string

	if len(service.DependsOnDb) > 1 {
		serviceNameInEnv = splitStringForEnv(db.ServiceName) + "_"
	}
	if dependingDb.EnvAlias != "" {
		if dependingDb.EnvAlias == "none" {
			serviceNameInEnv = ""
		} else {
			serviceNameInEnv = dependingDb.EnvAlias + "_"
		}
	}

	driverConfig, ok := DriverConfigs[db.Driver]
	if !ok {
		driverConfig = DriverConfigs["default"]
	}

	serviceNameInEnv += driverConfig.Prefix
	envForService := driverConfig.EnvGenerator(serviceNameInEnv, db)

	return envForService
}

func EnsurePathExists(dirName string) error {
	_, err := os.Stat(dirName)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.MkdirAll(dirName, 0755)
}

// Adds env variables to each service, including dependent db_services and services
func GenerateEnvForServices(corgiCompose *CorgiCompose) {
	corgiGeneratedMessage := "# 🐶 Auto generated vars by corgi"
	for _, service := range corgiCompose.Services {
		err := EnsurePathExists(service.Path)
		if err != nil {
			fmt.Println("Error ensuring directory:", err)
			return
		}

		if service.IgnoreEnv {
			fmt.Println(
				art.RedColor,
				"Ignoring env file for",
				service.ServiceName,
				art.WhiteColor,
			)
			continue
		}

		var envForService string

		if service.CopyEnvFromFilePath != "" {
			envForService = getEnvFromFile(service.CopyEnvFromFilePath, corgiGeneratedMessage)
		}

		// add url for dependent service
		envForService += handleDependentServices(service, *corgiCompose)

		envForService += handleDependsOnDb(service, *corgiCompose)

		if len(service.Environment[:]) > 0 {
			envForService =
				envForService + "\n" +
					strings.Join(service.Environment[:], "\n") +
					"\n"
		}

		if service.Port != 0 {
			portAlias := "PORT"
			if service.PortAlias != "" {
				portAlias = service.PortAlias
			}
			envForService = fmt.Sprintf(
				"%s%s",
				envForService,
				fmt.Sprintf("\n%s=%d", portAlias, service.Port),
			)
		}

		pathToEnvFile := getPathToEnv(service)

		var corgiEnvPosition []int
		envFileContent := GetFileContent(pathToEnvFile)

		for index, line := range envFileContent {
			if line == corgiGeneratedMessage {
				corgiEnvPosition = append(corgiEnvPosition, index)
			}
		}

		if len(corgiEnvPosition) == 2 {
			envFileContent = removeFromToIndexes(
				envFileContent,
				corgiEnvPosition[0],
				corgiEnvPosition[1],
			)
		}

		envFileContentString := strings.Join(envFileContent, "\n")

		if len(envForService) != 0 {
			envForService := fmt.Sprintf(
				"\n%s\n%s\n%s\n",
				corgiGeneratedMessage,
				envForService,
				corgiGeneratedMessage,
			)
			envFileContentString = envFileContentString + envForService
		}
		if envFileContentString == "" {
			continue
		}
		f, err := os.OpenFile(pathToEnvFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			fmt.Println(err)
			continue
		}

		defer f.Close()
		if _, err = f.WriteString(envFileContentString); err != nil {
			fmt.Println(err)
			continue
		}
	}
}

func splitStringForEnv(s string) string {
	if strings.Contains(s, "/") {
		return strings.ToUpper(
			strings.Join(strings.Split(s, "/"), "_"),
		)
	}
	if strings.Contains(s, "-") {
		return strings.ToUpper(
			strings.Join(strings.Split(s, "-"), "_"),
		)
	}
	re := regexp.MustCompile(`[^A-Z][^A-Z]*`)
	stringSlice := re.FindAllString(s, -1)

	for i := range stringSlice {
		if i == 0 {
			continue
		}
		characterIndex := strings.Index(s, stringSlice[i])
		stringSlice[i] = string(s[characterIndex-1]) + stringSlice[i]
	}
	return strings.ToUpper(
		strings.Join(stringSlice, "_"),
	)
}

func getPathToEnv(service Service) string {
	envName := ".env"
	if service.EnvPath != "" {
		service.EnvPath = strings.Replace(
			service.EnvPath,
			service.Path,
			"",
			-1,
		)
		if strings.Contains(service.EnvPath, "/") {
			if service.EnvPath[:1] == "." {
				service.EnvPath = service.EnvPath[1:]
			}
			if service.EnvPath[:1] == "/" {
				service.EnvPath = service.EnvPath[1:]
			}
		}
		envName = service.EnvPath
	}

	if len(service.Path) <= 1 {
		return envName
	}
	if service.Path[len(service.Path)-1:] != "/" {
		return service.Path + "/" + envName
	} else {
		return service.Path + envName
	}
}

func removeFromToIndexes(s []string, from int, to int) []string {
	return append(s[:from], s[to+1:]...)
}
