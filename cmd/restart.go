package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var restartService string

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Stop then start detached services (corgi stop + corgi run --detach)",
	Long: `Stops the currently detached stack (or a single --service) and brings it
back up detached. Convenience for long-lived envs.

Non-interactive safe. With --json the stdout is a single startup-summary
JSON object (the same shape as corgi run --detach --json).`,
	Run: runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
	restartCmd.Flags().StringVar(&restartService, "service", "", "Restart only this service (leave others running)")
	restartCmd.Flags().Bool("detach", true, "Start services detached (always on for restart)")
	restartCmd.Flags().Bool("force", true, "Ignore stale run-state and start anyway")
	restartCmd.Flags().String("host", "", `IP to use instead of localhost in service URL env vars. Pass an explicit IP or "auto"/"ip" to detect the first non-loopback IPv4.`)
}

func runRestart(cmd *cobra.Command, args []string) {
	if restartService != "" {
		restartSingleService(cmd)
		return
	}

	prevStopService := stopService
	prevToStderr := stopSummaryToStderr
	stopService = restartService
	stopSummaryToStderr = true
	runStop(cmd, args)
	stopService = prevStopService
	stopSummaryToStderr = prevToStderr

	cmd.Flags().Set("detach", "true")
	runRun(cmd, args)
}

// findRestartEntry returns the run-state entry for a service, or an error if
// it was never started in the current detached run.
func findRestartEntry(st utils.RunState, service string) (utils.RunStateEntry, error) {
	for _, e := range st.Services {
		if e.Name == service {
			return e, nil
		}
	}
	return utils.RunStateEntry{}, fmt.Errorf(
		"service %q is not in the current detached run; start it with corgi run --detach first", service)
}

// updateServiceEntry replaces the named service's run-state entry with a fresh
// running entry, leaving every other entry untouched.
func updateServiceEntry(st utils.RunState, name string, pid int, command string, port int) utils.RunState {
	now := time.Now().UTC()
	for i := range st.Services {
		if st.Services[i].Name == name {
			st.Services[i].PID = pid
			st.Services[i].PGID = pid
			st.Services[i].Command = command
			st.Services[i].Port = port
			st.Services[i].Status = "running"
			st.Services[i].StartedAt = now
			st.Services[i].StatusChangedAt = now
			st.Services[i].ExitCode = nil
		}
	}
	return st
}

func emitRestartError(code, msg string) {
	if utils.JSONOutput {
		utils.JSONError(code, msg)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
}

// resolveRestartTarget validates that a single-service restart can proceed:
// the project has a detached run, the service is in that run-state, and it is
// declared in the compose. Returns a stable error code with any error so the
// caller can branch without re-classifying. Side-effect-free beyond reading
// the run-state file, so it is unit-testable.
func resolveRestartTarget(statePath string, corgi *utils.CorgiCompose, service string) (utils.RunState, utils.RunStateEntry, *utils.Service, string, error) {
	st, err := utils.ReadRunState(statePath)
	if err != nil {
		return st, utils.RunStateEntry{}, nil, utils.ErrNotRunning, fmt.Errorf("no detached run found for this project")
	}
	entry, err := findRestartEntry(st, service)
	if err != nil {
		return st, entry, nil, utils.ErrNotRunning, err
	}
	svc := findService(corgi, service)
	if svc == nil {
		return st, entry, nil, utils.ErrServiceNotFound, fmt.Errorf("service not declared in corgi-compose.yml")
	}
	return st, entry, svc, "", nil
}

// restartSingleService restarts one service of a detached run, leaving the rest
// untouched. It refuses to start a service that was never in the run-state.
func restartSingleService(cmd *cobra.Command) {
	if herr := resolveHostFlag(cmd); herr != nil {
		emitRestartError(utils.ErrConfig, herr.Error())
		os.Exit(1)
	}

	corgi, cerr := utils.GetCorgiServices(cmd)
	if cerr != nil {
		emitRestartError(utils.ErrConfig, cerr.Error())
		os.Exit(1)
	}

	if unlock, lerr := utils.LockRunState(utils.CorgiComposePathDir); lerr == nil {
		defer unlock()
	}

	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	st, entry, svc, code, err := resolveRestartTarget(statePath, corgi, restartService)
	if err != nil {
		emitRestartError(code, err.Error())
		os.Exit(1)
	}

	// bakes --host override + cross-service exports into the .env
	if eerr := utils.GenerateEnvForServices(corgi); eerr != nil {
		emitRestartError(utils.ErrConfig, eerr.Error())
		os.Exit(1)
	}

	_ = stopProcessGroup(entry)
	runServiceAfterStop(corgi, restartService) // teardown stragglers before relaunch
	runDetachedBeforeStart(*svc)

	pid, command, serr := relaunchDetachedService(*svc)
	if serr != nil {
		emitRestartError(utils.ErrExecFailed, serr.Error())
		os.Exit(1)
	}

	updated := updateServiceEntry(st, restartService, pid, command, svc.Port)
	_ = utils.WriteRunState(statePath, updated)

	if utils.JSONOutput {
		utils.PrintJSON(updated)
	} else {
		utils.Infof("🔁 restarted %s (pid %d)\n", restartService, pid)
	}
}

// relaunchDetachedService mirrors the runner branching in spawnDetachedServices.
func relaunchDetachedService(svc utils.Service) (pid int, command string, err error) {
	if svc.Runner.Name == "docker" && svc.Port != 0 {
		if uerr := utils.ExecuteServiceCommandRun(svc.ServiceName, "make", "up"); uerr != nil {
			return 0, "make up", uerr
		}
		return 0, "make up", nil
	}
	command = strings.Join(svc.Start, " && ")
	proc, serr := utils.StartDetached(svc.ServiceName, command, svc.AbsolutePath, getServiceEnv(svc))
	if serr != nil {
		return 0, command, serr
	}
	return proc.Pid, command, nil
}
