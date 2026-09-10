package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SkipCorgiServicesMigration lets `corgi migrate` do the move itself.
var SkipCorgiServicesMigration bool

const (
	CorgiDirName      = ".corgi"
	CorgiServicesName = "corgi_services"
)

// CorgiServicesRelIn is the state folder relative to composeDir:
// .corgi/corgi_services, or the bare legacy folder while one is still there.
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

// ComposeDirOf walks back up from a path under the state folder.
func ComposeDirOf(corgiServicesPath string) string {
	dir := filepath.Dir(corgiServicesPath)
	if filepath.Base(dir) == CorgiDirName {
		return filepath.Dir(dir)
	}
	return dir
}

// MigrateCorgiServices moves a legacy corgi_services/ under .corgi/. It
// refuses while services are up, so nothing loses the paths it started with.
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
		return false, fmt.Errorf("both %s and %s exist — merge them by hand, then delete %s",
			legacy, target, legacy)
	}
	if names := runningIn(legacy); len(names) > 0 {
		return false, fmt.Errorf("%s still runs %s — `corgi stop` first", legacy, strings.Join(names, ", "))
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

// repairWorktrees re-points each source repo at the worktree's new path. Git
// records it absolutely, so without this `git worktree list` calls the moved
// checkout prunable and the next prune drops the branch's admin files.
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

// retargetRunState rewrites the absolute paths the last run recorded, so a
// log file the board still points at is found where the folder now is.
func retargetRunState(legacy, target string) {
	for _, name := range []string{".state.json", ".state.last.json"} {
		path := filepath.Join(target, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		moved := strings.ReplaceAll(string(data), legacy+string(filepath.Separator), target+string(filepath.Separator))
		if moved == string(data) {
			continue
		}
		_ = os.WriteFile(path, []byte(moved), 0o644)
	}
}

// runningIn is what would lose its paths if the folder moved. A "running"
// row left behind by a crash or a reboot is not one of them, so every row is
// probed: a service by its pid, a db service by its container.
func runningIn(corgiServicesPath string) []string {
	state, err := ReadRunState(filepath.Join(corgiServicesPath, ".state.json"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range state.Services {
		if e.Status != "running" {
			continue
		}
		// pid==0 is container-managed; its container is the truth.
		if e.PID == 0 {
			if containerRunning(e.Container) {
				names = append(names, e.Name)
			}
			continue
		}
		if PidAlive(e.PID, e.Command) {
			names = append(names, e.Name)
		}
	}
	for _, e := range state.DBServices {
		if e.Status == "running" && containerRunning(e.Container) {
			names = append(names, e.Name)
		}
	}
	return names
}

// containerRunning asks docker, and says no when docker cannot answer: a
// stopped daemon holds nothing that a moved folder could break.
func containerRunning(container string) bool {
	if strings.TrimSpace(container) == "" {
		return false
	}
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", container).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func retargetGitignore(composeDir string) {
	path := filepath.Join(composeDir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	nested := CorgiDirName + "/" + CorgiServicesName
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, line := range lines {
		bare := strings.TrimSpace(line)
		negate := strings.HasPrefix(bare, "!")
		bare = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(bare, "!"), "/"), "./")
		if bare != CorgiServicesName && !strings.HasPrefix(bare, CorgiServicesName+"/") {
			continue
		}
		lines[i] = nested + strings.TrimPrefix(bare, CorgiServicesName)
		if negate {
			lines[i] = "!" + lines[i]
		}
		changed = true
	}
	if !changed {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
