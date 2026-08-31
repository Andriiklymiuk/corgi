package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-qm", "init")
}

func workspaceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	previous := utils.CorgiComposePathDir
	utils.CorgiComposePathDir = dir
	t.Cleanup(func() { utils.CorgiComposePathDir = previous })
	return dir
}

func writeState(t *testing.T, dir string, services, dbs []utils.RunStateEntry) {
	t.Helper()
	if err := utils.WriteRunState(utils.RunStatePath(dir), utils.RunState{
		ComposePath: dir, Services: services, DBServices: dbs,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointFileRoundTrip(t *testing.T) {
	workspaceDir(t)

	first := checkpointFile{
		Name: "before", CreatedAt: time.Now().UTC().Add(-time.Hour),
		Repos: []checkpointRepo{{Name: "api", Path: "/repos/api", Branch: "main", Head: "abc123def456"}},
	}
	second := checkpointFile{Name: "after", CreatedAt: time.Now().UTC(), Repos: first.Repos,
		Databases: []checkpointDatabase{{Service: "pg", Snapshot: "checkpoint-after"}}}

	for _, file := range []checkpointFile{first, second} {
		if err := writeCheckpoint(file); err != nil {
			t.Fatal(err)
		}
	}

	got, err := readCheckpoint("before")
	if err != nil || got.Repos[0].Head != "abc123def456" {
		t.Fatalf("read back = %+v, err %v", got, err)
	}

	listed, err := listCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Name != "after" {
		t.Fatalf("listed = %+v, want newest first", listed)
	}
	if dbSuffix(listed[0]) != " + 1 db" || dbSuffix(listed[1]) != "" {
		t.Errorf("db suffix = %q / %q", dbSuffix(listed[0]), dbSuffix(listed[1]))
	}
	if _, err := readCheckpoint("nope"); err == nil {
		t.Error("an unknown checkpoint must error")
	}
}

func TestCaptureRepoRecordsDirtyWork(t *testing.T) {
	needGit(t)
	workspaceDir(t)
	repo := filepath.Join(t.TempDir(), "api")
	newRepo(t, repo)

	clean, ok := captureRepo(checkoutTarget{name: "api", path: repo}, "cp")
	if !ok || clean.StashSha != "" || clean.Branch != "main" || clean.Head == "" {
		t.Fatalf("clean repo = %+v ok=%v", clean, ok)
	}
	if capturedSuffix(clean) != "" {
		t.Error("a clean repo has nothing captured")
	}

	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, _ := captureRepo(checkoutTarget{name: "api", path: repo}, "cp")
	if dirty.StashSha == "" {
		t.Fatal("uncommitted work must be captured")
	}
	if !strings.Contains(capturedSuffix(dirty), "captured") {
		t.Errorf("suffix = %q", capturedSuffix(dirty))
	}
	if !strings.Contains(shortHead(dirty), "main @ ") {
		t.Errorf("shortHead = %q", shortHead(dirty))
	}

	if _, ok := captureRepo(checkoutTarget{name: "x", path: t.TempDir()}, "cp"); ok {
		t.Error("a non-repo cannot be captured")
	}
	if names := dirtyRepos([]checkpointRepo{{Name: "api", Path: repo}}); len(names) != 1 {
		t.Errorf("dirtyRepos = %v", names)
	}
}

func TestCheckpointTargetsPutWorkspaceFirst(t *testing.T) {
	dir := workspaceDir(t)
	corgi := &utils.CorgiCompose{Services: []utils.Service{
		{ServiceName: "web", AbsolutePath: "/repos/web"},
		{ServiceName: "api", AbsolutePath: "/repos/api"},
		{ServiceName: "ghost"},
	}}
	targets := checkpointTargets(corgi)
	if len(targets) != 3 {
		t.Fatalf("targets = %+v", targets)
	}
	if targets[0].name != workspaceRepoName || targets[0].path != dir {
		t.Errorf("first target = %+v, want the workspace", targets[0])
	}
	if targets[1].name != "api" || targets[2].name != "web" {
		t.Errorf("services must be sorted: %+v", targets[1:])
	}
}

func TestShortHeadWithoutBranch(t *testing.T) {
	if got := shortHead(checkpointRepo{Head: "0123456789abcdef"}); got != "01234567" {
		t.Errorf("detached head = %q", got)
	}
	if got := trimJSONExt("before.json"); got != "before" {
		t.Errorf("trimJSONExt = %q", got)
	}
}

func TestContextStatusesReadRunState(t *testing.T) {
	dir := workspaceDir(t)

	statuses, detached := contextStatuses(&utils.CorgiCompose{})
	if detached || len(statuses) != 0 {
		t.Fatal("no state file means nothing is detached")
	}

	writeState(t, dir,
		[]utils.RunStateEntry{{Name: "api", Kind: "service", Status: "running", PID: os.Getpid(), Port: 4000}},
		[]utils.RunStateEntry{{Name: "pg", Kind: "db_service", Status: "running"}})

	statuses, detached = contextStatuses(&utils.CorgiCompose{})
	if !detached || statuses["api"] == "" || statuses["pg"] != "running" {
		t.Fatalf("statuses = %v detached = %v", statuses, detached)
	}
}

func TestStatusOrFallsBackToAPortProbe(t *testing.T) {
	statuses := map[string]string{"api": "running"}
	if got := statusOr(statuses, "api", 4000); got != "running" {
		t.Errorf("known status = %q", got)
	}
	if got := statusOr(statuses, "web", 0); got != "unknown" {
		t.Errorf("portless unknown service = %q", got)
	}
	if got := statusOr(statuses, "web", 65533); got != "stopped" {
		t.Errorf("closed port = %q", got)
	}
}

func TestContextPresentationHelpers(t *testing.T) {
	if portURL(0) != "" || portURL(4000) != "http://localhost:4000" {
		t.Error("portURL")
	}
	if portCell(0) != "-" || portCell(80) != "80" {
		t.Error("portCell")
	}
	if repoBranchCell(nil) != "-" || repoBranchCell(&contextRepo{}) != "-" {
		t.Error("a repo with no branch shows a dash")
	}
	if repoBranchCell(&contextRepo{Branch: "main"}) != "main" {
		t.Error("repoBranchCell")
	}
	if repoNoteCell(nil) != "" {
		t.Error("no repo, no note")
	}
	note := repoNoteCell(&contextRepo{Dirty: true, Behind: 3, Ahead: 1})
	if note != "dirty · 3 behind · 1 ahead" {
		t.Errorf("note = %q", note)
	}
	if joinNote("", "a") != "a" || joinNote("a", "b") != "a · b" {
		t.Error("joinNote")
	}
}

func TestContextHeadlineSummarises(t *testing.T) {
	report := contextReport{
		Services:  []contextEntry{{}, {}},
		Databases: []contextEntry{{}},
		Tier:      "staging",
		Profiles:  []string{"backend"},
		Detached:  true,
	}
	line := contextHeadline(report)
	for _, want := range []string{"2 services", "1 db_services", "tier staging", "1 profiles", "detached"} {
		if !strings.Contains(line, want) {
			t.Errorf("headline %q is missing %q", line, want)
		}
	}
}

func TestReadContextRepo(t *testing.T) {
	needGit(t)
	repo := filepath.Join(t.TempDir(), "api")
	newRepo(t, repo)

	got := readContextRepo(repo)
	if got == nil || got.Branch != "main" || got.Head == "" {
		t.Fatalf("repo = %+v", got)
	}
	if readContextRepo(t.TempDir()) != nil {
		t.Error("a plain directory has no repo state")
	}
}

func TestUnreadyDependenciesSkipsRunningOnes(t *testing.T) {
	corgi := &utils.CorgiCompose{}
	service := utils.Service{
		ServiceName:       "api",
		DependsOnDb:       []utils.DependsOnDb{{Name: "pg"}, {Name: "redis"}},
		DependsOnServices: []utils.DependsOnService{{Name: "auth"}},
	}
	statuses := map[string]string{"pg": "running", "redis": "stopped"}

	deps := unreadyDependencies(corgi, service, statuses)
	if len(deps) != 2 {
		t.Fatalf("deps = %+v", deps)
	}
	if deps[0].Name != "redis" || deps[0].Kind != "db_service" || deps[0].Status != "stopped" {
		t.Errorf("db dep = %+v", deps[0])
	}
	if deps[1].Name != "auth" || deps[1].Kind != "service" || deps[1].Status != "not started" {
		t.Errorf("service dep = %+v", deps[1])
	}
}

func TestProbeServicePortAndRendering(t *testing.T) {
	if probeServicePort(utils.Service{}, "running") != nil {
		t.Error("a portless service has nothing to probe")
	}
	port := probeServicePort(utils.Service{Port: 65533}, "stopped")
	if port == nil || port.Listening {
		t.Fatalf("port = %+v", port)
	}
	if portStateLine(port) != "nothing listening" {
		t.Errorf("state line = %q", portStateLine(port))
	}
	if portStateLine(&whyPort{Listening: true, Ours: true}) != "listening (this service)" {
		t.Error("our own port")
	}
	if !strings.Contains(portStateLine(&whyPort{Listening: true, Owner: "node(pid=1)"}), "node(pid=1)") {
		t.Error("a foreign owner must be named")
	}
}

func TestLastExitCodeFor(t *testing.T) {
	dir := workspaceDir(t)
	if lastExitCodeFor("api") != nil {
		t.Error("no state file means no exit code")
	}
	code := 137
	writeState(t, dir, []utils.RunStateEntry{{Name: "api", Status: "crashed", ExitCode: &code}}, nil)

	got := lastExitCodeFor("api")
	if got == nil || *got != 137 {
		t.Fatalf("exit code = %v", got)
	}
	if lastExitCodeFor("ghost") != nil {
		t.Error("an unknown service has no exit code")
	}
	if exitCodeSuffix(nil) != "" || exitCodeSuffix(got) != " with code 137" {
		t.Error("exitCodeSuffix")
	}
}

func TestTailServiceLogKeepsTheLastLines(t *testing.T) {
	dir := workspaceDir(t)
	logDir := filepath.Join(dir, "corgi_services", ".logs", "api")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "2026-01-01T00:00:00.000Z first\n\n2026-01-01T00:00:01.000Z second\nthird\n"
	if err := os.WriteFile(filepath.Join(logDir, "2026-01-01_000000.ok.log"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	lines := tailServiceLog("api", 2)
	if len(lines) != 2 || lines[0] != "second" || lines[1] != "third" {
		t.Fatalf("tail = %q", lines)
	}
	if tailServiceLog("api", 0) != nil {
		t.Error("asking for no lines returns none")
	}
	if tailServiceLog("ghost", 3) != nil {
		t.Error("a service with no log returns none")
	}
}

func TestStripLogTimestamp(t *testing.T) {
	if got := stripLogTimestamp("2026-01-01T00:00:00.000Z hello"); got != "hello" {
		t.Errorf("stamped = %q", got)
	}
	if got := stripLogTimestamp("plain line"); got != "plain line" {
		t.Errorf("unstamped = %q", got)
	}
	if got := stripLogTimestamp("not-a-timestamp-but-long-enough xx"); got != "not-a-timestamp-but-long-enough xx" {
		t.Errorf("lookalike = %q", got)
	}
}

func TestFindServiceByName(t *testing.T) {
	corgi := &utils.CorgiCompose{Services: []utils.Service{{ServiceName: "api"}}}
	if _, ok := findServiceByName(corgi, "api"); !ok {
		t.Error("known service")
	}
	if _, ok := findServiceByName(corgi, "ghost"); ok {
		t.Error("unknown service")
	}
}

func TestUnresolvedPlaceholders(t *testing.T) {
	corgi := &utils.CorgiCompose{}
	service := utils.Service{ServiceName: "api", Environment: []string{"FILLED=yes"},
		EnvPlaceholdersToCheck: []string{"FILLED", "EMPTY"}}
	corgi.Services = []utils.Service{service}

	got := unresolvedPlaceholders(corgi, service)
	if len(got) != 1 || got[0] != "EMPTY" {
		t.Fatalf("unresolved = %v", got)
	}
	if unresolvedPlaceholders(corgi, utils.Service{ServiceName: "api"}) != nil {
		t.Error("no placeholders declared, nothing to report")
	}
}

func TestReadStackStateAndBaseline(t *testing.T) {
	dir := workspaceDir(t)
	statuses, ports, exits := readStackState()
	if len(statuses) != 0 || len(ports) != 0 || len(exits) != 0 {
		t.Fatal("no state file means empty maps")
	}

	writeState(t, dir,
		[]utils.RunStateEntry{{Name: "api", Status: "running", PID: os.Getpid(), Port: 4000}}, nil)

	statuses, ports, exits = readStackState()
	if ports["api"] != 4000 {
		t.Fatalf("ports = %v", ports)
	}
	events := baselineEvents(statuses, ports, exits)
	if len(events) != 1 || events[0].Kind != "state" || events[0].Port != 4000 || events[0].At == "" {
		t.Fatalf("baseline = %+v", events)
	}
}

func TestTransitionKindAndEncoding(t *testing.T) {
	for status, want := range map[string]string{
		"running": "started", "crashed": "crashed", "stopped": "stopped", "weird": "state",
	} {
		if got := transitionKind(status); got != want {
			t.Errorf("transitionKind(%q) = %q, want %q", status, got, want)
		}
	}
	encoded := compactStackEvent(stackEvent{Service: "api", Kind: "started"})
	if !strings.Contains(encoded, `"service":"api"`) || strings.Contains(encoded, "\n") {
		t.Errorf("encoded = %q, want one compact line", encoded)
	}
}

func TestLeasePortSummaryIsSorted(t *testing.T) {
	got := leasePortSummary(utils.Lease{Ports: map[string]int{"web": 3100, "api": 4100}})
	if got != "api:4100 web:3100" {
		t.Errorf("summary = %q", got)
	}
	if leasePortSummary(utils.Lease{}) != "" {
		t.Error("a lease with no ports summarises to nothing")
	}
}

func captureConsole(t *testing.T, run func()) string {
	t.Helper()
	var buf strings.Builder
	utils.SetConsoleOverride(&buf)
	t.Cleanup(utils.ClearConsoleOverride)
	run()
	utils.ClearConsoleOverride()
	return buf.String()
}

func TestPrintContextReportRendersEveryColumn(t *testing.T) {
	report := contextReport{
		Workspace: "acme",
		Databases: []contextEntry{{Name: "pg", Kind: "db_service", Port: 5432, Status: "running"}},
		Services: []contextEntry{
			{Name: "api", Kind: "service", Port: 4000, Status: "running",
				Repo: &contextRepo{Branch: "feature/x", Dirty: true, Behind: 2}},
		},
		Errors:   []utils.ValidationIssue{{Code: "E_DANGLING_DEP", Message: "api depends on ghost"}},
		Warnings: []utils.ValidationIssue{{Code: "W_NO_HEALTHCHECK", Message: "api has no healthCheck"}},
	}
	out := captureConsole(t, func() { printContextReport(report) })

	for _, want := range []string{"acme", "E_DANGLING_DEP", "W_NO_HEALTHCHECK", "1 services", "1 db_services"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestPrintWhyReportShowsTheEvidence(t *testing.T) {
	code := 137
	report := whyReport{
		Service: "api", Verdict: verdictCrashed, Detail: "it exited",
		Port:         &whyPort{Number: 4000, Listening: false},
		LastExitCode: &code,
		Dependencies: []whyDependency{{Name: "pg", Kind: "db_service", Status: "stopped"}},
		Env:          &whyEnv{SourceFileMissing: true, Missing: []string{"STRIPE_KEY"}, Unresolved: []string{"API_URL"}},
		LogTail:      []string{"panic: boom"},
		NextStep:     "corgi logs --service api",
	}
	out := captureConsole(t, func() { printWhyReport(report) })

	for _, want := range []string{"api → crashed", "port 4000", "waiting on db_service pg",
		"env file is missing", "STRIPE_KEY", "API_URL", "panic: boom", "next:"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
}

func TestEmitStackEventHumanForm(t *testing.T) {
	code := 137
	out := captureConsole(t, func() {
		emitStackEvent(stackEvent{At: "2026-01-01T00:00:00Z", Service: "worker", Kind: "crashed",
			Status: "crashed", ExitCode: &code})
	})
	if !strings.Contains(out, "worker") || !strings.Contains(out, "crashed") || !strings.Contains(out, "exit 137") {
		t.Errorf("event line = %q", out)
	}

	out = captureConsole(t, func() {
		emitStackEvent(stackEvent{At: "t", Service: "api", Kind: "state", Status: "running"})
	})
	if !strings.Contains(out, "state · running") {
		t.Errorf("a state event names its status: %q", out)
	}
}

func TestCheckpointsDirLivesUnderCorgiServices(t *testing.T) {
	if got := utils.CheckpointsDir("/ws"); got != filepath.Join("/ws", "corgi_services", ".checkpoints") {
		t.Errorf("CheckpointsDir = %q", got)
	}
}
