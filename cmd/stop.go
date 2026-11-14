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

var stopServices []string

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
stop a single service, or --services a,b for several, and leave the rest running.`,
	Run: runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
	stopCmd.Flags().StringVar(&stopService, "service", "", "Stop only this service (leave others running)")
	stopCmd.Flags().StringSliceVar(&stopServices, "services", nil, "Stop only these services, comma separated (leave others running)")
}

func stopNames() []string {
	var names []string
	for _, name := range append([]string{stopService}, stopServices...) {
		if name != "" && !containsName(names, name) {
			names = append(names, name)
		}
	}
	return names
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
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
	corgi := loadCorgiForStop(cmd)

	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	st, ok := readReconciledRunState(statePath)
	if !ok {
		emitStopSummary(stopSummary{Stopped: []string{}, Failed: []stopFailure{}})
		return
	}

	names := stopNames()
	if len(names) == 0 && !anythingRunning(st) && !hasContainerBackedEntries(st) {
		removeStateLocked(statePath)
		emitStopSummary(stopSummary{Stopped: []string{}, Failed: []stopFailure{}})
		return
	}

	var summary stopSummary
	if len(names) == 0 {
		summary = stopRunningServices(stopTargets(st, ""), true)
		stopEntireStack(corgi, st, statePath)
	} else {
		summary = stopSummary{Stopped: []string{}, Failed: []stopFailure{}}
		for i, name := range names {
			if i > 0 {
				st, _ = readReconciledRunState(statePath)
			}
			one := stopRunningServices(stopTargets(st, name), false)
			stopSingleService(corgi, st, statePath, name, &one)
			summary.Stopped = append(summary.Stopped, one.Stopped...)
			summary.Failed = append(summary.Failed, one.Failed...)
		}
	}

	emitStopSummary(summary)
	if len(summary.Failed) > 0 {
		exitProcess(1)
	}
}

func loadCorgiForStop(cmd *cobra.Command) *utils.CorgiCompose {
	corgi := mustLoadCorgiServices(cmd)
	if resolved, rerr := utils.ResolveRunnerModes(corgi.Services, false, false); rerr == nil {
		corgi.Services = resolved
	}
	return corgi
}

func readReconciledRunState(statePath string) (utils.RunState, bool) {
	if _, err := os.Stat(statePath); err != nil {
		return utils.RunState{}, false
	}
	st, err := utils.ReadRunState(statePath)
	if err != nil {
		return utils.RunState{}, false
	}
	st = utils.ReconcileRunState(st, utils.PidAlive, utils.ContainerRunning)
	st = probeDockerRunnerServices(st, utils.IsPortListening, time.Now().UTC())
	return st, true
}

func stopRunningServices(targets []utils.RunStateEntry, wholeStack bool) stopSummary {
	summary := stopSummary{Stopped: []string{}, Failed: []stopFailure{}}
	for _, t := range targets {
		if t.Kind != "service" {
			continue
		}
		if t.Status != "running" {
			continue
		}
		stopServiceProcess(t, wholeStack, &summary)
	}
	return summary
}

func stopServiceProcess(t utils.RunStateEntry, wholeStack bool, summary *stopSummary) {
	if t.PID == 0 {
		if wholeStack {
			summary.Stopped = append(summary.Stopped, t.Name)
		}
		return
	}
	if err := stopProcessGroup(t); err != nil {
		summary.Failed = append(summary.Failed, stopFailure{Name: t.Name, Error: err.Error()})
		return
	}
	summary.Stopped = append(summary.Stopped, t.Name)
}

func stopEntireStack(corgi *utils.CorgiCompose, st utils.RunState, statePath string) {
	cleanup(corgi)
	utils.StopDockerRunnerServices(containerBackedNotInCompose(st, corgi))
	if len(corgi.DatabaseServices) != 0 {
		utils.ExecuteForEachService("down")
	}
	removeStateLocked(statePath)
}

func stopSingleService(corgi *utils.CorgiCompose, st utils.RunState, statePath, name string, summary *stopSummary) {
	if entry, ok := findStateEntry(st.Services, name); ok && entry.PID == 0 {
		utils.StopDockerRunnerServices([]string{name})
		summary.Stopped = append(summary.Stopped, name)
	}
	stopK8sRunners(corgi, name)
	runServiceAfterStop(corgi, name)
	if unlock, lerr := utils.LockRunState(utils.CorgiComposePathDir); lerr == nil {
		defer unlock()
	}
	st.Services = removeStateEntry(st.Services, name)
	st.DBServices = removeStateEntry(st.DBServices, name)
	if err := utils.WriteRunState(statePath, st); err != nil {
		summary.Failed = append(summary.Failed, stopFailure{Name: name, Error: err.Error()})
	}
}

func containerBackedNotInCompose(st utils.RunState, corgi *utils.CorgiCompose) []string {
	known := map[string]bool{}
	for _, name := range utils.DockerRunnerServiceNames(corgi.Services) {
		known[name] = true
	}
	var names []string
	for _, e := range st.Services {
		if e.PID == 0 && !known[e.Name] {
			names = append(names, e.Name)
		}
	}
	return names
}

func hasContainerBackedEntries(st utils.RunState) bool {
	for _, e := range st.Services {
		if e.PID == 0 {
			return true
		}
	}
	return false
}

func findStateEntry(entries []utils.RunStateEntry, name string) (utils.RunStateEntry, bool) {
	for _, e := range entries {
		if e.Name == name {
			return e, true
		}
	}
	return utils.RunStateEntry{}, false
}

func removeStateLocked(statePath string) {
	unlock, _ := utils.LockRunState(utils.CorgiComposePathDir)
	if err := os.Rename(statePath, utils.RunStateLastPath(utils.CorgiComposePathDir)); err != nil {
		_ = os.Remove(statePath)
	}
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
