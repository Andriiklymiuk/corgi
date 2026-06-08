package utils

import (
	"strings"
	"testing"
)

func TestBuildPgDumpCommand_SecretsInEnvNotArgv(t *testing.T) {
	db := DatabaseService{
		Host: "db.example.com", Port: 5432, User: "u",
		Password: "p'; rm -rf /", DatabaseName: "app",
	}
	name, args, env := buildPgDumpCommand(db, "dump.sql")
	if name != "pg_dump" {
		t.Fatalf("cmd = %q, want pg_dump", name)
	}
	for _, a := range args {
		if strings.Contains(a, db.Password) {
			t.Fatalf("password leaked into argv: %v", args)
		}
	}
	if got := env["PGPASSWORD"]; got != db.Password {
		t.Fatalf("PGPASSWORD = %q, want raw password", got)
	}
	if !containsArg(args, "--host", "db.example.com") || !containsArg(args, "--username", "u") {
		t.Fatalf("missing host/username flags: %v", args)
	}
}

func TestBuildPgSeedCommand_ContainerIdNotShellExpanded(t *testing.T) {
	const id = "abc123$(reboot)"
	db := DatabaseService{User: "u", DatabaseName: "app"}
	name, args := buildPgSeedCommand(id, db)
	if name != "docker" {
		t.Fatalf("cmd = %q, want docker", name)
	}
	// the container id is one argv element, passed verbatim (never re-expanded).
	want := []string{"exec", "-i", id, "psql", "-U", "u", "-d", "app"}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("args = %v, want %v", args, want)
	}
}

// containsArg reports whether args contains flag immediately followed by val.
func containsArg(args []string, flag, val string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == val {
			return true
		}
	}
	return false
}
