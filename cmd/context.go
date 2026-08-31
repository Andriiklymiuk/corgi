package cmd

import (
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

type contextRepo struct {
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Dirty    bool   `json:"dirty"`
	Head     string `json:"head,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Behind   int    `json:"behind,omitempty"`
}

type contextEntry struct {
	Name   string       `json:"name"`
	Kind   string       `json:"kind"`
	Port   int          `json:"port,omitempty"`
	Status string       `json:"status"`
	URL    string       `json:"url,omitempty"`
	Repo   *contextRepo `json:"repo,omitempty"`
}

type contextReport struct {
	Workspace   string                  `json:"workspace,omitempty"`
	ComposePath string                  `json:"composePath"`
	Tier        string                  `json:"tier,omitempty"`
	Profiles    []string                `json:"profiles,omitempty"`
	Detached    bool                    `json:"detached"`
	Services    []contextEntry          `json:"services"`
	Databases   []contextEntry          `json:"databases"`
	Errors      []utils.ValidationIssue `json:"errors,omitempty"`
	Warnings    []utils.ValidationIssue `json:"warnings,omitempty"`
}

var contextNoGit bool

var contextCmd = &cobra.Command{
	Use:     "context",
	Aliases: []string{"ctx"},
	Short:   "One-call snapshot of the workspace: topology, health, repo state",
	Long: `Answers "where am I" in a single call: every service and db_service with its
port and current status, each repo's branch, uncommitted work and ahead/behind
counts, the active env tier, the declared profiles, and any validation findings.

Built for agents — the same picture that otherwise takes corgi ps, corgi status,
corgi validate and a git call per repo.`,
	Example: `corgi context --json

corgi context --no-git`,
	Run: runContext,
}

func init() {
	rootCmd.AddCommand(contextCmd)
	contextCmd.Flags().BoolVar(&contextNoGit, "no-git", false, "Skip per-repo git state (faster on a big workspace)")
}

func runContext(cmd *cobra.Command, _ []string) {
	corgi := mustLoadCorgiServices(cmd)
	report := buildContextReport(corgi)

	if utils.JSONOutput {
		utils.PrintJSON(report)
		return
	}
	printContextReport(report)
}

func buildContextReport(corgi *utils.CorgiCompose) contextReport {
	errs, warns := utils.ValidateCompose(corgi)
	report := contextReport{
		Workspace:   corgi.Name,
		ComposePath: utils.CorgiComposePathDir,
		Tier:        utils.ActiveTierName,
		Profiles:    declaredProfiles(corgi),
		Errors:      errs,
		Warnings:    warns,
		Services:    []contextEntry{},
		Databases:   []contextEntry{},
	}

	statuses, detached := contextStatuses(corgi)
	report.Detached = detached

	for _, db := range corgi.DatabaseServices {
		report.Databases = append(report.Databases, contextEntry{
			Name:   db.ServiceName,
			Kind:   "db_service",
			Port:   db.Port,
			Status: statusOr(statuses, db.ServiceName, db.Port),
			URL:    portURL(db.Port),
		})
	}
	for _, svc := range corgi.Services {
		entry := contextEntry{
			Name:   svc.ServiceName,
			Kind:   "service",
			Port:   svc.Port,
			Status: statusOr(statuses, svc.ServiceName, svc.Port),
			URL:    portURL(svc.Port),
		}
		if !contextNoGit {
			entry.Repo = readContextRepo(svc.AbsolutePath)
		}
		report.Services = append(report.Services, entry)
	}
	sort.SliceStable(report.Services, func(i, j int) bool { return report.Services[i].Name < report.Services[j].Name })
	sort.SliceStable(report.Databases, func(i, j int) bool { return report.Databases[i].Name < report.Databases[j].Name })
	return report
}

func readContextRepo(path string) *contextRepo {
	state, ok := utils.ReadRepoState(path)
	if !ok {
		return nil
	}
	return &contextRepo{
		Path:     state.Path,
		Branch:   state.Branch,
		Dirty:    state.Dirty,
		Head:     state.Head,
		Upstream: state.Upstream,
		Ahead:    state.Ahead,
		Behind:   state.Behind,
	}
}

func contextStatuses(corgi *utils.CorgiCompose) (map[string]string, bool) {
	statuses := map[string]string{}
	statePath := utils.RunStatePath(utils.CorgiComposePathDir)
	if _, err := os.Stat(statePath); err != nil {
		return statuses, false
	}
	state, err := utils.ReadRunState(statePath)
	if err != nil {
		return statuses, false
	}
	reconciled := utils.ReconcileRunState(state, utils.PidAlive, utils.ContainerRunning)
	for _, entry := range append(append([]utils.RunStateEntry{}, reconciled.Services...), reconciled.DBServices...) {
		statuses[entry.Name] = entry.Status
	}
	return statuses, len(statuses) > 0
}

func statusOr(statuses map[string]string, name string, port int) string {
	if status, ok := statuses[name]; ok && status != "" {
		return status
	}
	if port == 0 {
		return "unknown"
	}
	if utils.IsPortListening(port) {
		return "running"
	}
	return "stopped"
}

func portURL(port int) string {
	if port == 0 {
		return ""
	}
	return fmt.Sprintf("http://localhost:%d", port)
}

func declaredProfiles(corgi *utils.CorgiCompose) []string {
	seen := map[string]bool{}
	var names []string
	for _, svc := range corgi.Services {
		for _, profile := range svc.Profiles {
			if !seen[profile] {
				seen[profile] = true
				names = append(names, profile)
			}
		}
	}
	sort.Strings(names)
	return names
}

func printContextReport(report contextReport) {
	head := report.Workspace
	if head == "" {
		head = report.ComposePath
	}
	utils.Info(head)
	utils.Info("  " + contextHeadline(report))
	utils.Info("")

	w := tabwriter.NewWriter(utils.ConsoleOut(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tKIND\tPORT\tSTATUS\tBRANCH\tREPO")
	for _, entry := range append(append([]contextEntry{}, report.Databases...), report.Services...) {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			entry.Name, entry.Kind, portCell(entry.Port), entry.Status, repoBranchCell(entry.Repo), repoNoteCell(entry.Repo))
	}
	w.Flush()

	for _, issue := range report.Errors {
		utils.Info("✖", issue.Code, issue.Message)
	}
	for _, issue := range report.Warnings {
		utils.Info("!", issue.Code, issue.Message)
	}
}

func contextHeadline(report contextReport) string {
	line := fmt.Sprintf("%d services · %d db_services", len(report.Services), len(report.Databases))
	if report.Tier != "" {
		line += " · tier " + report.Tier
	}
	if len(report.Profiles) > 0 {
		line += fmt.Sprintf(" · %d profiles", len(report.Profiles))
	}
	if report.Detached {
		line += " · detached run active"
	}
	return line
}

func portCell(port int) string {
	if port == 0 {
		return "-"
	}
	return fmt.Sprint(port)
}

func repoBranchCell(repo *contextRepo) string {
	if repo == nil || repo.Branch == "" {
		return "-"
	}
	return repo.Branch
}

func repoNoteCell(repo *contextRepo) string {
	if repo == nil {
		return ""
	}
	notes := ""
	if repo.Dirty {
		notes = "dirty"
	}
	if repo.Behind > 0 {
		notes = joinNote(notes, fmt.Sprintf("%d behind", repo.Behind))
	}
	if repo.Ahead > 0 {
		notes = joinNote(notes, fmt.Sprintf("%d ahead", repo.Ahead))
	}
	return notes
}

func joinNote(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + " · " + add
}
