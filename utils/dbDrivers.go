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
				fmt.Sprintf("\n%sQUEUE_URL=%s", serviceNameInEnv, fmt.Sprintf("http://localhost:%d/000000000000/%s", db.Port, db.DatabaseName)),
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

			return fmt.Sprintf("%s%s%s%s", user,
				port,
				password,
				fmt.Sprintf("\n%sURL=%s", serviceNameInEnv, fmt.Sprintf("redis://localhost:%d", db.Port)),
			)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeRedis},
			{"Makefile", templates.MakefileRedis},
			{"Dockerfile", templates.DockerfileRedis},
			{"redis.conf", templates.RedisConfiguration},
			{"users.acl", templates.RedisAccessControlList},
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
