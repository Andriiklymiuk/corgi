package utils

import (
	"strings"
	"testing"
)

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
	args := cfg.argsFunc(db)
	if len(args) < 2 || args[0] != "-a" || args[1] != "secret" {
		t.Errorf("expected [-a secret] for redis with password, got %v", args)
	}
}

func TestExecArgs_PostgresAppendsDashC(t *testing.T) {
	cfg := driverShells["postgres"]
	args := cfg.execArgsFunc(DatabaseService{User: "u", DatabaseName: "d"}, "SELECT 1")
	if got := args[len(args)-2 : len(args)]; got[0] != "-c" || got[1] != "SELECT 1" {
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
