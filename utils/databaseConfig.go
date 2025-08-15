package utils

import (
	"encoding/json"
	"fmt"
	"os"
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
			additional = db.Additional

			// Parse RabbitMQ definition file to extract user credentials
			if db.Driver == "rabbitmq" {
				if definitionFile, err := os.ReadFile(db.Additional.DefinitionPath); err == nil {
					var definition RabbitMQDefinition
					if json.Unmarshal(definitionFile, &definition) == nil && len(definition.Users) > 0 {
						overrideUser = definition.Users[0].Name
						fmt.Printf("Info: Using user '%s' from definition file for service %s\n", overrideUser, serviceName)
					}
				}
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
