package cmd

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var stopService string

var stopSummaryToStderr bool

type stopFailure struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

type stopSummary struct {
	Stopped []string      `json:"stopped"`
	Failed  []stopFailure `json:"failed"`
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop detached services and db_services started with corgi run --detach",
	Long: `Reads corgi_services/.state.json, terminates each detached service's
process group, runs afterStart hooks, and brings db_service containers down.

Idempotent: with no run-state or nothing running it exits 0. Use --service to
stop a single service and leave the rest running.`,
	Run: runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
	stopCmd.Flags().StringVar(&stopService, "service", "", "Stop only this service (leave others running)")
}

func stopTargets(st utils.RunState, service string) []utils.RunStateEntry {
	all := append(append([]utils.RunStateEntry{}, st.Services...), st.DBServices...)
	if service == "" {
		return all
	}
	for _, e := range all {
		if e.Name == service {
			return []utils.RunStateEntry{e}
		}
	}
	return nil
}

func emitStopSummary(s stopSummary) {
	if utils.JSONOutput {
		if stopSummaryToStderr {
			utils.PrintJSONTo(os.Stderr, s)
		} else {
			utils.PrintJSON(s)
		}
		return
	}
	if len(s.Stopped) == 0 && len(s.Failed) == 0 {
		utils.Info("nothing to stop")
		return
	}
	for _, name := range s.Stopped {
		utils.Info("stopped", name)
	}
	for _, f := range s.Failed {
		utils.Info("failed to stop", f.Name+":", f.Error)
	}
}

func runStop(cmd *cobra.Command, _ []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError(utils.ErrConfig, err.Error())
		} else {
			utils.Infof("couldn't get services config: %s\n", err)
		}
		os.Exit(1)
	}

	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if _, err := os.Stat(statePath); err != nil {
		emitStopSummary(stopSummary{Stopped: []string{}, Failed: []stopFailure{}})
		return
	}

	st, err := utils.ReadRunState(statePath)
	if err != nil {
		emitStopSummary(stopSummary{Stopped: []string{}, Failed: []stopFailure{}})
		return
	}
	st = utils.ReconcileRunState(st, utils.PidAlive, utils.ContainerRunning)

	if stopService == "" && !anythingRunning(st) {
		removeStateLocked(statePath)
		emitStopSummary(stopSummary{Stopped: []string{}, Failed: []stopFailure{}})
		return
	}

	targets := stopTargets(st, stopService)
	summary := stopSummary{Stopped: []string{}, Failed: []stopFailure{}}
	for _, t := range targets {
		if t.Kind != "service" {
			continue
		}
		if t.Status != "running" {
			continue
		}
		// pid==0 → docker-runner container; cleanup() brings it down, not a pgroup kill.
		if t.PID == 0 {
			continue
		}
		if err := stopProcessGroup(t); err != nil {
			summary.Failed = append(summary.Failed, stopFailure{Name: t.Name, Error: err.Error()})
			continue
		}
		summary.Stopped = append(summary.Stopped, t.Name)
	}

	if stopService == "" {
		cleanup(corgi)
		if len(corgi.DatabaseServices) != 0 {
			utils.ExecuteForEachService("down")
		}
		removeStateLocked(statePath)
	} else {
		runServiceAfterStop(corgi, stopService)
		if unlock, lerr := utils.LockRunState(utils.CorgiComposePathDir); lerr == nil {
			defer unlock()
		}
		st.Services = removeStateEntry(st.Services, stopService)
		st.DBServices = removeStateEntry(st.DBServices, stopService)
		if err := utils.WriteRunState(statePath, st); err != nil {
			summary.Failed = append(summary.Failed, stopFailure{Name: stopService, Error: err.Error()})
		}
	}

	emitStopSummary(summary)
	if len(summary.Failed) > 0 {
		os.Exit(1)
	}
}

// removeStateLocked deletes the run-state file under the advisory lock so it
// can't clobber a concurrent restart's read-modify-write.
func removeStateLocked(statePath string) {
	unlock, _ := utils.LockRunState(utils.CorgiComposePathDir)
	_ = os.Remove(statePath)
	if unlock != nil {
		unlock()
	}
}

func stopProcessGroup(e utils.RunStateEntry) error {
	pgid := e.PGID
	if pgid == 0 {
		pgid = e.PID
	}
	if pgid <= 0 {
		return fmt.Errorf("no pid recorded")
	}
	if err := utils.SignalProcessGroup(pgid, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !utils.PidAlive(e.PID, e.Command) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if utils.PidAlive(e.PID, e.Command) {
		return utils.SignalProcessGroup(pgid, syscall.SIGKILL)
	}
	return nil
}

func anythingRunning(st utils.RunState) bool {
	for _, e := range st.Services {
		if e.Status == "running" {
			return true
		}
	}
	for _, e := range st.DBServices {
		if e.Status == "running" {
			return true
		}
	}
	return false
}

func removeStateEntry(entries []utils.RunStateEntry, name string) []utils.RunStateEntry {
	out := entries[:0]
	for _, e := range entries {
		if e.Name != name {
			out = append(out, e)
		}
	}
	return out
}
