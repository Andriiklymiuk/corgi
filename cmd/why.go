package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

const (
	verdictHealthy       = "healthy"
	verdictCrashed       = "crashed"
	verdictNotStarted    = "not_started"
	verdictDependency    = "dependency_unready"
	verdictPortTaken     = "port_taken"
	verdictEnvMissing    = "env_missing"
	verdictUnhealthy     = "unhealthy"
	verdictNoStartScript = "no_start_command"
)

type whyDependency struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type whyPort struct {
	Number    int    `json:"number"`
	Listening bool   `json:"listening"`
	Owner     string `json:"owner,omitempty"`
	Ours      bool   `json:"ours"`
}

type whyEnv struct {
	SourceFileMissing bool     `json:"sourceFileMissing,omitempty"`
	Missing           []string `json:"missing,omitempty"`
	Unresolved        []string `json:"unresolved,omitempty"`
}

type whyReport struct {
	Service      string          `json:"service"`
	Verdict      string          `json:"verdict"`
	Detail       string          `json:"detail"`
	Status       string          `json:"status,omitempty"`
	Port         *whyPort        `json:"port,omitempty"`
	LastExitCode *int            `json:"lastExitCode,omitempty"`
	Dependencies []whyDependency `json:"dependencies,omitempty"`
	Env          *whyEnv         `json:"env,omitempty"`
	LogTail      []string        `json:"logTail,omitempty"`
	NextStep     string          `json:"nextStep,omitempty"`
}

var whyLogLines int

var whyCmd = &cobra.Command{
	Use:     "why <service>",
	Aliases: []string{"diagnose"},
	Short:   "Explain in one call why a service is not up",
	Long: `Runs the diagnosis by hand nobody wants to repeat: unmet dependencies, who owns
the port, the last exit code, missing or unresolved env, and the tail of the
service's own log.

Reports a single machine-readable verdict — healthy, crashed, not_started,
dependency_unready, port_taken, env_missing, no_start_command or unhealthy — so a
script or an agent can branch without reading prose.`,
	Example: `corgi why api

corgi why api --json`,
	Args: cobra.ExactArgs(1),
	Run:  runWhy,
}

func init() {
	rootCmd.AddCommand(whyCmd)
	whyCmd.Flags().IntVar(&whyLogLines, "log-lines", 8, "How many trailing log lines to include")
}

func runWhy(cmd *cobra.Command, args []string) {
	corgi := mustLoadCorgiServices(cmd)
	name := strings.TrimSpace(args[0])

	service, found := findServiceByName(corgi, name)
	if !found {
		exitWithError(utils.ErrServiceNotFound,
			fmt.Errorf("no service named %q in corgi-compose.yml", name), 1)
		return
	}

	report := diagnoseService(corgi, service)
	if utils.JSONOutput {
		utils.PrintJSON(report)
	} else {
		printWhyReport(report)
	}
	if report.Verdict != verdictHealthy {
		exitProcess(1)
	}
}

func findServiceByName(corgi *utils.CorgiCompose, name string) (utils.Service, bool) {
	for _, svc := range corgi.Services {
		if svc.ServiceName == name {
			return svc, true
		}
	}
	return utils.Service{}, false
}

func diagnoseService(corgi *utils.CorgiCompose, service utils.Service) whyReport {
	report := whyReport{Service: service.ServiceName}

	statuses, _ := contextStatuses(corgi)
	report.Status = statuses[service.ServiceName]
	report.Dependencies = unreadyDependencies(corgi, service, statuses)
	report.Port = probeServicePort(service, report.Status)
	report.Env = checkServiceEnv(corgi, service)
	report.LastExitCode = lastExitCodeFor(service.ServiceName)
	report.LogTail = tailServiceLog(service.ServiceName, whyLogLines)

	report.Verdict, report.Detail, report.NextStep = whyVerdict(service, report)
	return report
}

func whyVerdict(service utils.Service, report whyReport) (verdict, detail, next string) {
	switch {
	case report.Status == "running" && (report.Port == nil || report.Port.Listening):
		return verdictHealthy, "the service is running and its port answers", ""

	case report.Status == "crashed":
		return verdictCrashed,
			fmt.Sprintf("%s started and then exited%s", service.ServiceName, exitCodeSuffix(report.LastExitCode)),
			"read the log tail below, then corgi restart --service " + service.ServiceName

	case len(report.Dependencies) > 0:
		dep := report.Dependencies[0]
		return verdictDependency,
			fmt.Sprintf("depends on %s %s, which is %s", dep.Kind, dep.Name, dep.Status),
			"start the dependency first: corgi run --services " + dep.Name

	case report.Port != nil && report.Port.Listening && !report.Port.Ours:
		return verdictPortTaken,
			fmt.Sprintf("port %d is already held by %s", report.Port.Number, report.Port.Owner),
			"stop that process, or change the service's port in corgi-compose.yml"

	case report.Env != nil && report.Env.SourceFileMissing:
		return verdictEnvMissing,
			"the env file this service reads is not there, so it would run on the example's placeholders",
			"corgi env check, then create the env file"

	case report.Env != nil && len(report.Env.Missing) > 0:
		return verdictEnvMissing,
			fmt.Sprintf("env is missing %s", strings.Join(report.Env.Missing, ", ")),
			"add the keys to the service env file, or declare them in corgi-compose.yml"

	case report.Env != nil && len(report.Env.Unresolved) > 0:
		return verdictEnvMissing,
			fmt.Sprintf("env still holds placeholder values for %s", strings.Join(report.Env.Unresolved, ", ")),
			"fill in the real values before starting"

	case len(service.Start) == 0 && service.Runner.Name == "":
		return verdictNoStartScript,
			"the service declares no start command and no runner, so corgi has nothing to launch",
			"add a start: block, or a runner"

	case report.Status == "":
		return verdictNotStarted,
			"corgi has no run state for this service — it was never started, or the stack was stopped",
			"corgi run --services " + service.ServiceName

	case report.Status == "running":
		return verdictUnhealthy,
			fmt.Sprintf("the process is alive but nothing is listening on port %d yet", report.Port.Number),
			"check the log tail; the service may still be compiling"
	}
	return verdictUnhealthy, "the service is not running and no single cause stands out", "corgi logs --service " + service.ServiceName
}

func exitCodeSuffix(code *int) string {
	if code == nil {
		return ""
	}
	return fmt.Sprintf(" with code %d", *code)
}

func unreadyDependencies(corgi *utils.CorgiCompose, service utils.Service, statuses map[string]string) []whyDependency {
	var out []whyDependency
	for _, dep := range service.DependsOnDb {
		status := statuses[dep.Name]
		if status == "running" {
			continue
		}
		out = append(out, whyDependency{Name: dep.Name, Kind: "db_service", Status: orUnknown(status)})
	}
	for _, dep := range service.DependsOnServices {
		status := statuses[dep.Name]
		if status == "running" {
			continue
		}
		out = append(out, whyDependency{Name: dep.Name, Kind: "service", Status: orUnknown(status)})
	}
	return out
}

func orUnknown(status string) string {
	if status == "" {
		return "not started"
	}
	return status
}

func probeServicePort(service utils.Service, status string) *whyPort {
	if service.Port == 0 {
		return nil
	}
	port := &whyPort{Number: service.Port, Listening: utils.IsPortListening(service.Port)}
	if !port.Listening {
		return port
	}
	port.Owner = utils.PortOwner(service.Port)
	port.Ours = status == "running"
	if port.Owner == "" {
		port.Owner = "an unknown process"
	}
	return port
}

func checkServiceEnv(corgi *utils.CorgiCompose, service utils.Service) *whyEnv {
	if service.IgnoreEnv {
		return nil
	}
	env := &whyEnv{}
	rows, err := utils.EnvCheckAll(corgi, "")
	if err == nil {
		for _, row := range rows {
			if row.Service != service.ServiceName {
				continue
			}
			env.SourceFileMissing = row.SourceAbsent
			env.Missing = row.Missing
		}
	}
	env.Unresolved = unresolvedPlaceholders(corgi, service)
	if !env.SourceFileMissing && len(env.Missing) == 0 && len(env.Unresolved) == 0 {
		return nil
	}
	return env
}

func unresolvedPlaceholders(corgi *utils.CorgiCompose, service utils.Service) []string {
	if len(service.EnvPlaceholdersToCheck) == 0 {
		return nil
	}
	resolved, err := utils.ResolveServiceEnv(service, corgi)
	if err != nil {
		return nil
	}
	values := map[string]string{}
	for _, entry := range resolved {
		values[entry.Key] = entry.Value
	}
	var out []string
	for _, key := range service.EnvPlaceholdersToCheck {
		value, present := values[key]
		if !present || strings.TrimSpace(value) == "" {
			out = append(out, key)
		}
	}
	return out
}

func lastExitCodeFor(name string) *int {
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	state, err := utils.ReadRunState(statePath)
	if err != nil {
		return nil
	}
	for _, entry := range append(append([]utils.RunStateEntry{}, state.Services...), state.DBServices...) {
		if entry.Name == name {
			return entry.ExitCode
		}
	}
	return nil
}

func tailServiceLog(name string, count int) []string {
	if count <= 0 {
		return nil
	}
	runs, err := utils.ListServiceRuns(logsBase(), name)
	if err != nil || len(runs) == 0 {
		return nil
	}
	file, err := os.Open(runs[0])
	if err != nil {
		return nil
	}
	defer file.Close()

	ring := make([]string, 0, count)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(ring) == count {
			ring = ring[1:]
		}
		ring = append(ring, stripLogTimestamp(line))
	}
	return ring
}

func stripLogTimestamp(line string) string {
	if len(line) < utils.LogTimestampLen {
		return line
	}
	head := line[:utils.LogTimestampLen-1]
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(head)); err != nil {
		return line
	}
	return line[utils.LogTimestampLen:]
}

func printWhyReport(report whyReport) {
	utils.Infof("%s → %s\n", report.Service, report.Verdict)
	utils.Info("  " + report.Detail)
	if report.Port != nil {
		utils.Infof("  port %d: %s\n", report.Port.Number, portStateLine(report.Port))
	}
	for _, dep := range report.Dependencies {
		utils.Infof("  waiting on %s %s (%s)\n", dep.Kind, dep.Name, dep.Status)
	}
	if report.Env != nil {
		if report.Env.SourceFileMissing {
			utils.Info("  env: the service's env file is missing")
		}
		if len(report.Env.Missing) > 0 {
			utils.Info("  env missing: " + strings.Join(report.Env.Missing, ", "))
		}
		if len(report.Env.Unresolved) > 0 {
			utils.Info("  env unresolved: " + strings.Join(report.Env.Unresolved, ", "))
		}
	}
	if len(report.LogTail) > 0 {
		utils.Info("")
		utils.Info("  last log lines:")
		for _, line := range report.LogTail {
			utils.Info("    " + line)
		}
	}
	if report.NextStep != "" {
		utils.Info("")
		utils.Info("  next: " + report.NextStep)
	}
}

func portStateLine(port *whyPort) string {
	if !port.Listening {
		return "nothing listening"
	}
	if port.Ours {
		return "listening (this service)"
	}
	return "listening, held by " + port.Owner
}
