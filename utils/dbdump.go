package utils

import (
	"fmt"
	"os"
	"os/exec"
)

// buildPgDumpCommand builds the pg_dump argv for a source DB plus the env that
// carries the password (PGPASSWORD), so the secret never lands on argv where
// `ps` and a re-expanding `make` recipe could see it.
func buildPgDumpCommand(db DatabaseService, outFile string) (string, []string, map[string]string) {
	args := []string{
		"--host", db.Host,
		"--port", fmt.Sprintf("%d", db.Port),
		"--username", defaultStr(db.User, "postgres"),
		"-d", defaultStr(db.DatabaseName, "postgres"),
		"--blobs", "--no-owner", "--no-privileges",
		"--no-unlogged-table-data", "--format", "plain",
		"--file", outFile,
	}
	return "pg_dump", args, map[string]string{"PGPASSWORD": db.Password}
}

// RunPgDump runs pg_dump for db, writing the dump into serviceDir/outFile.
func RunPgDump(db DatabaseService, serviceDir, outFile string) error {
	name, args, extraEnv := buildPgDumpCommand(db, outFile)
	cmd := exec.Command(name, args...) // NOSONAR — pg_dump is a known tool
	cmd.Dir = serviceDir
	cmd.Env = mergeEnv(os.Environ(), extraEnv)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildPgSeedCommand builds the `docker exec -i <id> psql ...` argv that feeds a
// dump into a running postgres container. The container id is one argv element,
// never re-expanded by a shell.
func buildPgSeedCommand(containerID string, db DatabaseService) (string, []string) {
	return "docker", []string{
		"exec", "-i", containerID,
		"psql",
		"-U", defaultStr(db.User, "postgres"),
		"-d", defaultStr(db.DatabaseName, "postgres"),
	}
}

// RunPgSeed pipes dumpFile (resolved under serviceDir) into psql inside the
// container via stdin, so the dump path is never a shell argument either.
func RunPgSeed(serviceDir, dumpFile, containerID string, db DatabaseService) error {
	f, err := os.Open(joinIfRelative(serviceDir, dumpFile))
	if err != nil {
		return err
	}
	defer f.Close()

	name, args := buildPgSeedCommand(containerID, db)
	cmd := exec.Command(name, args...) // NOSONAR — docker is a known system binary
	cmd.Dir = serviceDir
	cmd.Stdin = f
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// joinIfRelative resolves name against dir unless it is already absolute.
func joinIfRelative(dir, name string) string {
	if len(name) > 0 && name[0] == '/' {
		return name
	}
	return dir + "/" + name
}

// mergeEnv returns base with extra appended as KEY=VALUE (extra wins on dup).
func mergeEnv(base []string, extra map[string]string) []string {
	out := append([]string(nil), base...)
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
