package utils

import (
	"fmt"
	"os"
	"os/exec"
)

// Password goes via PGPASSWORD env, never argv, so ps cannot see it.
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

func RunPgDump(db DatabaseService, serviceDir, outFile string) error {
	name, args, extraEnv := buildPgDumpCommand(db, outFile)
	cmd := exec.Command(name, args...)
	cmd.Dir = serviceDir
	cmd.Env = mergeEnv(os.Environ(), extraEnv)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func buildPgSeedCommand(containerID string, db DatabaseService) (string, []string) {
	return "docker", []string{
		"exec", "-i", containerID,
		"psql",
		"-U", defaultStr(db.User, "postgres"),
		"-d", defaultStr(db.DatabaseName, "postgres"),
	}
}

func RunPgSeed(serviceDir, dumpFile, containerID string, db DatabaseService) error {
	f, err := os.Open(joinIfRelative(serviceDir, dumpFile))
	if err != nil {
		return err
	}
	defer f.Close()

	name, args := buildPgSeedCommand(containerID, db)
	cmd := exec.Command(name, args...)
	cmd.Dir = serviceDir
	cmd.Stdin = f
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func joinIfRelative(dir, name string) string {
	if len(name) > 0 && name[0] == '/' {
		return name
	}
	return dir + "/" + name
}

func mergeEnv(base []string, extra map[string]string) []string {
	out := append([]string(nil), base...)
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
