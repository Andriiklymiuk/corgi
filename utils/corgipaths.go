package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var SkipCorgiServicesMigration bool

const (
	CorgiDirName      = ".corgi"
	CorgiServicesName = "corgi_services"
)

func CorgiServicesRelIn(composeDir string) string {
	nested := filepath.Join(CorgiDirName, CorgiServicesName)
	if composeDir == "" {
		return nested
	}
	if _, err := os.Stat(filepath.Join(composeDir, nested)); err == nil {
		return nested
	}
	if _, err := os.Stat(filepath.Join(composeDir, CorgiServicesName)); err == nil {
		return CorgiServicesName
	}
	return nested
}

func CorgiServicesIn(composeDir string) string {
	return filepath.Join(composeDir, CorgiServicesRelIn(composeDir))
}

func CorgiServicesRel() string { return CorgiServicesRelIn(CorgiComposePathDir) }

func CorgiServicesDir() string { return CorgiServicesIn(CorgiComposePathDir) }

func DbServicesRel() string { return filepath.Join(CorgiServicesRel(), "db_services") }

func ServicesRel() string { return filepath.Join(CorgiServicesRel(), "services") }

func DbServicesIn(composeDir string) string {
	return filepath.Join(CorgiServicesIn(composeDir), "db_services")
}

func ServicesIn(composeDir string) string {
	return filepath.Join(CorgiServicesIn(composeDir), "services")
}

func ComposeDirOf(corgiServicesPath string) string {
	dir := filepath.Dir(corgiServicesPath)
	if filepath.Base(dir) == CorgiDirName {
		return filepath.Dir(dir)
	}
	return dir
}

func MigrateCorgiServices(composeDir string) (bool, error) {
	if composeDir == "" {
		return false, nil
	}
	legacy := filepath.Join(composeDir, CorgiServicesName)
	if _, err := os.Stat(legacy); err != nil {
		return false, nil
	}
	target := filepath.Join(composeDir, CorgiDirName, CorgiServicesName)
	if _, err := os.Stat(target); err == nil {
		return false, fmt.Errorf("both %s and %s exist - merge them by hand, then delete %s",
			legacy, target, legacy)
	}
	if names := runningIn(legacy); len(names) > 0 {
		return false, fmt.Errorf("%s still runs %s - `corgi stop` first", legacy, strings.Join(names, ", "))
	}
	if err := os.MkdirAll(filepath.Join(composeDir, CorgiDirName), 0o755); err != nil {
		return false, err
	}
	if err := os.Rename(legacy, target); err != nil {
		return false, fmt.Errorf("couldn't move %s to %s: %w", legacy, target, err)
	}
	retargetGitignore(composeDir)
	retargetRunState(legacy, target)
	repairWorktrees(target)
	return true, nil
}

func repairWorktrees(target string) {
	entries, err := os.ReadDir(filepath.Join(target, ".worktrees"))
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(target, ".worktrees", e.Name())
		c := exec.Command("git", "worktree", "repair")
		c.Dir = dir
		_ = c.Run()
	}
}

func retargetRunState(legacy, target string) {
	prefix := legacy + string(filepath.Separator)
	move := func(path string) string {
		if strings.HasPrefix(path, prefix) {
			return filepath.Join(target, strings.TrimPrefix(path, prefix))
		}
		return path
	}
	for _, name := range []string{".state.json", ".state.last.json"} {
		path := filepath.Join(target, name)
		state, err := ReadRunState(path)
		if err != nil {
			continue
		}
		changed := false
		for _, rows := range [][]RunStateEntry{state.Services, state.DBServices} {
			for i := range rows {
				if moved := move(rows[i].LogFile); moved != rows[i].LogFile {
					rows[i].LogFile = moved
					changed = true
				}
			}
		}
		if !changed {
			continue
		}
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			continue
		}
		_ = os.WriteFile(path, data, 0o644)
	}
}

func runningIn(corgiServicesPath string) []string {
	state, err := ReadRunState(filepath.Join(corgiServicesPath, ".state.json"))
	if err != nil {
		return nil
	}
	var names []string
	var ask []string
	byContainer := map[string]string{}
	for _, rows := range [][]RunStateEntry{state.Services, state.DBServices} {
		for _, e := range rows {
			if e.Status != "running" {
				continue
			}
			if e.PID > 0 {
				if PidAlive(e.PID, e.Command) {
					names = append(names, e.Name)
				}
				continue
			}
			if c := strings.TrimSpace(e.Container); c != "" {
				ask = append(ask, c)
				byContainer[c] = e.Name
			}
		}
	}
	for _, c := range runningContainers(ask) {
		names = append(names, byContainer[c])
	}
	return names
}

func runningContainers(containers []string) []string {
	if len(containers) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	args := append([]string{"inspect", "-f", "{{.Name}} {{.State.Running}}"}, containers...)
	out, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	var up []string
	for _, line := range strings.Split(string(out), "\n") {
		name, state, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || state != "true" {
			continue
		}
		up = append(up, strings.TrimPrefix(name, "/"))
	}
	return up
}

func retargetGitignore(composeDir string) {
	path := filepath.Join(composeDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	nested := CorgiDirName + "/" + CorgiServicesName
	lines := strings.Split(string(data), "\n")
	if ignoresCorgiDir(lines) || hasRule(lines, nested) {
		return
	}
	out := make([]string, 0, len(lines)+2)
	added := false
	for _, line := range lines {
		bare, negate, ok := legacyRule(line)
		if !ok {
			out = append(out, line)
			continue
		}
		rule := nested + strings.TrimPrefix(bare, CorgiServicesName)
		if negate {
			rule = "!" + rule
		}
		if !hasRule(out, rule) {
			out = append(out, rule)
			added = true
		}
		out = append(out, line)
	}
	if !added {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

func legacyRule(line string) (bare string, negate bool, ok bool) {
	bare = strings.TrimSpace(line)
	negate = strings.HasPrefix(bare, "!")
	bare = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(bare, "!"), "/"), "./")
	if bare != CorgiServicesName && !strings.HasPrefix(bare, CorgiServicesName+"/") {
		return "", false, false
	}
	return bare, negate, true
}

func ignoresCorgiDir(lines []string) bool {
	for _, line := range lines {
		bare := strings.TrimSpace(line)
		if strings.HasPrefix(bare, "!") {
			continue
		}
		bare = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(bare, "/"), "./"), "/")
		if bare == CorgiDirName {
			return true
		}
	}
	return false
}

func hasRule(lines []string, rule string) bool {
	for _, line := range lines {
		if strings.TrimSpace(line) == rule {
			return true
		}
	}
	return false
}
