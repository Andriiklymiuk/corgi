package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

type checkpointRepo struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Head     string `json:"head"`
	StashSha string `json:"stashSha,omitempty"`
}

type checkpointDatabase struct {
	Service  string `json:"service"`
	Snapshot string `json:"snapshot"`
}

type checkpointFile struct {
	Name      string               `json:"name"`
	CreatedAt time.Time            `json:"createdAt"`
	Repos     []checkpointRepo     `json:"repos"`
	Databases []checkpointDatabase `json:"databases,omitempty"`
}

var (
	checkpointWithDB bool
	restoreWithDB    bool
	restoreConfirmed bool
)

var checkpointNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var checkpointCmd = &cobra.Command{
	Use:   "checkpoint [name]",
	Short: "Mark every repo's state so a change can be undone in one command",
	Long: `Records where every repo in the workspace stands — its branch, its HEAD, and
its uncommitted work — under one name. corgi restore <name> puts all of it back.

Uncommitted work is captured with git stash create, so the working tree is not
touched and nothing lands in your stash list. A checkpoint costs nothing to keep.

--with-db also snapshots each postgres-family db_service, so a migration can be
undone alongside the code that ran it.`,
	Example: `corgi checkpoint before-referral

corgi checkpoint before-migration --with-db
corgi checkpoint list
corgi restore before-referral`,
	Args:    cobra.MaximumNArgs(1),
	Aliases: []string{"cp"},
	Run:     runCheckpoint,
}

var checkpointListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved checkpoints",
	Run:   runCheckpointList,
}

var checkpointRemoveCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"remove", "delete"},
	Short:   "Delete a checkpoint and release the work it was holding",
	Args:    cobra.ExactArgs(1),
	Run:     runCheckpointRemove,
}

var restoreCmd = &cobra.Command{
	Use:   "restore <name>",
	Short: "Put every repo back to a checkpoint",
	Long: `Restores each repo to the branch, commit and uncommitted work the checkpoint
recorded. A repo that has uncommitted work now is captured first, under a
safety checkpoint whose name is printed, so nothing is lost.`,
	Example: `corgi restore before-referral

corgi restore before-migration --with-db`,
	Args: cobra.ExactArgs(1),
	Run:  runRestore,
}

func init() {
	rootCmd.AddCommand(checkpointCmd)
	rootCmd.AddCommand(restoreCmd)
	checkpointCmd.AddCommand(checkpointListCmd, checkpointRemoveCmd)
	checkpointCmd.Flags().BoolVar(&checkpointWithDB, "with-db", false, "Also snapshot every postgres-family db_service")
	restoreCmd.Flags().BoolVar(&restoreWithDB, "with-db", false, "Also restore the db snapshots the checkpoint took")
	restoreCmd.Flags().BoolVar(&restoreConfirmed, "yes", false, "Do not ask before overwriting the current working trees")
}

func runCheckpoint(cmd *cobra.Command, args []string) {
	corgi := mustLoadCorgiServices(cmd)

	name := utils.DefaultSnapshotName(time.Now())
	if len(args) == 1 {
		name = args[0]
	}
	if !checkpointNameRe.MatchString(name) {
		exitWithError(utils.ErrUsage,
			fmt.Errorf("a checkpoint name may hold only letters, digits, dot, dash and underscore"), 2)
		return
	}

	file := checkpointFile{Name: name, CreatedAt: time.Now().UTC()}
	for _, target := range checkpointTargets(corgi) {
		repo, ok := captureRepo(target, name)
		if !ok {
			if state, isRepo := utils.ReadRepoState(target.path); isRepo && state.Dirty {
				exitWithError(utils.ErrConfig,
					fmt.Errorf("could not capture %s's uncommitted work; nothing was saved", target.name), 1)
				return
			}
			continue
		}
		file.Repos = append(file.Repos, repo)
	}
	if len(file.Repos) == 0 {
		exitWithError(utils.ErrConfig, fmt.Errorf("no git repositories to checkpoint"), 1)
		return
	}
	if checkpointWithDB {
		file.Databases = snapshotCheckpointDatabases(corgi, name)
	}

	if err := writeCheckpoint(file); err != nil {
		exitWithError(utils.ErrConfig, err, 1)
		return
	}

	if utils.JSONOutput {
		utils.PrintJSON(file)
		return
	}
	utils.Infof("📌 checkpoint %q\n", name)
	for _, repo := range file.Repos {
		utils.Infof("  %-16s %s%s\n", repo.Name, shortHead(repo), capturedSuffix(repo))
	}
	for _, db := range file.Databases {
		utils.Infof("  %-16s db snapshot %s\n", db.Service, db.Snapshot)
	}
}

func checkpointTargets(corgi *utils.CorgiCompose) []checkoutTarget {
	targets := []checkoutTarget{{name: workspaceRepoName, path: utils.CorgiComposePathDir}}
	var services []checkoutTarget
	for _, service := range corgi.Services {
		if service.AbsolutePath == "" {
			continue
		}
		services = append(services, checkoutTarget{name: service.ServiceName, path: service.AbsolutePath})
	}
	sort.Slice(services, func(i, j int) bool { return services[i].name < services[j].name })
	return append(targets, services...)
}

func captureRepo(target checkoutTarget, checkpoint string) (checkpointRepo, bool) {
	state, ok := utils.ReadRepoState(target.path)
	if !ok {
		return checkpointRepo{}, false
	}
	head, err := utils.RepoHead(target.path)
	if err != nil {
		return checkpointRepo{}, false
	}
	repo := checkpointRepo{Name: target.name, Path: target.path, Branch: state.Branch, Head: head}
	if state.Dirty {
		sha, capErr := utils.CaptureWorkTree(target.path, checkpoint, target.name)
		if capErr != nil {
			utils.Infof("could not capture uncommitted work in %s: %v\n", target.name, capErr)
			return checkpointRepo{}, false
		}
		repo.StashSha = sha
	}
	return repo, true
}

func snapshotCheckpointDatabases(corgi *utils.CorgiCompose, checkpoint string) []checkpointDatabase {
	if utils.IsStackSupervised(utils.CorgiComposePathDir) {
		utils.Info("a detached corgi run is managing this stack — stop it first to snapshot databases")
		return nil
	}
	var out []checkpointDatabase
	for _, db := range corgi.DatabaseServices {
		if !utils.IsPostgresFamilyDriver(db.Driver) {
			utils.Infof("%s: only postgres-family drivers can be snapshotted, skipped\n", db.ServiceName)
			continue
		}
		name := "checkpoint-" + checkpoint
		safe, err := utils.SanitizeSnapshotName(name)
		if err != nil {
			utils.Info(err)
			continue
		}
		container := utils.ContainerName(db.Driver, db.ServiceName)
		wasRunning, _ := utils.IsServiceRunning(container)
		if _, err := utils.RunSnapshot(utils.SnapshotRequest{
			Service: db.ServiceName, Driver: db.Driver,
			Stack: filepath.Base(utils.CorgiComposePathDir),
			Name:  safe, Force: true, WasRunning: wasRunning,
		}, time.Now()); err != nil {
			utils.Infof("%s: snapshot failed: %v\n", db.ServiceName, err)
			continue
		}
		out = append(out, checkpointDatabase{Service: db.ServiceName, Snapshot: safe})
	}
	return out
}

func runRestore(cmd *cobra.Command, args []string) {
	mustLoadCorgiServices(cmd)
	name := args[0]
	if !checkpointNameRe.MatchString(name) {
		exitWithError(utils.ErrUsage, fmt.Errorf("%q is not a checkpoint name", name), 2)
		return
	}

	file, err := readCheckpoint(name)
	if err != nil {
		exitWithError(utils.ErrConfig, err, 1)
		return
	}

	safety := "restore-" + utils.DefaultSnapshotName(time.Now())
	atRisk := reposAtRisk(file.Repos)
	if len(atRisk) > 0 {
		if !restoreConfirmed && !confirmRestore(atRisk, safety) {
			utils.Info("nothing restored")
			return
		}
		if err := saveSafetyCheckpoint(file.Repos, safety); err != nil {
			exitWithError(utils.ErrConfig,
				fmt.Errorf("could not save the safety checkpoint, so nothing was restored: %v", err), 1)
			return
		}
	}

	var failures int
	for _, repo := range file.Repos {
		if err := utils.RestoreWorkTree(repo.Path, repo.Branch, repo.Head, repo.StashSha); err != nil {
			utils.Infof("✖ %-16s %v\n", repo.Name, err)
			failures++
			continue
		}
		utils.Infof("✔ %-16s %s%s\n", repo.Name, shortHead(repo), capturedSuffix(repo))
	}
	if restoreWithDB {
		restoreCheckpointDatabases(file)
	}
	if failures > 0 {
		exitProcess(1)
	}
}

func dirtyRepos(repos []checkpointRepo) []string {
	var dirty []string
	for _, repo := range repos {
		if state, ok := utils.ReadRepoState(repo.Path); ok && state.Dirty {
			dirty = append(dirty, repo.Name)
		}
	}
	return dirty
}

func reposAtRisk(repos []checkpointRepo) []string {
	var at []string
	for _, repo := range repos {
		state, ok := utils.ReadRepoState(repo.Path)
		if !ok {
			continue
		}
		head, err := utils.RepoHead(repo.Path)
		switch {
		case state.Dirty:
			at = append(at, repo.Name+" (uncommitted work)")
		case err == nil && head != repo.Head:
			at = append(at, repo.Name+" (commits made since the checkpoint)")
		}
	}
	return at
}

func confirmRestore(atRisk []string, safety string) bool {
	utils.Info("restoring discards the current state of these repos:")
	for _, name := range atRisk {
		utils.Info("  " + name)
	}
	utils.Infof("tracked changes and current HEADs are saved as checkpoint %q first, then the trees are reset.\n", safety)
	if utils.NonInteractive {
		utils.Info("re-run with --yes to go ahead")
		return false
	}
	answer, err := utils.PickItemFromListPrompt("continue?", []string{"yes, restore"}, "no, stop")
	return err == nil && answer == "yes, restore"
}

func saveSafetyCheckpoint(repos []checkpointRepo, safety string) error {
	file := checkpointFile{Name: safety, CreatedAt: time.Now().UTC()}
	for _, repo := range repos {
		captured, ok := captureRepo(checkoutTarget{name: repo.Name, path: repo.Path}, safety)
		if !ok {
			if state, isRepo := utils.ReadRepoState(repo.Path); isRepo && state.Dirty {
				return fmt.Errorf("could not capture %s's uncommitted work", repo.Name)
			}
			continue
		}
		file.Repos = append(file.Repos, captured)
	}
	if err := writeCheckpoint(file); err != nil {
		return err
	}
	utils.Infof("saved current state as checkpoint %q\n", safety)
	return nil
}

func restoreCheckpointDatabases(file checkpointFile) {
	for _, db := range file.Databases {
		archive, meta, err := utils.SnapshotPaths(db.Service, db.Snapshot)
		if err != nil {
			utils.Infof("%s: %v\n", db.Service, err)
			continue
		}
		if err := utils.RunRestore(utils.RestoreRequest{
			Service: db.Service, Driver: "postgres",
			ArchivePath: archive, MetaPath: meta, Force: true,
		}); err != nil {
			utils.Infof("%s: restore failed: %v\n", db.Service, err)
			continue
		}
		utils.Infof("✔ %-16s db snapshot %s\n", db.Service, db.Snapshot)
	}
}

func runCheckpointList(cmd *cobra.Command, _ []string) {
	mustLoadCorgiServices(cmd)
	files, err := listCheckpoints()
	if err != nil || len(files) == 0 {
		if utils.JSONOutput {
			utils.PrintJSON([]checkpointFile{})
			return
		}
		utils.Info("no checkpoints yet")
		return
	}
	if utils.JSONOutput {
		utils.PrintJSON(files)
		return
	}
	for _, file := range files {
		utils.Infof("%-24s %s  %d repos%s\n",
			file.Name, file.CreatedAt.Local().Format("2006-01-02 15:04"), len(file.Repos), dbSuffix(file))
	}
}

func dbSuffix(file checkpointFile) string {
	if len(file.Databases) == 0 {
		return ""
	}
	return fmt.Sprintf(" + %d db", len(file.Databases))
}

func runCheckpointRemove(cmd *cobra.Command, args []string) {
	mustLoadCorgiServices(cmd)
	name := args[0]
	if !checkpointNameRe.MatchString(name) {
		exitWithError(utils.ErrUsage, fmt.Errorf("%q is not a checkpoint name", name), 2)
		return
	}
	file, err := readCheckpoint(name)
	if err != nil {
		exitWithError(utils.ErrConfig, err, 1)
		return
	}
	for _, repo := range file.Repos {
		utils.DropCheckpointRefs(repo.Path, name)
	}
	if err := os.Remove(checkpointPath(name)); err != nil {
		exitWithError(utils.ErrConfig, err, 1)
		return
	}
	utils.Infof("removed checkpoint %q\n", name)
}

func checkpointPath(name string) string {
	return filepath.Join(utils.CheckpointsDir(utils.CorgiComposePathDir), name+".json")
}

func writeCheckpoint(file checkpointFile) error {
	dir := utils.CheckpointsDir(utils.CorgiComposePathDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	utils.EnsureCorgiServicesIgnore(filepath.Dir(dir), ".checkpoints")
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(checkpointPath(file.Name), data, 0o644)
}

func readCheckpoint(name string) (checkpointFile, error) {
	var file checkpointFile
	if !checkpointNameRe.MatchString(name) {
		return file, fmt.Errorf("%q is not a checkpoint name", name)
	}
	data, err := os.ReadFile(checkpointPath(name))
	if err != nil {
		return file, fmt.Errorf("no checkpoint named %q (corgi checkpoint list)", name)
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return file, fmt.Errorf("checkpoint %q is unreadable: %v", name, err)
	}
	return file, nil
}

func listCheckpoints() ([]checkpointFile, error) {
	dir := utils.CheckpointsDir(utils.CorgiComposePathDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []checkpointFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		file, err := readCheckpoint(trimJSONExt(entry.Name()))
		if err != nil {
			continue
		}
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].CreatedAt.After(files[j].CreatedAt) })
	return files, nil
}

func trimJSONExt(name string) string {
	return name[:len(name)-len(filepath.Ext(name))]
}

func shortHead(repo checkpointRepo) string {
	head := repo.Head
	if len(head) > 8 {
		head = head[:8]
	}
	if repo.Branch != "" {
		return repo.Branch + " @ " + head
	}
	return head
}

func capturedSuffix(repo checkpointRepo) string {
	if repo.StashSha == "" {
		return ""
	}
	return " · uncommitted work captured"
}
