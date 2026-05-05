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

func processRabbitMQDefinition(db DatabaseService, serviceName string) (AdditionalDatabaseConfig, string) {
	var additional AdditionalDatabaseConfig
	definitionFile, err := os.ReadFile(db.Additional.DefinitionPath)
	if err != nil {
		return additional, ""
	}
	var definition RabbitMQDefinition
	if json.Unmarshal(definitionFile, &definition) != nil || len(definition.Users) == 0 {
		return additional, ""
	}

	overrideUser := definition.Users[0].Name
	fmt.Printf("Info: Using user '%s' from definition file for service %s\n", overrideUser, serviceName)

	if err := copyDefinitionFileToServiceDirectory(db.Additional.DefinitionPath, serviceName); err != nil {
		fmt.Printf("Warning: Failed to copy definition file to service directory: %s\n", err)
	} else {
		fmt.Printf("Info: Copied definition file to service directory for %s\n", serviceName)
	}

	additional = AdditionalDatabaseConfig{
		DefinitionPath: "./" + filepath.Base(db.Additional.DefinitionPath),
	}
	return additional, overrideUser
}

func resolveAdditionalConfig(db DatabaseService, serviceName string) (AdditionalDatabaseConfig, string) {
	if db.Additional.DefinitionPath == "" {
		return AdditionalDatabaseConfig{}, ""
	}
	if _, err := os.Stat(db.Additional.DefinitionPath); err != nil {
		fmt.Printf("Warning: Definition file %s not found for service %s, skipping additional config\n", db.Additional.DefinitionPath, serviceName)
		return AdditionalDatabaseConfig{}, ""
	}
	if db.Driver == "rabbitmq" {
		return processRabbitMQDefinition(db, serviceName)
	}
	return db.Additional, ""
}

func ProcessAdditionalDatabaseConfig(db DatabaseService, serviceName string) (AdditionalDatabaseConfig, string, string) {
	additional, overrideUser := resolveAdditionalConfig(db, serviceName)

	finalUser := db.User
	if overrideUser != "" {
		finalUser = overrideUser
	}
	return additional, finalUser, db.Password
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
