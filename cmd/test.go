package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Run each service's `test` script in its resolved environment",
	Long: `Run the test script configured for each selected service, in that service's
working directory with the same env corgi uses for its start commands. corgi
test does NOT start databases or services — that is corgi run's job; with
--ensure-deps it only WAITS for already-starting dependencies to be ready.

A service runs if it has a script named "test" in its scripts. Services without
one are skipped (not failed). Multi-command test scripts run sequentially and
stop on the first non-zero exit.

Examples:
  corgi test
  corgi test --service api
  corgi test --profile backend --json
  corgi test --ensure-deps`,
	Run: runTestCmd,
}

func init() {
	rootCmd.AddCommand(testCmd)
	testCmd.Flags().String("service", "", "Only run the test script for this service.")
	testCmd.Flags().String("profile", "", "Narrow to services in this profile before selecting test scripts.")
	testCmd.Flags().Bool(
		"ensure-deps",
		false,
		"Wait for each service's depends_on_db and depends_on_services to be ready before testing.",
	)
	testCmd.Flags().Duration(
		"ready-timeout",
		defaultReadyTimeout,
		"Max time to wait for dependencies when --ensure-deps is set.",
	)
}

// testResult is one service's outcome. Skipped wins over the run fields:
// a skipped service has no exit code/duration and never counts as a failure.
type testResult struct {
	Name       string `json:"name"`
	ExitCode   int    `json:"exitCode,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	Passed     bool   `json:"passed,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	Message    string `json:"message,omitempty"`
}

// selection holds the resolved set of services to consider for testing.
type selection struct {
	services []utils.Service
}

func runTestCmd(cmd *cobra.Command, args []string) {
	corgi, err := utils.GetCorgiServices(cmd)
	if err != nil {
		if utils.JSONOutput {
			utils.JSONError("config", err.Error())
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}

	serviceName, _ := cmd.Flags().GetString("service")
	profile, _ := cmd.Flags().GetString("profile")
	ensureDeps, _ := cmd.Flags().GetBool("ensure-deps")
	readyTimeout := defaultReadyTimeout
	if d, err := cmd.Flags().GetDuration("ready-timeout"); err == nil && d > 0 {
		readyTimeout = d
	}

	sel, err := resolveSelection(corgi, serviceName, profile)
	if err != nil {
		// Unknown --service: list valid services and exit 2.
		if utils.JSONOutput {
			utils.JSONError(utils.ErrServiceNotFound, err.Error())
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(2)
	}

	results, allPassed := runTests(corgi, sel, ensureDeps, readyTimeout)
	reportTestResults(results, allPassed)

	if !allPassed {
		os.Exit(1)
	}
}

// resolveSelection narrows corgi.Services by --service / --profile. An unknown
// --service is an error (exit 2 upstream). An unknown --profile yields an empty
// selection (warn + run nothing), matching run's behavior.
func resolveSelection(corgi *utils.CorgiCompose, serviceName, profile string) (selection, error) {
	if serviceName != "" {
		for _, s := range corgi.Services {
			if s.ServiceName == serviceName {
				return selection{services: []utils.Service{s}}, nil
			}
		}
		return selection{}, fmt.Errorf("service %q not found; valid services: %s",
			serviceName, strings.Join(serviceNames(corgi), ", "))
	}

	if profile != "" {
		picked, _ := utils.SelectByProfile(corgi, profile)
		if len(picked) == 0 {
			utils.Infof("profile %q matched no services; nothing to test\n", profile)
			return selection{}, nil
		}
		var svcs []utils.Service
		for _, s := range corgi.Services {
			if picked[s.ServiceName] {
				svcs = append(svcs, s)
			}
		}
		return selection{services: svcs}, nil
	}

	return selection{services: append([]utils.Service(nil), corgi.Services...)}, nil
}

// findTestScript returns the service's "test" script commands, or false when it
// has none (caller skips the service).
func findTestScript(service utils.Service) ([]string, bool) {
	for _, s := range service.Scripts {
		if s.Name == "test" {
			return s.Commands, true
		}
	}
	return nil, false
}

// runTests is the testable core: for each selected service, optionally gate on
// dependency readiness, then run its test script (sequential, stop on first
// failure). Services without a test script are recorded skipped. Returns the
// per-service results and whether every run test passed (skips don't count).
func runTests(corgi *utils.CorgiCompose, sel selection, ensureDeps bool, readyTimeout time.Duration) (results []testResult, allPassed bool) {
	results = []testResult{}
	allPassed = true

	// Keep stdout pure JSON in --json mode by routing child output to stderr.
	childOut := os.Stdout
	if utils.JSONOutput {
		childOut = os.Stderr
	}
	interactive := utils.StdinIsTTY()

	for _, service := range sel.services {
		commands, ok := findTestScript(service)
		if !ok {
			results = append(results, testResult{Name: service.ServiceName, Skipped: true})
			continue
		}

		if ensureDeps {
			if err := ensureServiceDeps(corgi, service, readyTimeout); err != nil {
				allPassed = false
				results = append(results, testResult{
					Name:     service.ServiceName,
					Passed:   false,
					ExitCode: 1,
					Message:  err.Error(),
				})
				continue
			}
		}

		res := runServiceTest(service, commands, interactive, childOut)
		if !res.Passed {
			allPassed = false
		}
		results = append(results, res)
	}

	return results, allPassed
}

// runServiceTest runs a service's test commands sequentially in its env,
// stopping on the first non-zero exit. The captured exit code is that of the
// failing command (or the last command when all succeed).
func runServiceTest(service utils.Service, commands []string, interactive bool, childOut *os.File) testResult {
	env := getServiceEnv(service)
	start := time.Now()

	exitCode := 0
	for _, command := range commands {
		code, err := utils.RunServiceCommandExitCode(
			command,
			service.AbsolutePath,
			interactive,
			childOut,
			os.Stderr,
			env,
		)
		if err != nil {
			// Spawn failure (command not found, bad cwd): treat as a failed test.
			return testResult{
				Name:       service.ServiceName,
				Passed:     false,
				ExitCode:   1,
				DurationMs: time.Since(start).Milliseconds(),
				Message:    fmt.Sprintf("failed to run test command: %v", err),
			}
		}
		exitCode = code
		if code != 0 {
			break // stop on first failing command within the service
		}
	}

	return testResult{
		Name:       service.ServiceName,
		Passed:     exitCode == 0,
		ExitCode:   exitCode,
		DurationMs: time.Since(start).Milliseconds(),
	}
}

// reportTestResults emits the JSON payload (pure stdout) or the human per-service
// lines + summary (to stdout in human mode, stderr in JSON mode via utils.Info).
func reportTestResults(results []testResult, allPassed bool) {
	if utils.JSONOutput {
		utils.PrintJSON(map[string]any{
			"services": results,
			"passed":   allPassed,
		})
		return
	}

	var passed, failed, skipped int
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
			utils.Infof("– %s (no test script)\n", r.Name)
		case r.Passed:
			passed++
			utils.Infof("✓ %s (%dms)\n", r.Name, r.DurationMs)
		default:
			failed++
			if r.Message != "" {
				utils.Infof("✗ %s: %s\n", r.Name, r.Message)
			} else {
				utils.Infof("✗ %s (exit %d)\n", r.Name, r.ExitCode)
			}
		}
	}
	utils.Infof("\n%d passed, %d failed, %d skipped\n", passed, failed, skipped)
}
