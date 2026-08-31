package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

func resolvePostgresService(name string, dbs []utils.DatabaseService) (utils.DatabaseService, error) {
	if name != "" {
		return findNamedPostgresService(name, dbs)
	}
	return pickSolePostgresService(dbs)
}

func findNamedPostgresService(name string, dbs []utils.DatabaseService) (utils.DatabaseService, error) {
	for _, d := range dbs {
		if d.ServiceName == name {
			if !utils.IsPostgresFamilyDriver(d.Driver) {
				return d, fmt.Errorf("db snapshot/restore: driver %q is not supported — postgres-family only (postgres, postgis, pgvector, timescaledb)", d.Driver)
			}
			return d, nil
		}
	}
	return utils.DatabaseService{}, fmt.Errorf("db_service %q not found", name)
}

func pickSolePostgresService(dbs []utils.DatabaseService) (utils.DatabaseService, error) {
	var family []utils.DatabaseService
	for _, d := range dbs {
		if utils.IsPostgresFamilyDriver(d.Driver) {
			family = append(family, d)
		}
	}
	switch len(family) {
	case 0:
		return utils.DatabaseService{}, fmt.Errorf("no postgres-family db_service in this stack")
	case 1:
		return family[0], nil
	default:
		names := make([]string, len(family))
		for i, d := range family {
			names[i] = d.ServiceName
		}
		return utils.DatabaseService{}, fmt.Errorf("multiple postgres-family dbs — name one: %s", strings.Join(names, ", "))
	}
}

var (
	snapList     bool
	snapRM       string
	snapForce    bool
	restoreYes   bool
	restoreForce bool
)

var dbSnapshotCmd = &cobra.Command{
	Use:   "snapshot [name] [service]",
	Short: "Physical snapshot of a Postgres-family db (--list / --rm to manage)",
	Run:   runDbSnapshot,
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore [name|path] [service]",
	Short: "Restore a Postgres-family db from a snapshot",
	Run:   runDbRestore,
}

func runDbSnapshot(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		utils.Info(err)
		exitProcess(1)
	}

	// --list / --rm manage existing snapshots; then the lone positional is the
	// service. Otherwise args are [name] [service] and we create.
	if snapList || snapRM != "" {
		manageSnapshots(args, corgi.DatabaseServices)
		return
	}
	createSnapshot(args, corgi.DatabaseServices)
}

func resolvePostgresServiceOrExit(serviceArg string, dbs []utils.DatabaseService) utils.DatabaseService {
	svc, err := resolvePostgresService(serviceArg, dbs)
	if err != nil {
		utils.Info(err)
		exitProcess(1)
	}
	return svc
}

func manageSnapshots(args []string, dbs []utils.DatabaseService) {
	serviceArg := ""
	if len(args) > 0 {
		serviceArg = args[0]
	}
	svc := resolvePostgresServiceOrExit(serviceArg, dbs)
	if snapRM != "" {
		removeSnapshot(svc.ServiceName, snapRM)
	} else {
		listSnapshots(svc.ServiceName)
	}
}

func createSnapshot(args []string, dbs []utils.DatabaseService) {
	name, serviceArg := "", ""
	if len(args) > 0 {
		name = args[0]
	}
	if len(args) > 1 {
		serviceArg = args[1]
	}

	svc := resolvePostgresServiceOrExit(serviceArg, dbs)

	if name == "" {
		name = utils.DefaultSnapshotName(time.Now())
	}
	name, err := utils.SanitizeSnapshotName(name)
	if err != nil {
		utils.Info(err)
		exitProcess(1)
	}

	if utils.IsStackSupervised(utils.CorgiComposePathDir) {
		utils.Info("a detached `corgi run` is managing this stack — run `corgi stop` first")
		exitProcess(1)
	}

	container := utils.ContainerName(svc.Driver, svc.ServiceName)
	wasRunning, _ := utils.IsServiceRunning(container)

	meta, err := utils.RunSnapshot(utils.SnapshotRequest{
		Service: svc.ServiceName, Driver: svc.Driver,
		Stack: filepath.Base(utils.CorgiComposePathDir),
		Name:  name, Force: snapForce, WasRunning: wasRunning,
	}, time.Now())
	if err != nil {
		utils.Info("snapshot failed:", err)
		exitProcess(1)
	}
	utils.Infof("📦 snapshot %q saved (%s, pg%s/%s, %d bytes)\n",
		name, meta.Image, meta.PgVersionMajor, meta.Arch, meta.SizeBytes)
}

func runDbRestore(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		utils.Info(err)
		exitProcess(1)
	}

	nameOrPath, serviceArg := "", ""
	if len(args) > 0 {
		nameOrPath = args[0]
	}
	if len(args) > 1 {
		serviceArg = args[1]
	}
	if nameOrPath == "" {
		utils.Info("usage: corgi db restore [name|path] [service]")
		exitProcess(1)
	}

	svc, err := resolvePostgresService(serviceArg, corgi.DatabaseServices)
	if err != nil {
		utils.Info(err)
		exitProcess(1)
	}

	if utils.IsStackSupervised(utils.CorgiComposePathDir) {
		utils.Info("a detached `corgi run` is managing this stack — run `corgi stop` first")
		exitProcess(1)
	}

	archive, metaPath, fromPath, err := resolveRestoreSource(svc.ServiceName, nameOrPath)
	if err != nil {
		utils.Info(err)
		exitProcess(1)
	}

	if !restoreYes {
		fmt.Printf("⚠️  This WIPES the current %q data volume and restores %s. Continue? [y/N] ",
			svc.ServiceName, filepath.Base(archive))
		var ans string
		_, _ = fmt.Scanln(&ans)
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			utils.Info("aborted")
			return
		}
	}

	if err := utils.RunRestore(utils.RestoreRequest{
		Service: svc.ServiceName, Driver: svc.Driver,
		ArchivePath: archive, MetaPath: metaPath,
		FromPath: fromPath, Force: restoreForce,
	}); err != nil {
		utils.Info("restore failed:", err)
		exitProcess(1)
	}
	utils.Infof("✅ restored %q from %s\n", svc.ServiceName, filepath.Base(archive))
}

func listSnapshots(service string) {
	items, err := utils.ListSnapshots(service)
	if err != nil {
		utils.Info(err)
		exitProcess(1)
	}
	if utils.JSONOutput {
		utils.PrintJSON(items)
		return
	}
	if len(items) == 0 {
		utils.Infof("no snapshots for %s\n", service)
		return
	}
	for _, it := range items {
		utils.Infof("%-20s pg%s/%s  %d bytes  %s\n", it.Name, it.PgVersionMajor, it.Arch, it.SizeBytes, it.CreatedAt)
	}
}

// resolveRestoreSource maps the [name|path] positional to (archive, meta,
// fromPath). A value with a path separator or a .tar.zst suffix is an explicit
// (untrusted) path; otherwise it is a named snapshot under the service dir.
func resolveRestoreSource(service, nameOrPath string) (archive, metaPath string, fromPath bool, err error) {
	fromPath = strings.ContainsAny(nameOrPath, `/\`) || strings.HasSuffix(nameOrPath, ".tar.zst")
	if fromPath {
		archive = nameOrPath
		metaPath = strings.TrimSuffix(archive, ".tar.zst") + ".meta.json"
		return archive, metaPath, true, nil
	}
	archive, metaPath, err = utils.SnapshotPaths(service, nameOrPath)
	return archive, metaPath, false, err
}

// snapshotRemovePaths sanitizes the name and resolves the pair to delete.
func snapshotRemovePaths(service, name string) (archive, metaPath string, err error) {
	name, err = utils.SanitizeSnapshotName(name)
	if err != nil {
		return "", "", err
	}
	return utils.SnapshotPaths(service, name)
}

func removeSnapshot(service, name string) {
	archive, metaPath, err := snapshotRemovePaths(service, name)
	if err != nil {
		utils.Info(err)
		exitProcess(1)
	}
	_ = os.Remove(archive)
	_ = os.Remove(metaPath)
	utils.Infof("🗑️  removed snapshot %q\n", name)
}
