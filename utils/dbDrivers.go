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
			password := fmt.Sprintf("\n%sPASSWORD=%s\n", serviceNameInEnv, db.Password)
			token := fmt.Sprintf("\n%sTOKEN=%s\n", serviceNameInEnv, db.Password)
			host := fmt.Sprintf("\n%sHOST=%s\n", serviceNameInEnv, db.Host)

			return fmt.Sprintf("%s%s%s%s%s",
				port,
				password,
				token,
				fmt.Sprintf("\n%sURL=%s", serviceNameInEnv, fmt.Sprintf("redis://%s:%d", db.Host, db.Port)),
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
	f, err := os.Open(fmt.Sprintf("%s/%s/docker-compose.yml", RootDbServicesFolder, targetService))
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
