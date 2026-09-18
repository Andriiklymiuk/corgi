package utils

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const redisCLITool = "redis-cli"

// envFunc secrets go in via `docker exec -e`, never argv.
type shellConfig struct {
	cmd          string
	argsFunc     func(db DatabaseService) []string
	execArgsFunc func(db DatabaseService, query string) []string
	envFunc      func(db DatabaseService) map[string]string
}

func mysqlEnv(db DatabaseService) map[string]string {
	if db.Password == "" {
		return nil
	}
	return map[string]string{"MYSQL_PWD": db.Password}
}

func redisEnv(db DatabaseService) map[string]string {
	if db.Password == "" {
		return nil
	}
	return map[string]string{"REDISCLI_AUTH": db.Password}
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
	return nil
}

func redisExecArgs(db DatabaseService, query string) []string {
	args := redisArgs(db)
	return append(args, strings.Fields(query)...)
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
		u.User = url.UserPassword(defaultStr(db.User, "mongo"), db.Password)
	}
	return []string{u.String()}
}

func mongoExecArgs(db DatabaseService, query string) []string {
	return append(mongoArgs(db), "--quiet", "--eval", query)
}

func mysqlArgs(db DatabaseService) []string {
	args := []string{"-u", defaultStr(db.User, "root")}
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

var driverShells = map[string]shellConfig{
	"postgres":    {cmd: "psql", argsFunc: postgresArgs, execArgsFunc: postgresExecArgs},
	"postgis":     {cmd: "psql", argsFunc: postgresArgs, execArgsFunc: postgresExecArgs},
	"pgvector":    {cmd: "psql", argsFunc: postgresArgs, execArgsFunc: postgresExecArgs},
	"timescaledb": {cmd: "psql", argsFunc: postgresArgs, execArgsFunc: postgresExecArgs},
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
	"redis":        {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs, envFunc: redisEnv},
	"redis-server": {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs, envFunc: redisEnv},
	"keydb":        {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs, envFunc: redisEnv},
	"dragonfly":    {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs, envFunc: redisEnv},
	"redict":       {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs, envFunc: redisEnv},
	"valkey":       {cmd: redisCLITool, argsFunc: redisArgs, execArgsFunc: redisExecArgs, envFunc: redisEnv},
	"mongodb":      {cmd: "mongosh", argsFunc: mongoArgs, execArgsFunc: mongoExecArgs},
	"mysql":        {cmd: "mysql", argsFunc: mysqlArgs, execArgsFunc: mysqlExecArgs, envFunc: mysqlEnv},
	"mariadb":      {cmd: "mysql", argsFunc: mysqlArgs, execArgsFunc: mysqlExecArgs, envFunc: mysqlEnv},
	"mssql":        {cmd: "sqlcmd", argsFunc: mssqlArgs, execArgsFunc: mssqlExecArgs},
	"cassandra":    {cmd: "cqlsh", argsFunc: cassandraArgs, execArgsFunc: cassandraExecArgs},
	"scylla": {
		cmd:          "cqlsh",
		argsFunc:     func(db DatabaseService) []string { return []string{"localhost"} },
		execArgsFunc: func(db DatabaseService, q string) []string { return []string{"localhost", "-e", q} },
	},
}

func OpenDBShell(db DatabaseService) error {
	return runDBShell(db, "", true, os.Stdout, os.Stderr)
}

func ExecDBQuery(db DatabaseService, query string) error {
	return runDBShell(db, query, false, os.Stdout, os.Stderr)
}

func ExecDBQueryCapture(db DatabaseService, query string) (string, error) {
	var buf bytes.Buffer
	err := runDBShell(db, query, false, &buf, &buf)
	return buf.String(), err
}

func buildDockerExecArgs(cfg shellConfig, db DatabaseService, query, containerID string, interactive bool) ([]string, map[string]string, error) {
	dockerArgs := []string{"exec"}
	if interactive {
		dockerArgs = append(dockerArgs, "-it")
	}

	var env map[string]string
	if cfg.envFunc != nil {
		env = cfg.envFunc(db)
	}
	for _, k := range sortedKeys(env) {
		dockerArgs = append(dockerArgs, "-e", k+"="+env[k])
	}

	dockerArgs = append(dockerArgs, containerID, cfg.cmd)
	if interactive {
		dockerArgs = append(dockerArgs, cfg.argsFunc(db)...)
	} else {
		if cfg.execArgsFunc == nil {
			return nil, nil, fmt.Errorf("driver %q does not support --exec", db.Driver)
		}
		dockerArgs = append(dockerArgs, cfg.execArgsFunc(db, query)...)
	}

	filtered := dockerArgs[:0]
	for _, a := range dockerArgs {
		if a != "" {
			filtered = append(filtered, a)
		}
	}
	return filtered, env, nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func runDBShell(db DatabaseService, query string, interactive bool, stdout, stderr io.Writer) error {
	cfg, ok := driverShells[db.Driver]
	if !ok {
		return fmt.Errorf("no interactive shell defined for driver %q\n"+
			"Tip: connect manually with the generated env in corgi_services/db_services/%s/.env",
			db.Driver, db.ServiceName)
	}

	containerName := ContainerName(db.Driver, db.ServiceName)
	containerID, err := getRunningContainerID(containerName)
	if err != nil {
		return fmt.Errorf("cannot find running container for %s: %w", db.ServiceName, err)
	}

	filtered, _, err := buildDockerExecArgs(cfg, db, query, containerID, interactive)
	if err != nil {
		return err
	}

	cmd := exec.Command("docker", filtered...)
	if interactive {
		cmd.Stdin = os.Stdin
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func getRunningContainerID(containerName string) (string, error) {
	out, err := exec.Command(
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
