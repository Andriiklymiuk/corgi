package cmd

import (
	"andriiklymiuk/corgi/utils"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// buildReport mirrors runValidate's report assembly without the os.Exit /
// flag plumbing so the JSON shape and exit decision can be asserted directly.
func buildReport(c *utils.CorgiCompose, strict bool) (validateReport, bool) {
	errs, warns := utils.ValidateCompose(c)
	if errs == nil {
		errs = []utils.ValidationIssue{}
	}
	if warns == nil {
		warns = []utils.ValidationIssue{}
	}
	failed := len(errs) > 0 || (strict && len(warns) > 0)
	return validateReport{Ok: !failed, Errors: errs, Warnings: warns}, failed
}

func TestPrintValidateHuman_Branches(t *testing.T) {
	prev := utils.JSONOutput
	utils.JSONOutput = false
	t.Cleanup(func() { utils.JSONOutput = prev })

	errItem := utils.ValidationIssue{Code: "E_X", Message: "broken", Field: "services.api"}
	warnItem := utils.ValidationIssue{Code: "W_X", Message: "soft", Field: "services.web"}

	cases := []struct {
		name   string
		errs   []utils.ValidationIssue
		warns  []utils.ValidationIssue
		strict bool
		want   []string
	}{
		{"clean", nil, nil, false, []string{"✓ no errors", "valid — no issues"}},
		{"errors", []utils.ValidationIssue{errItem}, nil, false,
			[]string{"✗ [E_X] broken", "(services.api)", "1 error(s), 0 warning(s)"}},
		{"warnings", nil, []utils.ValidationIssue{warnItem}, false,
			[]string{"⚠ [W_X] soft", "valid — 1 warning(s)"}},
		{"strict-warnings", nil, []utils.ValidationIssue{warnItem}, true,
			[]string{"failing under --strict"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				printValidateHuman(tc.errs, tc.warns, tc.strict)
			})
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in output, got %q", want, out)
				}
			}
		})
	}
}

func TestValidateReportJSONShape(t *testing.T) {
	// Compose with a dangling dep and a duplicate port -> two error codes.
	c := &utils.CorgiCompose{
		DatabaseServices: []utils.DatabaseService{
			{ServiceName: "db", Driver: "postgres", Port: 8080},
		},
		Services: []utils.Service{
			{ServiceName: "api", Port: 8080, Start: []string{"go run ."},
				DependsOnServices: []utils.DependsOnService{{Name: "ghost"}}},
		},
	}

	report, failed := buildReport(c, false)
	if !failed {
		t.Fatal("expected failure with errors present")
	}

	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, report)

	var got struct {
		Ok       bool                    `json:"ok"`
		Errors   []utils.ValidationIssue `json:"errors"`
		Warnings []utils.ValidationIssue `json:"warnings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}

	if got.Ok {
		t.Error("ok should be false when errors are present")
	}

	codes := map[string]bool{}
	for _, e := range got.Errors {
		codes[e.Code] = true
	}
	if !codes[utils.ErrDanglingDep] {
		t.Errorf("expected %s in errors, got %+v", utils.ErrDanglingDep, got.Errors)
	}
	if !codes[utils.ErrPortConflict] {
		t.Errorf("expected %s in errors, got %+v", utils.ErrPortConflict, got.Errors)
	}
}

func TestValidateReportCleanIsArrays(t *testing.T) {
	c := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "api", Port: 3000, Start: []string{"go run ."}},
		},
	}
	report, failed := buildReport(c, false)
	if failed {
		t.Fatal("clean compose should not fail")
	}
	if !report.Ok {
		t.Error("ok should be true for clean compose")
	}

	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, report)
	s := buf.String()
	// errors / warnings must serialize as [] not null so consumers can iterate.
	if !strings.Contains(s, `"errors": []`) || !strings.Contains(s, `"warnings": []`) {
		t.Errorf("empty errors/warnings must be [], got:\n%s", s)
	}
}

func TestValidateStrictPromotesWarnings(t *testing.T) {
	// cloneFrom without branch is a warning only.
	c := &utils.CorgiCompose{
		Services: []utils.Service{
			{ServiceName: "api", CloneFrom: "git@github.com:x/y.git", Start: []string{"go run ."}},
		},
	}

	if _, failed := buildReport(c, false); failed {
		t.Error("warnings alone should not fail without --strict")
	}
	report, failed := buildReport(c, true)
	if !failed {
		t.Error("warnings should fail under --strict")
	}
	if report.Ok {
		t.Error("ok should be false under --strict with warnings")
	}
	if len(report.Warnings) == 0 || report.Warnings[0].Code != utils.WarnNoBranch {
		t.Errorf("expected %s warning, got %+v", utils.WarnNoBranch, report.Warnings)
	}
}
