package cmd

import (
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"

	"github.com/spf13/cobra"
)

// Every command merges the root's persistent flags the first time its flag set
// is read. A shorthand that collides with one of them panics at that moment,
// which is a broken binary rather than a failing command.
func TestEveryCommandMergesRootFlagsWithoutColliding(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.InheritedFlags()
		cmd.LocalFlags()
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
}

func TestContextReportShape(t *testing.T) {
	previousDir := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = t.TempDir()
	t.Cleanup(func() { utils.CorgiComposePathDir = previousDir })

	contextNoGit = true
	t.Cleanup(func() { contextNoGit = false })

	corgi := &utils.CorgiCompose{
		Name: "acme",
		Services: []utils.Service{
			{ServiceName: "web", Profiles: []string{"frontend"}},
			{ServiceName: "api", Profiles: []string{"backend"}},
		},
		DatabaseServices: []utils.DatabaseService{{ServiceName: "pg", Driver: "postgres"}},
	}

	report := buildContextReport(corgi)
	if report.Workspace != "acme" {
		t.Errorf("workspace = %q", report.Workspace)
	}
	if len(report.Services) != 2 || report.Services[0].Name != "api" {
		t.Fatalf("services = %+v, want them sorted by name", report.Services)
	}
	if len(report.Databases) != 1 || report.Databases[0].Kind != "db_service" {
		t.Fatalf("databases = %+v", report.Databases)
	}
	if strings.Join(report.Profiles, ",") != "backend,frontend" {
		t.Errorf("profiles = %v, want both, sorted", report.Profiles)
	}
	if report.Services[0].Repo != nil {
		t.Error("--no-git must leave repo state out")
	}
}

func TestWhyVerdictPrefersTheEarliestCause(t *testing.T) {
	service := utils.Service{ServiceName: "api", Port: 4000, Start: []string{"npm start"}}

	crashed := whyReport{Status: "crashed"}
	if verdict, _, _ := whyVerdict(service, crashed); verdict != verdictCrashed {
		t.Errorf("crashed → %q", verdict)
	}

	blocked := whyReport{Dependencies: []whyDependency{{Name: "pg", Kind: "db_service", Status: "stopped"}}}
	if verdict, detail, _ := whyVerdict(service, blocked); verdict != verdictDependency || !strings.Contains(detail, "pg") {
		t.Errorf("dependency → %q (%s)", verdict, detail)
	}

	taken := whyReport{Port: &whyPort{Number: 4000, Listening: true, Owner: "node(pid=1)"}}
	if verdict, _, _ := whyVerdict(service, taken); verdict != verdictPortTaken {
		t.Errorf("foreign port owner → %q", verdict)
	}

	ours := whyReport{Status: "running", Port: &whyPort{Number: 4000, Listening: true, Ours: true}}
	if verdict, _, _ := whyVerdict(service, ours); verdict != verdictHealthy {
		t.Errorf("running with its own port → %q", verdict)
	}

	envGap := whyReport{Env: &whyEnv{Missing: []string{"STRIPE_KEY"}}}
	if verdict, detail, _ := whyVerdict(service, envGap); verdict != verdictEnvMissing || !strings.Contains(detail, "STRIPE_KEY") {
		t.Errorf("missing env → %q (%s)", verdict, detail)
	}

	if verdict, _, next := whyVerdict(service, whyReport{}); verdict != verdictNotStarted || next == "" {
		t.Errorf("no run state → %q, next %q", verdict, next)
	}

	noStart := utils.Service{ServiceName: "api"}
	if verdict, _, _ := whyVerdict(noStart, whyReport{}); verdict != verdictNoStartScript {
		t.Errorf("no start command → %q", verdict)
	}
}

func TestEnvExplainMarksTheWinner(t *testing.T) {
	chain := []utils.EnvVar{
		{Key: "DATABASE_URL", Value: "postgres://compose", Source: "literal"},
		{Key: "OTHER", Value: "x", Source: "literal"},
		{Key: "DATABASE_URL", Value: "postgres://file", Source: "file:.env"},
	}
	report := buildEnvExplain("api", "DATABASE_URL", chain)
	if !report.Found || len(report.Chain) != 2 {
		t.Fatalf("report = %+v", report)
	}
	if report.Chain[0].Used || !report.Chain[1].Used {
		t.Error("the last contributor is the one in effect")
	}
	if report.Value != "postgres://file" {
		t.Errorf("value = %q", report.Value)
	}

	missing := buildEnvExplain("api", "NOPE", chain)
	if missing.Found || len(missing.Chain) != 0 {
		t.Errorf("unset key = %+v", missing)
	}
}

func TestNarrowToChangedServicesKeepsUnknownRepos(t *testing.T) {
	sel := selection{services: []utils.Service{
		{ServiceName: "api", AbsolutePath: t.TempDir()},
		{ServiceName: "web"},
	}}
	got := narrowToChangedServices(sel, "main")
	if len(got.services) != 2 {
		t.Fatalf("a repo corgi cannot compare must still be tested, got %+v", got.services)
	}
}

func TestParseSince(t *testing.T) {
	at, err := parseSince("5m")
	if err != nil || time.Since(at) < 4*time.Minute {
		t.Fatalf("duration form: %v %v", at, err)
	}
	if _, err := parseSince("2026-01-02T03:04:05Z"); err != nil {
		t.Errorf("RFC3339 form: %v", err)
	}
	if _, err := parseSince("yesterday"); err == nil {
		t.Error("an unparsable --since must be an error")
	}
}

func TestLogStreamFilterAllows(t *testing.T) {
	previous := activeLogFilter
	t.Cleanup(func() { activeLogFilter = previous })

	logsSinceFlag, logsGrepFlag, logsWaitForFlag = "1h", "ERROR", ""
	t.Cleanup(func() { logsSinceFlag, logsGrepFlag, logsWaitForFlag = "", "", "" })

	filter, err := buildLogStreamFilter()
	if err != nil {
		t.Fatal(err)
	}
	recent := time.Now().UTC().Format(time.RFC3339)
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)

	if !filter.allows(recent, "boom ERROR here") {
		t.Error("a recent matching line must pass")
	}
	if filter.allows(recent, "all good") {
		t.Error("--grep must drop non-matching lines")
	}
	if filter.allows(old, "boom ERROR here") {
		t.Error("--since must drop older lines")
	}
}

func TestDiffStackStateReportsTransitions(t *testing.T) {
	previous := map[string]string{"api": "running", "web": "running"}
	next := map[string]string{"api": "crashed"}
	exit := 137
	events := diffStackState(previous, next, map[string]int{"api": 4000}, map[string]*int{"api": &exit})

	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Service != "api" || events[0].Kind != "crashed" || *events[0].ExitCode != 137 {
		t.Errorf("crash event = %+v", events[0])
	}
	if events[1].Service != "web" || events[1].Kind != "gone" {
		t.Errorf("disappearance = %+v", events[1])
	}
	if len(diffStackState(next, next, nil, nil)) != 0 {
		t.Error("an unchanged state emits nothing")
	}
}
