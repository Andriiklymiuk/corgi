package utils

import (
	"andriiklymiuk/corgi/templates"
	"fmt"
)

type FilenameForService struct {
	Name     string
	Template string
}

type DriverConfig struct {
	Prefix        string
	EnvGenerator  func(string, DatabaseService) string
	FilesToCreate []FilenameForService
}

var DriverConfigs = map[string]DriverConfig{
	"rabbitmq": {
		Prefix: "RABBITMQ_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s", host, user, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeRabbitMQ},
			{"Makefile", templates.MakefileRabbitMQ},
		},
	},
	"sqs": {
		Prefix: "AWS_SQS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)

			return fmt.Sprintf("%s%s%s%s%s%s", host,
				fmt.Sprintf("\nREGION=%s", templates.SqsRegion),
				fmt.Sprintf("\nAWS_REGION=%s", templates.SqsRegion),
				fmt.Sprintf("\n%sQUEUE_URL=%s", serviceNameInEnv, fmt.Sprintf("http://%s:%d/000000000000/%s", db.Host, db.Port, db.DatabaseName)),
				"\nAWS_ACCESS_KEY_ID=test",
				"\nAWS_SECRET_ACCESS_KEY=test",
			)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeSqs},
			{"Makefile", templates.MakefileSqs},
			{"bootstrap/bootstrap.sh", templates.BootstrapSqs},
		},
	},
	"redis": {
		Prefix: "REDIS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)
			host := fmt.Sprintf("\n%sHOST=%s\n", serviceNameInEnv, db.Host)

			return fmt.Sprintf("%s%s%s%s%s", user,
				port,
				password,
				fmt.Sprintf("\n%sURL=%s", serviceNameInEnv, fmt.Sprintf("redis://%s:%d", "localhost", db.Port)),
				host,
			)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeRedis},
			{"Makefile", templates.MakefileRedis},
			{"redis.conf", templates.RedisConfiguration},
			{"users.acl", templates.RedisAccessControlList},
		},
	},
	"mongodb": {
		Prefix: "MONGO_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeMongodb},
			{"Makefile", templates.MakefileMongodb},
		},
	},
	"mysql": {
		Prefix: "MYSQL_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeMySQL},
			{"Makefile", templates.MakefileMySQL},
		},
	},
	"dynamodb": {
		Prefix: "DYNAMODB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)

			return fmt.Sprintf("%s%s%s%s%s",
				host,
				port,
				name,
				fmt.Sprintf("\nREGION=%s", templates.DynamoDBRegion),
				fmt.Sprintf("\nAWS_REGION=%s", templates.DynamoDBRegion),
			)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeDynamoDB},
			{"Makefile", templates.MakefileDynamoDB},
			{"bootstrap/bootstrap.sh", templates.BootstrapDynamoDB},
		},
	},
	"kafka": {
		Prefix: "KAFKA_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, port, name, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeKafka},
			{"Makefile", templates.MakefileKafka},
			{"bootstrap/bootstrap.sh", templates.BootstrapKafka},
		},
	},
	"mssql": {
		Prefix: "MSSQL_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, port, name, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeMSSQL},
			{"Makefile", templates.MakefileMSSQL},
			{"bootstrap/bootstrap.sh", templates.BootstrapMSSQL},
		},
	},
	"default": {
		Prefix: "DB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposePostgres},
			{"Makefile", templates.MakefilePostgres},
		},
	},
}
