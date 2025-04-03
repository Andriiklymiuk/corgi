package utils

import (
	"andriiklymiuk/corgi/utils/art"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

		if s.ManualRun && !dependingService.ForceUseEnv {
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
		// TODO: use export environment too, if present

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
					if db.ManualRun && !dependingDb.ForceUseEnv {
						continue
					}

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
	for _, service := range corgiCompose.Services {
		GenerateEnvForService(
			corgiCompose,
			service,
			"",
			false,
		)
	}
}

func GenerateEnvForService(
	corgiCompose *CorgiCompose,
	service Service,
	copyEnvFilePath string,
	ignoreDependentServicesEnvs bool,
) error {
	corgiGeneratedMessage := "# 🐶 Auto generated vars by corgi"
	err := EnsurePathExists(service.AbsolutePath)
	if err != nil {
		fmt.Println("Error ensuring directory:", err)
		return err
	}

	if service.IgnoreEnv {
		fmt.Println(
			art.RedColor,
			"Ignoring env file for",
			service.ServiceName,
			art.WhiteColor,
		)
		return nil
	}

	var envForService string
	var pathToCopyEnvFileFrom string
	if copyEnvFilePath != "" {
		pathToCopyEnvFileFrom = copyEnvFilePath
	} else {
		pathToCopyEnvFileFrom = service.CopyEnvFromFilePath
	}

	if pathToCopyEnvFileFrom != "" {
		copyEnvFromFileAbsolutePath := fmt.Sprintf(
			"%s/%s",
			CorgiComposePathDir,
			pathToCopyEnvFileFrom,
		)
		envForService = getEnvFromFile(
			copyEnvFromFileAbsolutePath,
			corgiGeneratedMessage,
		)
	}

	if !ignoreDependentServicesEnvs {
		// add url for dependent service
		envForService += handleDependentServices(service, *corgiCompose)

		envForService += handleDependsOnDb(service, *corgiCompose)

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

		if len(service.Environment) > 0 {
			// Parse existing environment variables from envForService into a map for easy lookup.
			existingEnvVars := parseEnvVarsIntoMap(envForService)

			var updatedEnvironment []string
			for _, envLine := range service.Environment {
				// Process each environment variable line for potential substitutions.
				updatedEnvLine := substituteEnvVarReferences(envLine, existingEnvVars)
				updatedEnvironment = append(updatedEnvironment, updatedEnvLine)
			}

			// Join the updated environment strings and add them to envForService.
			envForService += "\n" + strings.Join(updatedEnvironment, "\n") + "\n"
		}
	}

	pathToEnvFile := GetPathToEnv(service)

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

	if service.LocalhostNameInEnv != "" {
		envFileContentString = strings.ReplaceAll(
			envFileContentString,
			"localhost",
			service.LocalhostNameInEnv,
		)
	}

	if envFileContentString == "" {
		return nil
	}
	f, err := os.OpenFile(pathToEnvFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		fmt.Println(err)
		return err
	}

	defer f.Close()
	if _, err = f.WriteString(envFileContentString); err != nil {
		fmt.Println(err)
		return err
	}
	return nil
}

func parseEnvVarsIntoMap(envForService string) map[string]string {
	envMap := make(map[string]string)
	lines := strings.Split(envForService, "\n")
	for _, line := range lines {
		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := parts[0]
				value := parts[1]
				envMap[key] = value
			}
		}
	}
	return envMap
}

// substituteEnvVarReferences processes an environment variable line for variable references and substitutes them.
func substituteEnvVarReferences(envLine string, envMap map[string]string) string {
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(envLine, func(match string) string {
		// Extract the variable name from the match.
		varName := match[2 : len(match)-1] // Remove ${ and }
		if value, exists := envMap[varName]; exists {
			return value
		}
		// If there's no match, return the original placeholder.
		return match
	})
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

func GetPathToEnv(service Service) string {
	envName := ".env"
	if service.EnvPath != "" {
		service.EnvPath = strings.Replace(
			service.EnvPath,
			service.AbsolutePath,
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

	if len(service.AbsolutePath) <= 1 {
		return envName
	}
	if service.AbsolutePath[len(service.AbsolutePath)-1:] != "/" {
		return service.AbsolutePath + "/" + envName
	} else {
		return service.AbsolutePath + envName
	}
}

func removeFromToIndexes(s []string, from int, to int) []string {
	return append(s[:from], s[to+1:]...)
}

func CreateFileForPath(path string) {
	if path == "" {
		return
	}
	copyEnvFromFileAbsolutePath := fmt.Sprintf(
		"%s/%s",
		CorgiComposePathDir,
		path,
	)
	dirPath := filepath.Dir(
		copyEnvFromFileAbsolutePath,
	)
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {

		if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
			fmt.Printf(
				"Failed to create directory for env file %s, error: %s\n",
				copyEnvFromFileAbsolutePath,
				err,
			)
			return
		}
	}

	_, err := os.Stat(copyEnvFromFileAbsolutePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			f, err := os.Create(copyEnvFromFileAbsolutePath)
			if err != nil {
				fmt.Println(err)
			}
			defer f.Close()
		}
	}
}
