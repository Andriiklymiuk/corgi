package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:   "exec <service> -- <cmd> [args...]",
	Short: "Run a one-off command inside a service's resolved environment",
	Long: `Run a one-off command in a service's working directory with the same env
corgi uses for that service's start commands (its .env is sourced via the same
mechanism, honoring autoSourceEnv). stdout/stderr stream through and the child's
exit code becomes corgi's exit code.

Examples:
  corgi exec api -- npm run migrate
  corgi exec api --json -- pytest -q
  corgi exec api --ensure-deps -- npm run migrate`,
	Run:                runExec,
	DisableFlagParsing: false,
}

func init() {
	rootCmd.AddCommand(execCmd)
	execCmd.Flags().Bool(
		"ensure-deps",
		false,
		"Wait for the service's depends_on_db and depends_on_services to be ready before running.",
	)
	execCmd.Flags().Duration(
		"ready-timeout",
		60*time.Second,
		"Max time to wait for dependencies when --ensure-deps is set.",
	)
}

// splitExecArgs separates the service name from the command tokens. dash is
// cmd.ArgsLenAtDash(): the index of the first token after `--`, or -1 when no
// `--` was given (then the first arg is the service, the rest the command).
func splitExecArgs(args []string, dash int) (service string, cmdTokens []string) {
	if dash >= 0 {
		service = strings.Join(args[:dash], " ")
		cmdTokens = args[dash:]
	} else if len(args) > 0 {
		service = args[0]
		cmdTokens = args[1:]
	}
	return strings.TrimSpace(service), cmdTokens
}

// shellJoin single-quotes each token so the runner's `/bin/sh -c` sees the
// original argument boundaries. A literal single quote is escaped as '\”.
func shellJoin(tokens []string) string {
	quoted := make([]string, len(tokens))
	for i, tok := range tokens {
		quoted[i] = "'" + strings.ReplaceAll(tok, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

func runExec(cmd *cobra.Command, args []string) {
	serviceName, cmdTokens := splitExecArgs(args, cmd.ArgsLenAtDash())

	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError("config", err.Error())
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}

	if serviceName == "" {
		// No service named: under non-interactive there's nobody to prompt, so
		// fail with the valid-service list rather than hang.
		utils.JSONError(utils.ErrServiceNotFound,
			fmt.Sprintf("no service given; valid services: %s", strings.Join(serviceNames(corgi), ", ")))
		os.Exit(2)
	}

	if len(cmdTokens) == 0 {
		msg := "no command given; usage: corgi exec <service> -- <cmd> [args...]"
		if utils.JSONOutput {
			utils.JSONError("E_USAGE", msg)
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(2)
	}

	ensureDeps, _ := cmd.Flags().GetBool("ensure-deps")
	readyTo := 60 * time.Second
	if d, err := cmd.Flags().GetDuration("ready-timeout"); err == nil && d > 0 {
		readyTo = d
	}

	code, err := execService(corgi, serviceName, cmdTokens, ensureDeps, readyTo)
	if err != nil {
		// execService only returns an error for resolution/spawn/readiness
		// failures; it has already emitted the right message/JSON. Exit code
		// is carried separately.
		os.Exit(code)
	}
	os.Exit(code)
}

func serviceNames(corgi *utils.CorgiCompose) []string {
	names := make([]string, len(corgi.Services))
	for i, s := range corgi.Services {
		names[i] = s.ServiceName
	}
	return names
}

// execService is the testable core: resolve the named service, optionally gate
// on dependency readiness, then run the command in the service's working dir
// with its env. Returns the exit code corgi should propagate plus an error for
// the failure cases (unknown service, readiness timeout, spawn failure). It
// emits human/JSON output itself so the cobra wrapper just maps to os.Exit.
func execService(
	corgi *utils.CorgiCompose,
	serviceName string,
	cmdTokens []string,
	ensureDeps bool,
	readyTimeout time.Duration,
) (int, error) {
	var service *utils.Service
	for i := range corgi.Services {
		if corgi.Services[i].ServiceName == serviceName {
			service = &corgi.Services[i]
			break
		}
	}
	if service == nil {
		msg := fmt.Sprintf("service %q not found; valid services: %s",
			serviceName, strings.Join(serviceNames(corgi), ", "))
		utils.JSONError(utils.ErrServiceNotFound, msg)
		return 2, fmt.Errorf("%s", msg)
	}

	if ensureDeps {
		if err := ensureServiceDeps(corgi, *service, readyTimeout); err != nil {
			if utils.JSONOutput {
				utils.JSONError(utils.ErrReadinessTimeout, err.Error())
			} else {
				fmt.Fprintln(os.Stderr, err)
			}
			return 1, err
		}
	}

	// The runner wraps the command in `/bin/sh -c`, so shell-quote each token
	// to preserve argument boundaries (e.g. `sh -c 'exit 7'` stays one arg).
	command := shellJoin(cmdTokens)
	interactive := utils.StdinIsTTY()

	// Keep stdout pure JSON in --json mode by routing child output to stderr;
	// otherwise stream straight through to the user's terminal.
	childOut := os.Stdout
	if utils.JSONOutput {
		childOut = os.Stderr
	}

	start := time.Now()
	code, err := utils.RunServiceCommandExitCode(
		command,
		service.AbsolutePath,
		interactive,
		childOut,
		os.Stderr,
		getServiceEnv(*service),
	)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		// Spawn failure (command not found, bad cwd): exit 1.
		msg := fmt.Sprintf("failed to run command for %s: %v", serviceName, err)
		if utils.JSONOutput {
			utils.JSONError("E_EXEC_FAILED", msg)
		} else {
			fmt.Fprintln(os.Stderr, msg)
		}
		return 1, err
	}

	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{
			"service":    serviceName,
			"exitCode":   code,
			"durationMs": durationMs,
		})
	}
	return code, nil
}

// ensureServiceDeps blocks until the service's depends_on_db and
// depends_on_services targets are reachable, bounded by readyTimeout. Returns
// an error on the first dependency that times out.
func ensureServiceDeps(corgi *utils.CorgiCompose, service utils.Service, readyTimeout time.Duration) error {
	for _, dep := range service.DependsOnDb {
		db, err := utils.GetDbServiceByName(dep.Name, corgi.DatabaseServices)
		if err != nil {
			continue // unknown dep — corgi validate flags these
		}
		ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
		err = utils.WaitForDBReady(ctx, db)
		cancel()
		if err != nil {
			return err
		}
	}
	for _, dep := range service.DependsOnServices {
		producer := findService(corgi, dep.Name)
		if producer == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), readyTimeout)
		err := utils.WaitForServiceReady(ctx, *producer)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func findService(corgi *utils.CorgiCompose, name string) *utils.Service {
	for i := range corgi.Services {
		if corgi.Services[i].ServiceName == name {
			return &corgi.Services[i]
		}
	}
	return nil
}
