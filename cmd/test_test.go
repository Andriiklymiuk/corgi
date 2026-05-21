package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"andriiklymiuk/corgi/utils"
)

// testScriptService builds a service rooted at dir with a `test` script.
func testScriptService(name, dir string, commands ...string) utils.Service {
	return utils.Service{
		ServiceName:  name,
		AbsolutePath: dir,
		Scripts:      []utils.Script{{Name: "test", Commands: commands}},
	}
}

func TestRunTests_PassingService(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{testScriptService("api", t.TempDir(), "sh -c 'exit 0'")},
	}
	sel, err := resolveSelection(corgi, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results, allPassed := runTests(corgi, sel, false, time.Second)
	if !allPassed {
		t.Errorf("expected allPassed=true")
	}
	if len(results) != 1 || !results[0].Passed || results[0].ExitCode != 0 {
		t.Errorf("expected one passed result, got %+v", results)
	}
}

func TestRunTests_FailingService(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{testScriptService("api", t.TempDir(), "sh -c 'exit 1'")},
	}
	sel, _ := resolveSelection(corgi, "", "")

	results, allPassed := runTests(corgi, sel, false, time.Second)
	if allPassed {
		t.Errorf("expected allPassed=false")
	}
	if len(results) != 1 || results[0].Passed || results[0].ExitCode != 1 {
		t.Errorf("expected one failed result with exitCode 1, got %+v", results)
	}
}

func TestRunTests_StopsOnFirstFailingCommand(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			testScriptService("api", t.TempDir(), "sh -c 'exit 5'", "sh -c 'exit 0'"),
		},
	}
	sel, _ := resolveSelection(corgi, "", "")

	results, allPassed := runTests(corgi, sel, false, time.Second)
	if allPassed {
		t.Errorf("expected allPassed=false")
	}
	if results[0].ExitCode != 5 {
		t.Errorf("expected exitCode 5 from first failing command, got %d", results[0].ExitCode)
	}
}

func TestRunTests_NoTestScriptIsSkipped(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "web", AbsolutePath: t.TempDir()}, // no scripts
		},
	}
	sel, _ := resolveSelection(corgi, "", "")

	results, allPassed := runTests(corgi, sel, false, time.Second)
	if !allPassed {
		t.Errorf("a skipped service must not fail the run; got allPassed=false")
	}
	if len(results) != 1 || !results[0].Skipped {
		t.Errorf("expected skipped result, got %+v", results)
	}
	if results[0].Passed {
		t.Errorf("skipped service must not be marked passed")
	}
}

func TestResolveSelection_UnknownService(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{testScriptService("api", t.TempDir(), "true")},
	}
	_, err := resolveSelection(corgi, "nope", "")
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "api") {
		t.Errorf("expected message listing valid services, got %q", err)
	}
}

func TestResolveSelection_SingleService(t *testing.T) {
	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			testScriptService("api", t.TempDir(), "true"),
			testScriptService("web", t.TempDir(), "true"),
		},
	}
	sel, err := resolveSelection(corgi, "web", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sel.services) != 1 || sel.services[0].ServiceName != "web" {
		t.Errorf("expected only web selected, got %+v", sel.services)
	}
}

func TestReportTestResults_JSONShape(t *testing.T) {
	prev := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = prev })

	corgi := &utils.CorgiCompose{
		Services: []utils.Service{
			testScriptService("api", t.TempDir(), "sh -c 'echo running; exit 0'"),
			{ServiceName: "web", AbsolutePath: t.TempDir()}, // skipped
		},
	}
	sel, _ := resolveSelection(corgi, "", "")
	results, allPassed := runTests(corgi, sel, false, time.Second)

	out := captureStdout(t, func() {
		reportTestResults(results, allPassed)
	})

	var payload struct {
		Services []testResult `json:"services"`
		Passed   bool         `json:"passed"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &payload); err != nil {
		t.Fatalf("stdout is not pure JSON: %v\noutput: %q", err, out)
	}
	if !payload.Passed {
		t.Errorf("expected passed=true")
	}
	if len(payload.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(payload.Services))
	}
	if payload.Services[0].Name != "api" || !payload.Services[0].Passed {
		t.Errorf("expected api passed, got %+v", payload.Services[0])
	}
	if payload.Services[1].Name != "web" || !payload.Services[1].Skipped {
		t.Errorf("expected web skipped, got %+v", payload.Services[1])
	}
	// Child output ("running") must not leak onto stdout in JSON mode.
	if strings.Contains(out, "running") {
		t.Errorf("child output leaked into stdout: %q", out)
	}
}

func TestRunTests_JSONServicesNeverNull(t *testing.T) {
	prev := utils.JSONOutput
	utils.JSONOutput = true
	t.Cleanup(func() { utils.JSONOutput = prev })

	corgi := &utils.CorgiCompose{} // no services
	sel, _ := resolveSelection(corgi, "", "")
	results, allPassed := runTests(corgi, sel, false, time.Second)

	out := captureStdout(t, func() {
		reportTestResults(results, allPassed)
	})
	if strings.Contains(out, "null") {
		t.Errorf("services array must never be null, got %q", out)
	}
}
