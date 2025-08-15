package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type RabbitMQDefinition struct {
	Users []RabbitMQUser `json:"users"`
}

type RabbitMQUser struct {
	Name string `json:"name"`
}

func ProcessAdditionalDatabaseConfig(db DatabaseService, serviceName string) (AdditionalDatabaseConfig, string, string) {
	var additional AdditionalDatabaseConfig
	var overrideUser, overridePassword string

	if db.Additional.DefinitionPath != "" {
		if _, err := os.Stat(db.Additional.DefinitionPath); err == nil {
			// Parse RabbitMQ definition file to extract user credentials
			if db.Driver == "rabbitmq" {
				if definitionFile, err := os.ReadFile(db.Additional.DefinitionPath); err == nil {
					var definition RabbitMQDefinition
					if json.Unmarshal(definitionFile, &definition) == nil && len(definition.Users) > 0 {
						overrideUser = definition.Users[0].Name
						fmt.Printf("Info: Using user '%s' from definition file for service %s\n", overrideUser, serviceName)

						if err := copyDefinitionFileToServiceDirectory(db.Additional.DefinitionPath, serviceName); err != nil {
							fmt.Printf("Warning: Failed to copy definition file to service directory: %s\n", err)
						} else {
							fmt.Printf("Info: Copied definition file to service directory for %s\n", serviceName)
						}

						additional = AdditionalDatabaseConfig{
							DefinitionPath: "./" + filepath.Base(db.Additional.DefinitionPath),
						}
					}
				}
			} else {
				additional = db.Additional
			}
		} else {
			fmt.Printf("Warning: Definition file %s not found for service %s, skipping additional config\n", db.Additional.DefinitionPath, serviceName)
		}
	}

	finalUser := db.User
	if overrideUser != "" {
		finalUser = overrideUser
	}

	finalPassword := db.Password
	if overridePassword != "" {
		finalPassword = overridePassword
	}

	return additional, finalUser, finalPassword
}

func copyDefinitionFileToServiceDirectory(definitionPath, serviceName string) error {
	targetDir := filepath.Join(CorgiComposePathDir, RootDbServicesFolder, serviceName)

	if err := os.MkdirAll(targetDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create target directory %s: %w", targetDir, err)
	}

	filename := filepath.Base(definitionPath)
	targetPath := filepath.Join(targetDir, filename)

	sourceFile, err := os.Open(definitionPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", definitionPath, err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", targetPath, err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	return nil
}
