package utils

import (
	"strings"
	"testing"
)

func TestDriverShells_DragonflyAuthViaEnv(t *testing.T) {
	for _, d := range []string{"dragonfly", "redict", "valkey"} {
		cfg := driverShells[d]
		db := DatabaseService{Password: "secret"}
		if args := cfg.argsFunc(db); len(args) != 0 {
			t.Errorf("%s: password must not be on argv, got %v", d, args)
		}
		if cfg.envFunc == nil || cfg.envFunc(db)["REDISCLI_AUTH"] != "secret" {
			t.Errorf("%s: expected REDISCLI_AUTH=secret via env", d)
		}
	}
}

func TestBuildDockerExecArgs_MysqlPasswordViaEnvNotArgv(t *testing.T) {
	cfg := driverShells["mysql"]
	db := DatabaseService{User: "root", Password: "p w", DatabaseName: "d"}
	args, env, err := buildDockerExecArgs(cfg, db, "", "cid", true)
	if err != nil {
		t.Fatal(err)
	}
	// The secret must never reach the mysql CLI as a -p<pw> flag (visible in `ps`).
	for _, a := range args {
		if strings.HasPrefix(a, "-p") && strings.Contains(a, db.Password) {
			t.Fatalf("password leaked onto mysql CLI argv: %v", args)
		}
	}
	if env["MYSQL_PWD"] != db.Password {
		t.Fatalf("MYSQL_PWD = %q, want %q", env["MYSQL_PWD"], db.Password)
	}
	// the env value must be carried as `docker exec -e MYSQL_PWD=...` before the container id.
	if !containsArg(args, "-e", "MYSQL_PWD="+db.Password) {
		t.Fatalf("expected -e MYSQL_PWD=<pw> in docker args, got %v", args)
	}
}

func TestSupportedShellDrivers_NotEmpty(t *testing.T) {
	drivers := SupportedShellDrivers()
	if len(drivers) == 0 {
		t.Error("expected at least one supported shell driver")
	}
}

func TestDriverShells_KeyDriversPresent(t *testing.T) {
	required := []string{"postgres", "redis", "mongodb", "mysql", "mariadb", "mssql", "cassandra"}
	for _, d := range required {
		if _, ok := driverShells[d]; !ok {
			t.Errorf("expected shell config for driver %q", d)
		}
	}
}

func TestDefaultStr(t *testing.T) {
	if defaultStr("", "fallback") != "fallback" {
		t.Error("expected fallback for empty string")
	}
	if defaultStr("value", "fallback") != "value" {
		t.Error("expected value when non-empty")
	}
}

func TestOpenDBShell_UnknownDriver(t *testing.T) {
	db := DatabaseService{
		Driver:      "unknowndriver",
		ServiceName: "test",
	}
	err := OpenDBShell(db)
	if err == nil {
		t.Fatal("expected error for unknown driver")
	}
	if !strings.Contains(err.Error(), "unknowndriver") {
		t.Errorf("expected driver name in error, got: %v", err)
	}
}

func TestDriverShells_PostgresArgs(t *testing.T) {
	cfg := driverShells["postgres"]
	db := DatabaseService{User: "myuser", DatabaseName: "mydb"}
	args := cfg.argsFunc(db)
	if len(args) == 0 {
		t.Error("expected args for postgres")
	}
	// Should contain -U myuser
	found := false
	for i, a := range args {
		if a == "-U" && i+1 < len(args) && args[i+1] == "myuser" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected -U myuser in args, got %v", args)
	}
}

func TestDriverShells_RedisNoPassword(t *testing.T) {
	cfg := driverShells["redis"]
	db := DatabaseService{}
	args := cfg.argsFunc(db)
	if len(args) != 0 {
		t.Errorf("expected empty args for redis without password, got %v", args)
	}
}

func TestDriverShells_RedisWithPassword(t *testing.T) {
	cfg := driverShells["redis"]
	db := DatabaseService{Password: "secret"}
	if args := cfg.argsFunc(db); len(args) != 0 {
		t.Errorf("expected no -a on argv for redis, got %v", args)
	}
	if cfg.envFunc(db)["REDISCLI_AUTH"] != "secret" {
		t.Errorf("expected REDISCLI_AUTH=secret via env for redis")
	}
}

func TestExecArgs_PostgresAppendsDashC(t *testing.T) {
	cfg := driverShells["postgres"]
	args := cfg.execArgsFunc(DatabaseService{User: "u", DatabaseName: "d"}, "SELECT 1")
	if got := args[len(args)-2:]; got[0] != "-c" || got[1] != "SELECT 1" {
		t.Errorf("expected trailing -c SELECT 1, got %v", args)
	}
}

func TestExecArgs_MysqlSkipsPwhenEmpty(t *testing.T) {
	cfg := driverShells["mysql"]
	args := cfg.execArgsFunc(DatabaseService{User: "u"}, "SELECT 1")
	for _, a := range args {
		if strings.HasPrefix(a, "-p") {
			t.Errorf("expected no -p<empty>, got %v", args)
		}
	}
}

func TestExecArgs_MongoNoAuthSegmentWhenEmpty(t *testing.T) {
	cfg := driverShells["mongodb"]
	args := cfg.argsFunc(DatabaseService{Port: 27017})
	if !strings.HasPrefix(args[0], "mongodb://localhost:") {
		t.Errorf("expected anonymous mongodb URI, got %v", args)
	}
}

func TestExecArgs_MongoEscapesSpecialCharsInPassword(t *testing.T) {
	cfg := driverShells["mongodb"]
	args := cfg.argsFunc(DatabaseService{
		User:     "admin",
		Password: "p@ss:w/rd",
		Port:     27017,
	})
	uri := args[0]
	if strings.Contains(uri, "p@ss:w/rd") {
		t.Errorf("password should be percent-encoded, got raw in URI: %s", uri)
	}
	// '@' (0x40), ':' (0x3A), '/' (0x2F) must all be escaped in the password.
	for _, want := range []string{"%40", "%3A", "%2F"} {
		if !strings.Contains(uri, want) {
			t.Errorf("expected %s in escaped URI, got %s", want, uri)
		}
	}
}

func TestExecArgs_RedisTokenizesQuery(t *testing.T) {
	cfg := driverShells["redis"]
	args := cfg.execArgsFunc(DatabaseService{}, "GET mykey")
	if len(args) < 2 || args[len(args)-2] != "GET" || args[len(args)-1] != "mykey" {
		t.Errorf("expected trailing GET mykey, got %v", args)
	}
}

func TestExecArgs_ScyllaSupportsExec(t *testing.T) {
	cfg := driverShells["scylla"]
	if cfg.execArgsFunc == nil {
		t.Fatal("scylla should support --exec")
	}
	args := cfg.execArgsFunc(DatabaseService{}, "SELECT now() FROM system.local")
	if args[len(args)-2] != "-e" {
		t.Errorf("expected -e <query>, got %v", args)
	}
}

func TestArgs_CassandraNoAuth(t *testing.T) {
	args := cassandraArgs(DatabaseService{})
	if len(args) != 1 || args[0] != "localhost" {
		t.Errorf("expected [localhost] without auth, got %v", args)
	}
}

func TestArgs_CassandraWithAuth(t *testing.T) {
	args := cassandraArgs(DatabaseService{User: "cassandra", Password: "pw"})
	want := []string{"localhost", "-u", "cassandra", "-p", "pw"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", args, want)
	}
	exec := cassandraExecArgs(DatabaseService{}, "SELECT now() FROM system.local")
	if exec[len(exec)-2] != "-e" {
		t.Errorf("expected trailing -e <query>, got %v", exec)
	}
}

func TestArgs_MssqlDefaultsAndExec(t *testing.T) {
	joined := strings.Join(mssqlArgs(DatabaseService{Password: "secret"}), " ")
	for _, want := range []string{"-U sa", "-P secret", "-d master"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in %s", want, joined)
		}
	}
	exec := mssqlExecArgs(DatabaseService{Password: "secret"}, "SELECT 1")
	if exec[len(exec)-2] != "-Q" || exec[len(exec)-1] != "SELECT 1" {
		t.Errorf("expected trailing -Q SELECT 1, got %v", exec)
	}
}

func TestArgs_MongoExecAppendsEval(t *testing.T) {
	args := mongoExecArgs(DatabaseService{Port: 27017}, "db.users.find()")
	if args[len(args)-2] != "--eval" || args[len(args)-1] != "db.users.find()" {
		t.Errorf("expected trailing --eval <query>, got %v", args)
	}
}

func TestExecDBQuery_UnknownDriver(t *testing.T) {
	err := ExecDBQuery(DatabaseService{Driver: "nope", ServiceName: "svc"}, "SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected error naming driver, got %v", err)
	}
}

// TestDriverArgBuilders_AllDrivers walks every registered driver and asserts
// the shape of both the interactive and (where supported) --exec arg lists.
func TestDriverArgBuilders_AllDrivers(t *testing.T) {
	const q = "SELECT 1"
	cases := []struct {
		driver       string
		db           DatabaseService
		wantArgs     []string // interactive
		wantExecTail []string // trailing tokens of execArgs (nil = skip exec check)
	}{
		{
			driver:       "postgres",
			db:           DatabaseService{User: "u", DatabaseName: "d"},
			wantArgs:     []string{"-U", "u", "-d", "d"},
			wantExecTail: []string{"-c", q},
		},
		{
			driver:       "postgres",
			db:           DatabaseService{},
			wantArgs:     []string{"-U", "postgres", "-d", "postgres"},
			wantExecTail: []string{"-c", q},
		},
		{
			driver:       "yugabytedb",
			db:           DatabaseService{},
			wantArgs:     []string{"-U", "yugabyte", "-d", "yugabyte"},
			wantExecTail: []string{"-c", q},
		},
		{
			driver:       "cockroachdb",
			db:           DatabaseService{},
			wantArgs:     []string{"sql", "--insecure"},
			wantExecTail: []string{"--insecure", "-e", q},
		},
		{
			driver:       "redis",
			db:           DatabaseService{Password: "pw"},
			wantArgs:     nil, // password rides REDISCLI_AUTH env, not argv
			wantExecTail: []string{"GET", "k"},
		},
		{
			driver:       "dragonfly",
			db:           DatabaseService{Password: "pw"},
			wantArgs:     nil,
			wantExecTail: []string{"GET", "k"},
		},
		{
			driver:       "mysql",
			db:           DatabaseService{User: "u", Password: "pw", DatabaseName: "d"},
			wantArgs:     []string{"-u", "u", "d"}, // password rides MYSQL_PWD env, not argv
			wantExecTail: []string{"-e", q},
		},
		{
			driver:       "mariadb",
			db:           DatabaseService{},
			wantArgs:     []string{"-u", "root"},
			wantExecTail: []string{"-e", q},
		},
		{
			driver:       "mssql",
			db:           DatabaseService{User: "sa", Password: "pw", DatabaseName: "m"},
			wantArgs:     []string{"-U", "sa", "-P", "pw", "-d", "m"},
			wantExecTail: []string{"-Q", q},
		},
		{
			driver:       "cassandra",
			db:           DatabaseService{User: "c", Password: "pw"},
			wantArgs:     []string{"localhost", "-u", "c", "-p", "pw"},
			wantExecTail: []string{"-e", q},
		},
		{
			driver:       "scylla",
			db:           DatabaseService{},
			wantArgs:     []string{"localhost"},
			wantExecTail: []string{"localhost", "-e", q},
		},
	}

	for _, tc := range cases {
		t.Run(tc.driver, func(t *testing.T) {
			cfg, ok := driverShells[tc.driver]
			if !ok {
				t.Fatalf("no shell config for %q", tc.driver)
			}
			query := q
			if cfg.cmd == redisCLITool {
				query = "GET k"
			}
			if got := cfg.argsFunc(tc.db); strings.Join(got, " ") != strings.Join(tc.wantArgs, " ") {
				t.Errorf("argsFunc = %v, want %v", got, tc.wantArgs)
			}
			if tc.wantExecTail == nil {
				return
			}
			exec := cfg.execArgsFunc(tc.db, query)
			tail := exec[len(exec)-len(tc.wantExecTail):]
			if strings.Join(tail, " ") != strings.Join(tc.wantExecTail, " ") {
				t.Errorf("execArgs tail = %v, want %v (full %v)", tail, tc.wantExecTail, exec)
			}
		})
	}
}

func TestMongoArgs_DefaultPortAndUser(t *testing.T) {
	// Port 0 falls back to 27017; user empty + password set defaults user to "mongo".
	args := mongoArgs(DatabaseService{Password: "pw", DatabaseName: "app"})
	uri := args[0]
	if !strings.Contains(uri, "localhost:27017") {
		t.Errorf("expected default port 27017 in %s", uri)
	}
	if !strings.Contains(uri, "mongo:") {
		t.Errorf("expected default user 'mongo' in %s", uri)
	}
	if !strings.HasSuffix(uri, "/app") {
		t.Errorf("expected db path /app in %s", uri)
	}
}

func TestMongoArgs_CustomPort(t *testing.T) {
	args := mongoArgs(DatabaseService{Port: 30000})
	if !strings.Contains(args[0], "localhost:30000") {
		t.Errorf("expected custom port, got %s", args[0])
	}
}

func TestMysqlArgs_NoPasswordNoDb(t *testing.T) {
	// No password, no db name → only -u <user>.
	args := mysqlArgs(DatabaseService{User: "admin"})
	if strings.Join(args, " ") != "-u admin" {
		t.Errorf("expected [-u admin], got %v", args)
	}
}

func TestBuildDockerExecArgs_InteractivePostgres(t *testing.T) {
	cfg := driverShells["postgres"]
	got, _, err := buildDockerExecArgs(cfg, DatabaseService{User: "u", DatabaseName: "d"}, "", "abc123", true)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "-it", "abc123", "psql", "-U", "u", "-d", "d"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildDockerExecArgs_NonInteractiveMysql(t *testing.T) {
	cfg := driverShells["mysql"]
	got, _, err := buildDockerExecArgs(cfg, DatabaseService{User: "root", Password: "pw", DatabaseName: "d"}, "SELECT 1", "cid", false)
	if err != nil {
		t.Fatal(err)
	}
	// password rides `-e MYSQL_PWD=pw` (before the container id), never on argv.
	want := []string{"exec", "-e", "MYSQL_PWD=pw", "cid", "mysql", "-u", "root", "d", "-e", "SELECT 1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildDockerExecArgs_NonInteractiveMongo(t *testing.T) {
	cfg := driverShells["mongodb"]
	got, _, err := buildDockerExecArgs(cfg, DatabaseService{Port: 27017}, "db.x.find()", "cid", false)
	if err != nil {
		t.Fatal(err)
	}
	// exec cid mongosh <uri> --quiet --eval db.x.find()
	if got[0] != "exec" || got[1] != "cid" || got[2] != "mongosh" {
		t.Errorf("unexpected prefix: %v", got)
	}
	if got[len(got)-2] != "--eval" || got[len(got)-1] != "db.x.find()" {
		t.Errorf("expected trailing --eval query, got %v", got)
	}
}

func TestBuildDockerExecArgs_DropsEmptyTokens(t *testing.T) {
	// redis with no password → argsFunc returns nil, no stray "" left in output.
	cfg := driverShells["redis"]
	got, _, err := buildDockerExecArgs(cfg, DatabaseService{}, "", "cid", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range got {
		if a == "" {
			t.Errorf("empty token leaked into args: %v", got)
		}
	}
	want := []string{"exec", "-it", "cid", redisCLITool}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBuildDockerExecArgs_NoExecSupport(t *testing.T) {
	// A config without execArgsFunc must error in non-interactive mode.
	cfg := shellConfig{cmd: "x", argsFunc: func(DatabaseService) []string { return nil }}
	_, _, err := buildDockerExecArgs(cfg, DatabaseService{Driver: "x"}, "q", "cid", false)
	if err == nil || !strings.Contains(err.Error(), "does not support --exec") {
		t.Fatalf("expected --exec unsupported error, got %v", err)
	}
}

func TestExecDBQueryCapture_UnknownDriver(t *testing.T) {
	// Unknown driver returns a clean error before any docker call, no panic.
	out, err := ExecDBQueryCapture(DatabaseService{Driver: "nope", ServiceName: "svc"}, "SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected error naming driver, got %v (out=%q)", err, out)
	}
	if out != "" {
		t.Errorf("expected empty output on driver error, got %q", out)
	}
}
