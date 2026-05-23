package cmd

import (
	"andriiklymiuk/corgi/utils"
	"strings"
	"testing"
)

func TestCollectDeclaredPorts_IncludesDbAndServicesSorted(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "api-db", Driver: "postgres", Port: 5432},
			{ServiceName: "shared-aws", Driver: "localstack", Port: 4566},
		},
		Services: []utils.Service{
			{ServiceName: "api-secondary", Port: 3010},
			{ServiceName: "api", Port: 3030},
		},
	}
	ports := collectDeclaredPorts(corgi)
	want := []int{3010, 3030, 4566, 5432}
	if len(ports) != len(want) {
		t.Fatalf("expected %d ports, got %d: %+v", len(want), len(ports), ports)
	}
	for i, p := range ports {
		if p.Port != want[i] {
			t.Errorf("index %d: want %d, got %d (full: %+v)", i, want[i], p.Port, ports)
		}
	}
}

func TestCollectDeclaredPorts_SkipsZeroPortAndManualRun(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "with-port", Driver: "postgres", Port: 5432},
			{ServiceName: "no-port", Driver: "postgres", Port: 0},
		},
		Services: []utils.Service{
			{ServiceName: "normal", Port: 3030},
			{ServiceName: "manual", Port: 9999, ManualRun: true},
			{ServiceName: "zero", Port: 0},
		},
	}
	ports := collectDeclaredPorts(corgi)
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %d: %+v", len(ports), ports)
	}
	for _, p := range ports {
		if p.Port == 0 || p.Port == 9999 {
			t.Errorf("unexpected port %d slipped through: %+v", p.Port, p)
		}
	}
}

func TestCollectDeclaredPorts_Empty(t *testing.T) {
	corgi := &utils.CorgiCompose{}
	ports := collectDeclaredPorts(corgi)
	if len(ports) != 0 {
		t.Fatalf("expected no ports for empty compose, got %+v", ports)
	}
}

func TestCollectDeclaredPorts_DescIncludesDriver(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "shared-aws", Driver: "localstack", Port: 4566},
		},
	}
	ports := collectDeclaredPorts(corgi)
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Desc != "db_services.shared-aws (localstack)" {
		t.Errorf("unexpected desc: %q", ports[0].Desc)
	}
}


func TestRunRequiredEmpty(t *testing.T) {
	if !RunRequired(nil) {
		t.Error("want true for empty")
	}
}

func TestRunDockerCheckNoDb(t *testing.T) {
	if !runDockerCheck(&utils.CorgiCompose{}) {
		t.Error("want true when no db")
	}
}

func TestCollectDeclaredPortsNoServices(t *testing.T) {
	got := collectDeclaredPorts(&utils.CorgiCompose{})
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestCollectDeclaredPortsSorts(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Driver: "postgres", Port: 5432},
			{ServiceName: "redis", Driver: "redis", Port: 6379},
		},
		Services: []utils.Service{
			{ServiceName: "api", Port: 3000},
		},
	}
	got := collectDeclaredPorts(corgi)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if got[0].Port != 3000 || got[1].Port != 5432 || got[2].Port != 6379 {
		t.Errorf("not sorted: %v", got)
	}
}

func TestCollectDeclaredPortsSkipsZeroAndManual(t *testing.T) {
	corgi := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Driver: "postgres", Port: 0},
		},
		Services: []utils.Service{
			{ServiceName: "manual", Port: 9999, ManualRun: true},
			{ServiceName: "ok", Port: 3000},
		},
	}
	got := collectDeclaredPorts(corgi)
	if len(got) != 1 || got[0].Port != 3000 {
		t.Errorf("got %v", got)
	}
}

func TestRunPortChecksNoPorts(t *testing.T) {
	if !runPortChecks(&utils.CorgiCompose{}) {
		t.Error("want true when no ports")
	}
}

func TestCheckRequiredIsFoundExistingCmd(t *testing.T) {
	ok, desc := checkRequiredIsFound(utils.Required{Name: "echo", CheckCmd: "echo --version"})
	if !ok {
		t.Errorf("expected echo found, desc=%s", desc)
	}
}

func TestCheckRequiredIsFoundMissing(t *testing.T) {
	ok, _ := checkRequiredIsFound(utils.Required{Name: "this-tool-does-not-exist-zzz"})
	if ok {
		t.Error("expected not found")
	}
}

func TestDoctorResultComputeOK(t *testing.T) {
	res := doctorResult{Checks: []doctorCheck{{Name: "docker", OK: true}, {Name: "port:5432", OK: false, Detail: "in use"}}}
	res.computeOK()
	if res.OK {
		t.Error("overall OK must be false when any check fails")
	}
	res2 := doctorResult{Checks: []doctorCheck{{Name: "docker", OK: true}}}
	res2.computeOK()
	if !res2.OK {
		t.Error("overall OK must be true when all checks pass")
	}
}

func TestCheckRequiredIsFoundQuiet(t *testing.T) {
	ok, _ := checkRequiredIsFoundQuiet(utils.Required{Name: "echo", CheckCmd: "echo --version"})
	if !ok {
		t.Error("expected echo to be found")
	}
	ok, detail := checkRequiredIsFoundQuiet(utils.Required{Name: "this-tool-does-not-exist-zzz"})
	if ok {
		t.Error("expected missing tool to report not found")
	}
	if detail == "" {
		t.Error("expected a detail message for missing tool")
	}
}

func TestProcessRequired_Found(t *testing.T) {
	if !processRequired(utils.Required{Name: "echo", CheckCmd: "echo --version"}) {
		t.Error("expected processRequired true for an installed tool")
	}
}

func TestProcessRequired_MissingNoInstallSteps(t *testing.T) {
	// Missing tool with no install steps must fail without prompting.
	if processRequired(utils.Required{Name: "this-tool-does-not-exist-zzz"}) {
		t.Error("expected false when tool missing and no install steps")
	}
}

func TestProcessRequired_OptionalNonInteractiveSkips(t *testing.T) {
	orig := utils.NonInteractive
	defer func() { utils.NonInteractive = orig }()
	utils.NonInteractive = true
	// Optional + non-interactive: skipped (no prompt, no install attempt), returns false.
	got := processRequired(utils.Required{
		Name:     "this-tool-does-not-exist-zzz",
		Optional: true,
		Install:  []string{"echo noop"},
	})
	if got {
		t.Error("expected false for optional missing tool in non-interactive mode")
	}
}

func TestBuildDoctorResult_RequiredPresent(t *testing.T) {
	// 'go' is guaranteed present in this test environment.
	corgi := &utils.CorgiCompose{
		Required: []utils.Required{{Name: "go", CheckCmd: "go version"}},
	}
	res := buildDoctorResult(corgi)
	if len(res.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d: %+v", len(res.Checks), res.Checks)
	}
	c := res.Checks[0]
	if c.Name != "required:go" || !c.OK || c.Detail != "" {
		t.Errorf("expected ok required check, got %+v", c)
	}
	if !res.OK {
		t.Error("overall result must be OK when the only check passes")
	}
}

func TestBuildDoctorResult_RequiredMissing(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Required: []utils.Required{{Name: "this-tool-does-not-exist-zzz"}},
	}
	res := buildDoctorResult(corgi)
	if len(res.Checks) != 1 {
		t.Fatalf("expected 1 check, got %+v", res.Checks)
	}
	c := res.Checks[0]
	if c.OK {
		t.Error("missing tool check must be ok=false")
	}
	if c.Detail != "not found" {
		t.Errorf("expected 'not found' detail, got %q", c.Detail)
	}
	if res.OK {
		t.Error("overall result must be false when a check fails")
	}
}

func TestBuildDoctorResult_OKIsAndOfChecks(t *testing.T) {
	// One present + one missing required → overall false (AND of checks).
	corgi := &utils.CorgiCompose{
		Required: []utils.Required{
			{Name: "go", CheckCmd: "go version"},
			{Name: "this-tool-does-not-exist-zzz"},
		},
	}
	res := buildDoctorResult(corgi)
	if len(res.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %+v", res.Checks)
	}
	if !res.Checks[0].OK || res.Checks[1].OK {
		t.Errorf("expected [ok, not-ok], got %+v", res.Checks)
	}
	if res.OK {
		t.Error("overall must be false when any check fails")
	}
}

func TestRunRequired_AllPresent(t *testing.T) {
	if !RunRequired([]utils.Required{{Name: "go", CheckCmd: "go version"}}) {
		t.Error("expected true when all required tools present")
	}
}

func TestRunRequired_ReportsMissing(t *testing.T) {
	got := RunRequired([]utils.Required{
		{Name: "go", CheckCmd: "go version"},
		{Name: "this-tool-does-not-exist-zzz"},
	})
	if got {
		t.Error("expected false when a required tool is missing")
	}
}

func TestProcessRequired_RequiredRunsInstallThenRechecks(t *testing.T) {
	// Non-optional missing tool: no prompt, runs the (harmless) install step,
	// re-checks, still absent → false. Exercises the install loop + recheck.
	got := processRequired(utils.Required{
		Name:    "this-tool-does-not-exist-zzz",
		Why:     []string{"to test the install path"},
		Install: []string{"echo installing"},
	})
	if got {
		t.Error("expected false: tool still missing after install step")
	}
}

func TestRunDoctorJSON_EmptyComposePasses(t *testing.T) {
	origJSON := utils.JSONOutput
	defer func() { utils.JSONOutput = origJSON }()
	utils.JSONOutput = true
	// No required tools, no db_services, no ports → all checks pass, no os.Exit.
	out := captureStdout(t, func() { runDoctorJSON(&utils.CorgiCompose{}) })
	if !strings.Contains(out, `"ok": true`) {
		t.Errorf("expected ok:true JSON for empty compose, got %q", out)
	}
}


func TestFixDecision(t *testing.T) {
	cases := []struct {
		name           string
		kind           fixKind
		nonInteractive bool
		yes            bool
		want           bool
	}{
		{"docker always safe", fixDocker, true, false, true},
		{"tool needs yes in non-interactive", fixInstall, true, false, false},
		{"tool with yes", fixInstall, true, true, true},
		{"kill never auto", fixKillPort, true, false, false},
		{"kill with yes", fixKillPort, true, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldAutoFix(c.kind, c.nonInteractive, c.yes)
			if got != c.want {
				t.Fatalf("shouldAutoFix(%v,%v,%v)=%v want %v",
					c.kind, c.nonInteractive, c.yes, got, c.want)
			}
		})
	}
}

func TestRunFixes_SkipsDestructiveWhenNonInteractiveNoYes(t *testing.T) {
	res := doctorResult{Checks: []doctorCheck{
		{Name: "port:3000", OK: false, Detail: "busy"},
	}}
	acts := fixActions{
		startDocker: func() error { return nil },
		installTool: func(string) error { return nil },
		killPort:    func(int) error { t.Fatal("killPort must not be called"); return nil },
		confirm:     func(string) bool { return true },
	}
	out := runFixes(res, acts, true, false)
	if len(out.Skipped) != 1 || out.Skipped[0].Check != "port:3000" {
		t.Fatalf("expected port:3000 skipped, got %+v", out.Skipped)
	}
	if out.OK {
		t.Fatal("expected OK=false when a check was skipped")
	}
}

func TestRunFixes_KillsWithYes(t *testing.T) {
	killed := 0
	res := doctorResult{Checks: []doctorCheck{{Name: "port:3000", OK: false}}}
	acts := fixActions{killPort: func(int) error { killed++; return nil }}
	out := runFixes(res, acts, true, true)
	if killed != 1 || len(out.Fixed) != 1 || !out.OK {
		t.Fatalf("expected one kill + fixed, got killed=%d out=%+v", killed, out)
	}
}

func TestRunFixes_InteractiveDestructiveAsksConfirm(t *testing.T) {
	asked := false
	res := doctorResult{Checks: []doctorCheck{{Name: "port:3000", OK: false}}}
	acts := fixActions{
		killPort: func(int) error { t.Fatal("must not kill when declined"); return nil },
		confirm:  func(string) bool { asked = true; return false },
	}
	out := runFixes(res, acts, false, false)
	if !asked {
		t.Fatal("expected confirm to be asked in interactive destructive fix")
	}
	if len(out.Skipped) != 1 || out.Skipped[0].Reason != "declined" {
		t.Fatalf("expected declined skip, got %+v", out.Skipped)
	}
}
