package utils

import (
	"andriiklymiuk/corgi/templates"
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	regionEnvFmt    = "\nREGION=%s"
	awsRegionEnvFmt = "\nAWS_REGION=%s"

	envHost         = "\n%sHOST=%s"
	envHostNL       = "\n%sHOST=%s\n"
	envUser         = "\n%sUSER=%s"
	envName         = "\n%sNAME=%s"
	envPort         = "\n%sPORT=%d"
	envPassword     = "\n%sPASSWORD=%s\n"
	envURL          = "\n%sURL=%s"
	envDashboardURL = "\n%sDASHBOARD_URL=%s\n"

	urlHostPort    = "http://%s:%s"
	urlHostPortInt = "http://%s:%d"

	concat5 = "%s%s%s%s%s"
	concat6 = "%s%s%s%s%s%s"

	fileDockerCompose = "docker-compose.yml"
	fileBootstrap     = "bootstrap/bootstrap.sh"
	fileMakefile      = "Makefile"
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
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)
			dashboardUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf(urlHostPort, db.Host, "15672"))

			return fmt.Sprintf(concat5, host, user, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeRabbitMQ},
			{fileMakefile, templates.MakefileRabbitMQ},
		},
	},
	"sqs": {
		Prefix: "AWS_SQS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)

			return fmt.Sprintf("%s%s%s%s%s%s%s%s", host,
				fmt.Sprintf(regionEnvFmt, templates.SqsRegion),
				fmt.Sprintf(awsRegionEnvFmt, templates.SqsRegion),
				fmt.Sprintf("\n%sENDPOINT=http://%s:%d/000000000000/", serviceNameInEnv, db.Host, db.Port),
				fmt.Sprintf("\n%sQUEUE_NAME=%s", serviceNameInEnv, db.DatabaseName),
				fmt.Sprintf("\n%sQUEUE_URL=%s", serviceNameInEnv, fmt.Sprintf("http://%s:%d/000000000000/%s", db.Host, db.Port, db.DatabaseName)),
				"\nAWS_ACCESS_KEY_ID=test",
				"\nAWS_SECRET_ACCESS_KEY=test",
			)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeSqs},
			{fileMakefile, templates.MakefileSqs},
			{fileBootstrap, templates.BootstrapSqs},
		},
	},
	"s3": {
		Prefix: "AWS_S3_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s%s%s%s%s%s%s",
				host,
				port,
				fmt.Sprintf(regionEnvFmt, templates.S3Region),
				fmt.Sprintf(awsRegionEnvFmt, templates.S3Region),
				fmt.Sprintf("\n%sENDPOINT_URL=http://%s:%d", serviceNameInEnv, db.Host, db.Port),
				fmt.Sprintf("\n%sBUCKET=%s", serviceNameInEnv, db.DatabaseName),
				"\nAWS_ACCESS_KEY_ID=test",
				"\nAWS_SECRET_ACCESS_KEY=test",
			)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeS3},
			{fileMakefile, templates.MakefileS3},
			{fileBootstrap, templates.BootstrapS3},
		},
	},
	"redis": {
		Prefix: "REDIS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)
			host := fmt.Sprintf(envHostNL, serviceNameInEnv, db.Host)

			return fmt.Sprintf(concat5,
				user,
				port,
				password,
				fmt.Sprintf(envURL, serviceNameInEnv, fmt.Sprintf("redis://%s:%d", db.Host, db.Port)),
				host,
			)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeRedis},
			{fileMakefile, templates.MakefileRedis},
			{"redis.conf", templates.RedisConfiguration},
			{"users.acl", templates.RedisAccessControlList},
		},
	},
	"redis-server": {
		Prefix: "REDIS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			var password, token string
			if db.Password != "" {
				password = fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)
				token = fmt.Sprintf("\n%sTOKEN=%s\n", serviceNameInEnv, db.Password)
			}

			var url string
			if db.Password != "" {
				url = fmt.Sprintf(envURL, serviceNameInEnv,
					fmt.Sprintf("redis://:%s@%s:%d", db.Password, db.Host, db.Port))
			} else {
				url = fmt.Sprintf(envURL, serviceNameInEnv,
					fmt.Sprintf("redis://%s:%d", db.Host, db.Port))
			}

			host := fmt.Sprintf(envHostNL, serviceNameInEnv, db.Host)

			return fmt.Sprintf(concat5,
				port,
				password,
				token,
				url,
				host,
			)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeRedisServer},
			{fileMakefile, templates.MakefileRedisServer},
		},
	},
	"keydb": {
		Prefix: "KEYDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)
			host := fmt.Sprintf(envHostNL, serviceNameInEnv, db.Host)

			return fmt.Sprintf(concat5,
				user,
				port,
				password,
				fmt.Sprintf(envURL, serviceNameInEnv, fmt.Sprintf("keydb://%s:%d", db.Host, db.Port)),
				host,
			)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeKeyDB},
			{fileMakefile, templates.MakefileKeyDB},
			{"keydb.conf", templates.KeyDBConfiguration},
			{"users.acl", templates.KeyDBAccessControlList},
		},
	},
	"mongodb": {
		Prefix: "MONGO_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeMongodb},
			{fileMakefile, templates.MakefileMongodb},
		},
	},
	"mysql": {
		Prefix: "MYSQL_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeMySQL},
			{fileMakefile, templates.MakefileMySQL},
		},
	},
	"mariadb": {
		Prefix: "MARIADB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeMariaDB},
			{fileMakefile, templates.MakefileMariaDB},
		},
	},
	"dynamodb": {
		Prefix: "DYNAMODB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)

			return fmt.Sprintf(concat5,
				host,
				port,
				name,
				fmt.Sprintf(regionEnvFmt, templates.DynamoDBRegion),
				fmt.Sprintf(awsRegionEnvFmt, templates.DynamoDBRegion),
			)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeDynamoDB},
			{fileMakefile, templates.MakefileDynamoDB},
			{fileBootstrap, templates.BootstrapDynamoDB},
		},
	},
	"kafka": {
		Prefix: "KAFKA_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf(urlHostPort, db.Host, "9000"))

			return fmt.Sprintf(concat6, host, user, port, name, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeKafka},
			{fileMakefile, templates.MakefileKafka},
			{fileBootstrap, templates.BootstrapKafka},
		},
	},
	"mssql": {
		Prefix: "MSSQL_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, port, name, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeMSSQL},
			{fileMakefile, templates.MakefileMSSQL},
			{fileBootstrap, templates.BootstrapMSSQL},
		},
	},
	"cassandra": {
		Prefix: "CASSANDRA_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeCassandra},
			{fileMakefile, templates.MakefileCassandra},
		},
	},
	"scylla": {
		Prefix: "SCYLLA_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeScylla},
			{fileMakefile, templates.MakefileScylla},
			{fileBootstrap, templates.BootstrapScylla},
		},
	},
	"cockroach": {
		Prefix: "COCKROACH_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeCockroach},
			{fileMakefile, templates.MakefileCockroach},
			{fileBootstrap, templates.BootstrapCockroach},
		},
	},
	"clickhouse": {
		Prefix: "CLICKHOUSE_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
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
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeSurrealDB},
			{fileMakefile, templates.MakefileSurrealDB},
		},
	},
	"influxdb": {
		Prefix: "INFLUXDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeInfluxDB},
			{fileMakefile, templates.MakefileInfluxDB},
		},
	},
	"neo4j": {
		Prefix: "NEO4J_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			// add this fix, when neo4j community edition supports multiple databases
			// validDatabaseName := strings.ReplaceAll(db.DatabaseName, "-", "_")

			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, "neo4j")
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf(urlHostPort, db.Host, "7474"))

			return fmt.Sprintf(concat6, host, user, name, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeNeo4j},
			{fileMakefile, templates.MakefileNeo4j},
			{fileBootstrap, templates.BootstrapNeo4j},
		},
	},
	"dgraph": {
		Prefix: "DGRAPH_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			name := fmt.Sprintf(envName, serviceNameInEnv, "0")
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			// no user and password is added, because acl is only available in enterprise version

			dashboardUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf(urlHostPort, db.Host, "8000"))
			dbUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf(urlHostPortInt, db.Host, db.Port))

			return fmt.Sprintf(concat5, host, name, port, dashboardUrl, dbUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeDgraph},
			{fileMakefile, templates.MakefileDgraph},
		},
	},
	"arangodb": {
		Prefix: "ARANGO_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, "root")
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf(urlHostPortInt, db.Host, db.Port))

			return fmt.Sprintf(concat6, host, user, name, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeArangoDB},
			{fileMakefile, templates.MakefileArangoDB},
			{fileBootstrap, templates.BootstrapArangodb},
		},
	},
	"elasticsearch": {
		Prefix: "ELASTIC_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			kibanaDashboardUrl := fmt.Sprintf("\n%sKIBANA_DASHBOARD_URL=%s\n", serviceNameInEnv, fmt.Sprintf("http://%s:5601", db.Host))

			return fmt.Sprintf(concat6, host, user, name, port, password, kibanaDashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeElasticsearch},
			{fileMakefile, templates.MakefileElasticsearch},
			{fileBootstrap, templates.BootstrapElasticsearch},
		},
	},
	"timescaledb": {
		Prefix: "TIMESCALE_DB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeTimescale},
			{fileMakefile, templates.MakefileTimescale},
		},
	},
	"couchdb": {
		Prefix: "COUCHDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf("http://%s:%d/_utils", db.Host, db.Port))

			return fmt.Sprintf(concat6, host, user, name, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeCouchDB},
			{fileMakefile, templates.MakefileCouchDB},
			{fileBootstrap, templates.BootstrapCouchDB},
		},
	},
	"meilisearch": {
		Prefix: "MEILISEARCH_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			// it doesn't use traditional usernames, so only host, port, name (for MeiliSearch itself), and the master key (acting like a password) are provided.

			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			name := fmt.Sprintf(envName, serviceNameInEnv, "meilisearch")
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			masterKey := fmt.Sprintf("\n%sMASTER_KEY=%s\n", serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf(urlHostPortInt, db.Host, db.Port))

			return fmt.Sprintf(concat5, host, name, port, masterKey, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeMeiliSearch},
			{fileMakefile, templates.MakefileMeiliSearch},
		},
	},
	"faunadb": {
		Prefix: "FAUNADB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			// secret is default password in faunadb
			password := fmt.Sprintf(envPassword, serviceNameInEnv, "secret")

			return fmt.Sprintf("%s%s%s", host, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeFauna},
			{fileMakefile, templates.MakefileFauna},
		},
	},
	"yugabytedb": {
		Prefix: "YUGABYTEDB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)

			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			password := fmt.Sprintf("\n%sPASSWORD=%s", serviceNameInEnv, db.Password)

			dashboardUrl := fmt.Sprintf(envDashboardURL, serviceNameInEnv, fmt.Sprintf(urlHostPortInt, db.Host, 15433))

			return fmt.Sprintf(concat6, host, user, name, port, password, dashboardUrl)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeYugabytedb},
			{fileMakefile, templates.MakefileYugabytedb},
		},
	},
	"skytable": {
		Prefix: "SKYTABLE_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			// now docker generates password in logs, so we don't need to provide it
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeSkytable},
			{fileMakefile, templates.MakefileSkytable},
		},
	},
	"dragonfly": {
		Prefix: "DRAGONFLY_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeDragonfly},
			{fileMakefile, templates.MakefileDragonfly},
		},
	},
	"redict": {
		Prefix: "REDICT_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeRedict},
			{fileMakefile, templates.MakefileRedict},
		},
	},
	"valkey": {
		Prefix: "VALKEY_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			return fmt.Sprintf("%s%s", host, port)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeValkey},
			{fileMakefile, templates.MakefileValkey},
		},
	},
	"postgis": {
		Prefix: "POSTGIS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposePostgis},
			{fileMakefile, templates.MakefilePostgis},
		},
	},
	"pgvector": {
		Prefix: "DB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposePgvector},
			{fileMakefile, templates.MakefilePgvector},
		},
	},
	"localstack": {
		// Unified LocalStack driver: one container, multiple AWS services,
		// multiple queues and buckets. Emits generic AWS_* env + per-queue/per-bucket env.
		Prefix: "AWS_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			var out strings.Builder

			fmt.Fprintf(&out, envHost, serviceNameInEnv, db.Host)
			fmt.Fprintf(&out, envPort, serviceNameInEnv, db.Port)
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
			{fileDockerCompose, templates.DockerComposeLocalstack},
			{fileMakefile, templates.MakefileLocalstack},
			{fileBootstrap, templates.BootstrapLocalstack},
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

			// Read ports from config.toml, then let yaml override per section.
			// Path depends on configTomlPath: corgi-managed dir if set, root if not.
			tomlSource := CorgiComposePathDir
			if db.ConfigTomlPath != "" {
				tomlSource = CorgiComposePathDir + "/" + RootDbServicesFolder + "/" + db.ServiceName + "/supabase/config.toml"
			}
			ports := templates.ReadSupabasePorts(tomlSource)
			if db.Port != 0 {
				ports.API = db.Port
			}
			if db.DbPort != 0 {
				ports.DB = db.DbPort
			}
			if db.StudioPort != 0 {
				ports.Studio = db.StudioPort
			}
			if db.InbucketPort != 0 {
				ports.Inbucket = db.InbucketPort
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
			{fileMakefile, templates.MakefileSupabase},
			{fileBootstrap, templates.BootstrapSupabase},
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
				fmt.Fprintf(&out, envHost, prefix, host)
				fmt.Fprintf(&out, envPort, prefix, db.Port)
			}
			out.WriteString("\n")
			return out.String()
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposeImage},
			{fileMakefile, templates.MakefileImage},
		},
	},
	"default": {
		Prefix: "DB_",
		EnvGenerator: func(serviceNameInEnv string, db DatabaseService) string {
			host := fmt.Sprintf(envHost, serviceNameInEnv, db.Host)
			user := fmt.Sprintf(envUser, serviceNameInEnv, db.User)
			name := fmt.Sprintf(envName, serviceNameInEnv, db.DatabaseName)
			port := fmt.Sprintf(envPort, serviceNameInEnv, db.Port)
			password := fmt.Sprintf(envPassword, serviceNameInEnv, db.Password)

			return fmt.Sprintf(concat5, host, user, name, port, password)
		},
		FilesToCreate: []FilenameForService{
			{fileDockerCompose, templates.DockerComposePostgres},
			{fileMakefile, templates.MakefilePostgres},
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
