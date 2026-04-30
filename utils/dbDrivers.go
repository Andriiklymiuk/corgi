package utils

import (
	"andriiklymiuk/corgi/templates"
	"bufio"
	"fmt"
	"os"
	"strings"
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
			dashboardUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%s", db.Host, "15672"))

			return fmt.Sprintf("%s%s%s%s%s", host, user, port, password, dashboardUrl)
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

			return fmt.Sprintf("%s%s%s%s%s%s%s%s", host,
				fmt.Sprintf("\nREGION=%s", templates.SqsRegion),
				fmt.Sprintf("\nAWS_REGION=%s", templates.SqsRegion),
				fmt.Sprintf("\n%sENDPOINT=http://%s:%d/000000000000/", serviceNameInEnv, db.Host, db.Port),
				fmt.Sprintf("\n%sQUEUE_NAME=%s", serviceNameInEnv, db.DatabaseName),
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
	"s3": {
		Prefix: "AWS_S3_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s%s%s%s%s%s%s",
				host,
				port,
				fmt.Sprintf("\nREGION=%s", templates.S3Region),
				fmt.Sprintf("\nAWS_REGION=%s", templates.S3Region),
				fmt.Sprintf("\n%sENDPOINT_URL=http://%s:%d", serviceNameInEnv, db.Host, db.Port),
				fmt.Sprintf("\n%sBUCKET=%s", serviceNameInEnv, db.DatabaseName),
				"\nAWS_ACCESS_KEY_ID=test",
				"\nAWS_SECRET_ACCESS_KEY=test",
			)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeS3},
			{"Makefile", templates.MakefileS3},
			{"bootstrap/bootstrap.sh", templates.BootstrapS3},
		},
	},
	"redis": {
		Prefix: "REDIS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)
			host := fmt.Sprintf("\n%sHOST=%s\n", serviceNameInEnv, db.Host)

			return fmt.Sprintf("%s%s%s%s%s",
				user,
				port,
				password,
				fmt.Sprintf("\n%sURL=%s", serviceNameInEnv, fmt.Sprintf("redis://%s:%d", db.Host, db.Port)),
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
	"redis-server": {
		Prefix: "REDIS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			var password, token string
			if db.Password != "" {
				password = fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)
				token = fmt.Sprintf("\n%sTOKEN=%s\n", serviceNameInEnv, db.Password)
			}

			var url string
			if db.Password != "" {
				url = fmt.Sprintf("\n%sURL=%s", serviceNameInEnv,
					fmt.Sprintf("redis://:%s@%s:%d", db.Password, db.Host, db.Port))
			} else {
				url = fmt.Sprintf("\n%sURL=%s", serviceNameInEnv,
					fmt.Sprintf("redis://%s:%d", db.Host, db.Port))
			}

			host := fmt.Sprintf("\n%sHOST=%s\n", serviceNameInEnv, db.Host)

			return fmt.Sprintf("%s%s%s%s%s",
				port,
				password,
				token,
				url,
				host,
			)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeRedisServer},
			{"Makefile", templates.MakefileRedisServer},
		},
	},
	"keydb": {
		Prefix: "KEYDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)
			host := fmt.Sprintf("\n%sHOST=%s\n", serviceNameInEnv, db.Host)

			return fmt.Sprintf("%s%s%s%s%s",
				user,
				port,
				password,
				fmt.Sprintf("\n%sURL=%s", serviceNameInEnv, fmt.Sprintf("keydb://%s:%d", db.Host, db.Port)),
				host,
			)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeKeyDB},
			{"Makefile", templates.MakefileKeyDB},
			{"keydb.conf", templates.KeyDBConfiguration},
			{"users.acl", templates.KeyDBAccessControlList},
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
	"mariadb": {
		Prefix: "MARIADB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeMariaDB},
			{"Makefile", templates.MakefileMariaDB},
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

			dashboardUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%s", db.Host, "9000"))

			return fmt.Sprintf("%s%s%s%s%s%s", host, user, port, name, password, dashboardUrl)
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
		// TODO: mention somewhere, that if password is less than 8 characters, it will not create mssql db
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeMSSQL},
			{"Makefile", templates.MakefileMSSQL},
			{"bootstrap/bootstrap.sh", templates.BootstrapMSSQL},
		},
	},
	"cassandra": {
		Prefix: "CASSANDRA_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeCassandra},
			{"Makefile", templates.MakefileCassandra},
		},
	},
	"scylla": {
		Prefix: "SCYLLA_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeScylla},
			{"Makefile", templates.MakefileScylla},
			{"bootstrap/bootstrap.sh", templates.BootstrapScylla},
		},
	},
	"cockroach": {
		Prefix: "COCKROACH_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeCockroach},
			{"Makefile", templates.MakefileCockroach},
			{"bootstrap/bootstrap.sh", templates.BootstrapCockroach},
		},
	},
	"clickhouse": {
		Prefix: "CLICKHOUSE_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{
				"docker-compose.yml",
				strings.Replace(templates.DockerComposeClickHouse, "@@", "`", -1),
			},
			{
				"Makefile",
				strings.Replace(templates.MakefileClickHouse, "@@", "`", -1),
			},
			{
				"bootstrap/bootstrap.sh",
				strings.Replace(templates.BootstrapClickHouse, "@@", "`", -1),
			},
		},
	},
	"surrealdb": {
		Prefix: "SURREALDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeSurrealDB},
			{"Makefile", templates.MakefileSurrealDB},
		},
	},
	"influxdb": {
		Prefix: "INFLUXDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeInfluxDB},
			{"Makefile", templates.MakefileInfluxDB},
		},
	},
	"neo4j": {
		Prefix: "NEO4J_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			// add this fix, when neo4j community edition supports multiple databases
			// validDatabaseName := strings.ReplaceAll(db.DatabaseName, "-", "_")

			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, "neo4j")
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%s", db.Host, "7474"))

			return fmt.Sprintf("%s%s%s%s%s%s", host, user, name, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeNeo4j},
			{"Makefile", templates.MakefileNeo4j},
			{"bootstrap/bootstrap.sh", templates.BootstrapNeo4j},
		},
	},
	"dgraph": {
		Prefix: "DGRAPH_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, "0")
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			// no user and password is added, because acl is only available in enterprise version

			dashboardUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%s", db.Host, "8000"))
			dbUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%d", db.Host, db.Port))

			return fmt.Sprintf("%s%s%s%s%s", host, name, port, dashboardUrl, dbUrl)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeDgraph},
			{"Makefile", templates.MakefileDgraph},
		},
	},
	"arangodb": {
		Prefix: "ARANGO_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, "root")
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%d", db.Host, db.Port))

			return fmt.Sprintf("%s%s%s%s%s%s", host, user, name, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeArangoDB},
			{"Makefile", templates.MakefileArangoDB},
			{"bootstrap/bootstrap.sh", templates.BootstrapArangodb},
		},
	},
	"elasticsearch": {
		Prefix: "ELASTIC_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			kibanaDashboardUrl := fmt.Sprintf("\n%sKIBANA_DASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:5601", db.Host))

			return fmt.Sprintf("%s%s%s%s%s%s", host, user, name, port, password, kibanaDashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeElasticsearch},
			{"Makefile", templates.MakefileElasticsearch},
			{"bootstrap/bootstrap.sh", templates.BootstrapElasticsearch},
		},
	},
	"timescaledb": {
		Prefix: "TIMESCALE_DB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeTimescale},
			{"Makefile", templates.MakefileTimescale},
		},
	},
	"couchdb": {
		Prefix: "COUCHDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%d/_utils", db.Host, db.Port))

			return fmt.Sprintf("%s%s%s%s%s%s", host, user, name, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeCouchDB},
			{"Makefile", templates.MakefileCouchDB},
			{"bootstrap/bootstrap.sh", templates.BootstrapCouchDB},
		},
	},
	"meilisearch": {
		Prefix: "MEILISEARCH_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			// it doesn't use traditional usernames, so only host, port, name (for MeiliSearch itself), and the master key (acting like a password) are provided.

			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, "meilisearch")
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			masterKey := fmt.Sprintf("\n%sMASTER_KEY=%s\n", serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%d", db.Host, db.Port))

			return fmt.Sprintf("%s%s%s%s%s", host, name, port, masterKey, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeMeiliSearch},
			{"Makefile", templates.MakefileMeiliSearch},
		},
	},
	"faunadb": {
		Prefix: "FAUNADB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			// secret is default password in faunadb
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, "secret")

			return fmt.Sprintf("%s%s%s", host, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeFauna},
			{"Makefile", templates.MakefileFauna},
		},
	},
	"yugabytedb": {
		Prefix: "YUGABYTEDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)

			// use yugabyte as default one for use and password. TODO: change it to the provided one
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			password := fmt.Sprintf("\n%sPASSWORD=%s", serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf("\n%sDASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:%d", db.Host, 15433))

			return fmt.Sprintf("%s%s%s%s%s%s", host, user, name, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeYugabytedb},
			{"Makefile", templates.MakefileYugabytedb},
		},
	},
	"skytable": {
		Prefix: "SKYTABLE_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			// now docker generates password in logs, so we don't need to provide it
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeSkytable},
			{"Makefile", templates.MakefileSkytable},
		},
	},
	"dragonfly": {
		Prefix: "DRAGONFLY_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeDragonfly},
			{"Makefile", templates.MakefileDragonfly},
		},
	},
	"redict": {
		Prefix: "REDICT_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeRedict},
			{"Makefile", templates.MakefileRedict},
		},
	},
	"valkey": {
		Prefix: "VALKEY_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeValkey},
			{"Makefile", templates.MakefileValkey},
		},
	},
	"postgis": {
		Prefix: "POSTGIS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf("\n%sHOST=%s", serviceNameInEnv, db.Host)
			user := fmt.Sprintf("\n%sUSER=%s", serviceNameInEnv, db.User)
			name := fmt.Sprintf("\n%sNAME=%s", serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf("\n%sPORT=%d", serviceNameInEnv, db.Port)
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)

			return fmt.Sprintf("%s%s%s%s%s", host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposePostgis},
			{"Makefile", templates.MakefilePostgis},
		},
	},
	"pgvector": {
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
			{"docker-compose.yml", templates.DockerComposePgvector},
			{"Makefile", templates.MakefilePgvector},
		},
	},
	"localstack": {
		// Unified LocalStack driver: one container, multiple AWS services,
		// multiple queues and buckets. Emits generic AWS_* env + per-queue/per-bucket env.
		Prefix: "AWS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			var out strings.Builder

			fmt.Fprintf(&out, "\n%sHOST=%s", serviceNameInEnv, db.Host)
			fmt.Fprintf(&out, "\n%sPORT=%d", serviceNameInEnv, db.Port)
			fmt.Fprintf(&out, "\n%sENDPOINT_URL=http://%s:%d", serviceNameInEnv, db.Host, db.Port)
			fmt.Fprintf(&out, "\n%sSQS_ENDPOINT=http://%s:%d/000000000000/", serviceNameInEnv, db.Host, db.Port)
			fmt.Fprintf(&out, "\n%sS3_ENDPOINT_URL=http://%s:%d", serviceNameInEnv, db.Host, db.Port)
			fmt.Fprintf(&out, "\n%sREGION=%s", serviceNameInEnv, templates.LocalstackRegion)
			fmt.Fprintf(&out, "\n%sACCESS_KEY_ID=test", serviceNameInEnv)
			fmt.Fprintf(&out, "\n%sSECRET_ACCESS_KEY=test", serviceNameInEnv)

			// Per-queue: AWS_SQS_<NAME>=queue-name  AND  AWS_SQS_<NAME>_URL=full-url
			for _, q := range db.Queues {
				envKey := strings.ToUpper(strings.ReplaceAll(q, "-", "_"))
				fmt.Fprintf(&out, "\n%sSQS_%s=%s", serviceNameInEnv, envKey, q)
				fmt.Fprintf(&out, "\n%sSQS_%s_URL=http://%s:%d/000000000000/%s",
					serviceNameInEnv, envKey, db.Host, db.Port, q)
			}

			// Per-bucket: AWS_S3_<NAME>_BUCKET=bucket-name
			for _, b := range db.Buckets {
				envKey := strings.ToUpper(strings.ReplaceAll(b, "-", "_"))
				fmt.Fprintf(&out, "\n%sS3_%s_BUCKET=%s", serviceNameInEnv, envKey, b)
			}

			// Per-topic: AWS_SNS_<NAME>=topic-name  AND  AWS_SNS_<NAME>_ARN=full-arn
			for _, t := range db.Topics {
				envKey := strings.ToUpper(strings.ReplaceAll(t, "-", "_"))
				fmt.Fprintf(&out, "\n%sSNS_%s=%s", serviceNameInEnv, envKey, t)
				fmt.Fprintf(&out, "\n%sSNS_%s_ARN=arn:aws:sns:%s:000000000000:%s",
					serviceNameInEnv, envKey, templates.LocalstackRegion, t)
			}

			// Per-secret: AWS_SECRET_<NAME>=secret-name (path keys flattened)
			for _, s := range db.Secrets {
				envKey := awsEnvKey(s.Name)
				fmt.Fprintf(&out, "\n%sSECRET_%s=%s", serviceNameInEnv, envKey, s.Name)
			}

			// Per-parameter: AWS_SSM_<NAME>=parameter-name
			for _, p := range db.Parameters {
				envKey := awsEnvKey(p.Name)
				fmt.Fprintf(&out, "\n%sSSM_%s=%s", serviceNameInEnv, envKey, p.Name)
			}

			// Per-stream: AWS_KINESIS_<NAME>=stream-name
			for _, st := range db.Streams {
				envKey := strings.ToUpper(strings.ReplaceAll(st, "-", "_"))
				fmt.Fprintf(&out, "\n%sKINESIS_%s=%s", serviceNameInEnv, envKey, st)
			}

			out.WriteString("\n")
			return out.String()
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeLocalstack},
			{"Makefile", templates.MakefileLocalstack},
			{"bootstrap/bootstrap.sh", templates.BootstrapLocalstack},
		},
	},
	"supabase": {
		// Wraps the supabase CLI rather than running its containers directly —
		// the CLI manages its own multi-container stack (postgres, gotrue,
		// postgrest, kong, studio, storage-api, etc.). corgi only emits env
		// vars and triggers `supabase start/stop` from the project root.
		//
		// Defaults below match `supabase status -o env` output for a project
		// initialized with the stock JWT secret. Customizing the secret in
		// supabase/config.toml will diverge ANON_KEY / SERVICE_ROLE_KEY /
		// JWT_SECRET — handle via overrides in v2.
		Prefix: "SUPABASE_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			var out strings.Builder

			host := db.Host
			if host == "" {
				host = "localhost"
			}

			// Default source of truth = supabase/config.toml. If yaml `port:`
			// is set, it overrides [api].port — the Makefile patches
			// config.toml to match before `supabase start`, so emitted URLs
			// and the actual bind port stay consistent.
			ports := templates.ReadSupabasePorts(CorgiComposePathDir)
			if db.Port != 0 {
				ports.API = db.Port
			}

			jwtSecret := db.JWTSecret
			if jwtSecret == "" {
				jwtSecret = templates.SupabaseJWTSecret
			}
			anonKey := templates.SignSupabaseJWT(jwtSecret, "anon")
			serviceRoleKey := templates.SignSupabaseJWT(jwtSecret, "service_role")

			fmt.Fprintf(&out, "\n%sURL=http://%s:%d", serviceNameInEnv, host, ports.API)
			fmt.Fprintf(&out, "\n%sANON_KEY=%s", serviceNameInEnv, anonKey)
			fmt.Fprintf(&out, "\n%sSERVICE_ROLE_KEY=%s", serviceNameInEnv, serviceRoleKey)
			fmt.Fprintf(&out, "\n%sJWT_SECRET=%s", serviceNameInEnv, jwtSecret)
			fmt.Fprintf(&out, "\n%sDB_URL=postgresql://postgres:postgres@%s:%d/postgres", serviceNameInEnv, host, ports.DB)
			fmt.Fprintf(&out, "\n%sDB_HOST=%s", serviceNameInEnv, host)
			fmt.Fprintf(&out, "\n%sDB_PORT=%d", serviceNameInEnv, ports.DB)
			fmt.Fprintf(&out, "\n%sSTUDIO_URL=http://%s:%d", serviceNameInEnv, host, ports.Studio)
			fmt.Fprintf(&out, "\n%sINBUCKET_URL=http://%s:%d", serviceNameInEnv, host, ports.Inbucket)
			fmt.Fprintf(&out, "\n%sSTORAGE_S3_URL=http://%s:%d/storage/v1/s3", serviceNameInEnv, host, ports.API)
			fmt.Fprintf(&out, "\n%sS3_PROTOCOL_ACCESS_KEY_ID=%s", serviceNameInEnv, templates.SupabaseS3AccessKeyID)
			fmt.Fprintf(&out, "\n%sS3_PROTOCOL_ACCESS_KEY_SECRET=%s", serviceNameInEnv, templates.SupabaseS3AccessKey)
			fmt.Fprintf(&out, "\n%sS3_PROTOCOL_REGION=%s", serviceNameInEnv, templates.SupabaseS3Region)

			// Per-bucket: SUPABASE_BUCKET_<NAME>=<bucket-name>. Buckets are
			// auto-created by supabase via [storage.buckets.<name>] entries
			// in supabase/config.toml; corgi just emits the name for consumers.
			for _, b := range db.Buckets {
				envKey := strings.ToUpper(strings.ReplaceAll(b, "-", "_"))
				fmt.Fprintf(&out, "\n%sBUCKET_%s=%s", serviceNameInEnv, envKey, b)
			}

			out.WriteString("\n")
			return out.String()
		},
		FilesToCreate: []FilenameForService{
			{"Makefile", templates.MakefileSupabase},
			{"bootstrap/bootstrap.sh", templates.BootstrapSupabase},
		},
	},
	"image": {
		// Stateless docker-image driver. Use for services shipped as a public
		// image with no DB / persistent state (gotenberg, mailhog, jaeger,
		// redis-commander, etc.). Default env emission: <PREFIX>URL/HOST/PORT.
		// PREFIX is empty by default; consumers usually set `envAlias:` on
		// their depends_on_db entry. When no alias is set + no driver prefix,
		// emit uses the uppercased ServiceName as fallback prefix so vars
		// don't collide.
		Prefix: "",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			prefix := serviceNameInEnv
			if prefix == "" {
				prefix = strings.ToUpper(strings.ReplaceAll(db.ServiceName, "-", "_")) + "_"
			}
			host := db.Host
			if host == "" {
				host = "localhost"
			}
			var out strings.Builder
			if db.Port != 0 {
				fmt.Fprintf(&out, "\n%sURL=http://%s:%d", prefix, host, db.Port)
				fmt.Fprintf(&out, "\n%sHOST=%s", prefix, host)
				fmt.Fprintf(&out, "\n%sPORT=%d", prefix, db.Port)
			}
			out.WriteString("\n")
			return out.String()
		},
		FilesToCreate: []FilenameForService{
			{"docker-compose.yml", templates.DockerComposeImage},
			{"Makefile", templates.MakefileImage},
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

func GetServiceInfo(targetService string) (string, error) {
	f, err := os.Open(
		fmt.Sprintf(
			"%s/%s/%s/docker-compose.yml",
			CorgiComposePathDir,
			RootDbServicesFolder,
			targetService,
		),
	)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	var service []string
	for scanner.Scan() {
		service = getDbInfoFromString(scanner.Text(), service)
	}

	if len(service) == 0 {
		return "", fmt.Errorf("haven't found db_service info ")
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	result := fmt.Sprintf(`
Connection info to %s:
%s

`,
		targetService,
		strings.Join(service, "\n"),
	)

	return result, nil
}

func getDbInfoFromString(text string, dbInfoStringsArray []string) []string {
	// postgres
	if strings.Contains(text, "POSTGRES") {
		serviceInfo := strings.Replace(strings.TrimSpace(text), "POSTGRES_", "", 1)
		v := strings.Split(serviceInfo, "=")
		l := strings.Split(v[0], " ")[1] + " " + v[len(v)-1]
		return append(dbInfoStringsArray, l)
	}
	if strings.Contains(text, "5432") {
		serviceInfo := strings.ReplaceAll(strings.TrimSpace(text), `"`, "")
		v := strings.Split(serviceInfo, ":")
		return append(dbInfoStringsArray, "PORT "+strings.Split(v[0], " ")[1])
	}

	// rabbitmq
	if strings.Contains(text, "RABBITMQ") {
		serviceInfo := strings.Replace(strings.TrimSpace(text), "RABBITMQ_DEFAULT_", "", 1)
		v := strings.Split(serviceInfo, "=")
		l := strings.Split(v[0], " ")[1] + " " + v[len(v)-1]
		return append(dbInfoStringsArray, l)
	}
	if strings.Contains(text, "5672") {
		serviceInfo := strings.ReplaceAll(strings.TrimSpace(text), `"`, "")
		v := strings.Split(serviceInfo, ":")
		return append(dbInfoStringsArray, "PORT "+strings.Split(v[0], " ")[1])
	}

	// mongodb
	if strings.Contains(text, "MONGO") {
		serviceInfo := strings.Replace(strings.TrimSpace(text), "MONGO_INITDB_", "", 1)
		v := strings.Split(serviceInfo, "=")
		l := strings.Split(v[0], " ")[1] + " " + v[len(v)-1]
		return append(dbInfoStringsArray, l)
	}
	if strings.Contains(text, "27017") {
		serviceInfo := strings.ReplaceAll(strings.TrimSpace(text), `"`, "")
		v := strings.Split(serviceInfo, ":")
		return append(dbInfoStringsArray, "PORT "+strings.Split(v[0], " ")[1])
	}

	// mysql
	if strings.Contains(text, "MYSQL") {
		serviceInfo := strings.Replace(strings.TrimSpace(text), "MYSQL_", "", 1)
		v := strings.Split(serviceInfo, "=")
		l := strings.Split(v[0], " ")[1] + " " + v[len(v)-1]
		return append(dbInfoStringsArray, l)
	}
	if strings.Contains(text, "3306") {
		serviceInfo := strings.ReplaceAll(strings.TrimSpace(text), `"`, "")
		v := strings.Split(serviceInfo, ":")
		return append(dbInfoStringsArray, "PORT "+strings.Split(v[0], " ")[1])
	}

	return dbInfoStringsArray
}

func GetDumpFilename(driver string) string {
	switch driver {
	case "mssql":
		return "dump.bak"
	case "postgres":
		return "dump.sql"
	case "cassandra", "scylla":
		return "dump.cql"
	case "redis", "redis-server", "keydb":
		return "dump.rdb"
	case "surrealdb":
		return "dump.surql"
	case "neo4j":
		return "dump.cypher"
	case "couchdb":
		return "dump.json"
	default:
		return "dump.sql"
	}
}
