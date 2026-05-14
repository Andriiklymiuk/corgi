package utils

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
)

const redisCLITool = "redis-cli"

// shellConfig is the per-driver mapping from corgi DatabaseService to the
// CLI tool that opens its shell (psql, mongosh, ...). argsFunc builds args
// for interactive mode; execArgsFunc is the non-interactive --exec variant.
type shellConfig struct {
	cmd          string
	argsFunc     func(db DatabaseService) []string
	execArgsFunc func(db DatabaseService, query string) []string
}

func postgresArgs(db DatabaseService) []string {
	return []string{
		"-U", defaultStr(db.User, "postgres"),
		"-d", defaultStr(db.DatabaseName, "postgres"),
	}
}

func postgresExecArgs(db DatabaseService, query string) []string {
	return append(postgresArgs(db), "-c", query)
}

func redisArgs(db DatabaseService) []string {
	if db.Password != "" {
		return []string{"-a", db.Password}
	}
	return nil
}

func redisExecArgs(db DatabaseService, query string) []string {
	args := redisArgs(db)
	// redis-cli treats trailing space-separated tokens as the command.
	for _, tok := range strings.Fields(query) {
		args = append(args, tok)
	}
	return args
}

func mongoArgs(db DatabaseService) []string {
	port := db.Port
	if port == 0 {
		port = 27017
	}
	u := &url.URL{
		Scheme: "mongodb",
		Host:   fmt.Sprintf("localhost:%d", port),
		Path:   "/" + db.DatabaseName,
	}
	if db.User != "" || db.Password != "" {
		// url.UserPassword escapes '@', ':', '/' so passwords don't break the URI.
		u.User = url.UserPassword(defaultStr(db.User, "mongo"), db.Password)
	}
	return []string{u.String()}
}

func mongoExecArgs(db DatabaseService, query string) []string {
	return append(mongoArgs(db), "--quiet", "--eval", query)
}

func mysqlArgs(db DatabaseService) []string {
	args := []string{"-u", defaultStr(db.User, "root")}
	if db.Password != "" {
		args = append(args, fmt.Sprintf("-p%s", db.Password))
	}
	if db.DatabaseName != "" {
		args = append(args, db.DatabaseName)
	}
	return args
}

func mysqlExecArgs(db DatabaseService, query string) []string {
	return append(mysqlArgs(db), "-e", query)
}

func mssqlArgs(db DatabaseService) []string {
	return []string{
		"-U", defaultStr(db.User, "sa"),
		"-P", db.Password,
		"-d", defaultStr(db.DatabaseName, "master"),
	}
}

func mssqlExecArgs(db DatabaseService, query string) []string {
	return append(mssqlArgs(db), "-Q", query)
}

func cassandraArgs(db DatabaseService) []string {
	args := []string{"localhost"}
	if db.User != "" {
		args = append(args, "-u", db.User, "-p", db.Password)
	}
	return args
}

func cassandraExecArgs(db DatabaseService, query string) []string {
	return append(cassandraArgs(db), "-e", query)
}

// driverShells maps driver names to their interactive shell configurations.
var driverShells = map[string]shellConfig{
	"postgres":     {cmd: "psql", argsFunc: postgresArgs, execArgsFunc: postgresExecArgs},
	"postgis":      {cmd: "psql", argsFunc: postgresArgs, execArgsFunc: postgresExecArgs},
	"pgvector":     {cmd: "psql", argsFunc: postgresArgs, execArgsFunc: postgresExecArgs},
	"timescaledb":  {cmd: "psql", argsFunc: postgresArgs, execArgsFunc: postgresExecArgs},
	"cockroachdb": {
		cmd:          "cockroach",
		argsFunc:     func(db DatabaseService) []string { return []string{"sql", "--insecure"} },
		execArgsFunc: func(db DatabaseService, q string) []string { return []string{"sql", "--insecure", "-e", q} },
	},
	"yugabytedb": {
		cmd: "ysqlsh",
		argsFunc: func(db DatabaseService) []string {
			return []string{
				"-U", defaultStr(db.User, "yugabyte"),
				"-d", defaultStr(db.DatabaseName, "yugabyte"),
			}
		},
		execArgsFunc: func(db DatabaseService, q string) []string {
			return []string{
				"-U", defaultStr(db.User, "yugabyte"),
				"-d", defaultStr(db.DatabaseName, "yugabyte"),
				"-c", q,
			}
		},
	},
	"redis":        {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs},
	"redis-server": {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs},
	"keydb":        {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs},
	"dragonfly":    {cmd: redisCLITool, argsFunc: func(db DatabaseService) []string { return nil }, execArgsFunc: func(db DatabaseService, q string) []string { return strings.Fields(q) }},
	"redict":       {cmd: redisCLITool, argsFunc: func(db DatabaseService) []string { return nil }, execArgsFunc: func(db DatabaseService, q string) []string { return strings.Fields(q) }},
	"valkey":       {cmd: redisCLITool, argsFunc: func(db DatabaseService) []string { return nil }, execArgsFunc: func(db DatabaseService, q string) []string { return strings.Fields(q) }},
	"mongodb":      {cmd: "mongosh", argsFunc: mongoArgs, execArgsFunc: mongoExecArgs},
	"mysql":        {cmd: "mysql", argsFunc: mysqlArgs, execArgsFunc: mysqlExecArgs},
	"mariadb":      {cmd: "mysql", argsFunc: mysqlArgs, execArgsFunc: mysqlExecArgs},
	"mssql":        {cmd: "sqlcmd", argsFunc: mssqlArgs, execArgsFunc: mssqlExecArgs},
	"cassandra":    {cmd: "cqlsh", argsFunc: cassandraArgs, execArgsFunc: cassandraExecArgs},
	"scylla": {
		cmd:          "cqlsh",
		argsFunc:     func(db DatabaseService) []string { return []string{"localhost"} },
		execArgsFunc: func(db DatabaseService, q string) []string { return []string{"localhost", "-e", q} },
	},
}

// OpenDBShell drops the user into an interactive psql/mongosh/etc. inside
// the db_service's running container.
func OpenDBShell(db DatabaseService) error {
	return runDBShell(db, "", true)
}

// ExecDBQuery runs a single query against the db_service's container and
// writes the tool's output to stdout. Exits with the tool's exit code.
func ExecDBQuery(db DatabaseService, query string) error {
	return runDBShell(db, query, false)
}

func runDBShell(db DatabaseService, query string, interactive bool) error {
	cfg, ok := driverShells[db.Driver]
	if !ok {
		return fmt.Errorf("no interactive shell defined for driver %q\n"+
			"Tip: connect manually with the generated env in corgi_services/db_services/%s/.env",
			db.Driver, db.ServiceName)
	}

	containerName := fmt.Sprintf("%s-%s", db.Driver, db.ServiceName)
	containerID, err := getRunningContainerID(containerName)
	if err != nil {
		return fmt.Errorf("cannot find running container for %s: %w", db.ServiceName, err)
	}

	dockerArgs := []string{"exec"}
	if interactive {
		dockerArgs = append(dockerArgs, "-it")
	}
	dockerArgs = append(dockerArgs, containerID, cfg.cmd)
	if interactive {
		dockerArgs = append(dockerArgs, cfg.argsFunc(db)...)
	} else {
		if cfg.execArgsFunc == nil {
			return fmt.Errorf("driver %q does not support --exec", db.Driver)
		}
		dockerArgs = append(dockerArgs, cfg.execArgsFunc(db, query)...)
	}

	filtered := dockerArgs[:0]
	for _, a := range dockerArgs {
		if a != "" {
			filtered = append(filtered, a)
		}
	}

	cmd := exec.Command("docker", filtered...) // NOSONAR — docker is a known system binary
	if interactive {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// getRunningContainerID looks up a container by exact name match (anchored
// regex), so a "postgres-api" filter never picks up "postgres-api-staging".
func getRunningContainerID(containerName string) (string, error) {
	out, err := exec.Command( // NOSONAR — docker is a known system binary
		"docker", "ps", "--filter", fmt.Sprintf("name=^%s$", containerName),
		"--format", "{{.ID}}",
	).Output()
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return "", fmt.Errorf("container %q is not running (start it with: corgi db --upAll)", containerName)
	}
	return strings.SplitN(id, "\n", 2)[0], nil
}

// SupportedShellDrivers returns the list of driver names that have a shell defined.
func SupportedShellDrivers() []string {
	names := make([]string, 0, len(driverShells))
	for k := range driverShells {
		names = append(names, k)
	}
	return names
}

func defaultStr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
