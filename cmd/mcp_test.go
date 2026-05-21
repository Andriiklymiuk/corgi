package cmd

import (
	"encoding/json"
	"reflect"
	"testing"

	"andriiklymiuk/corgi/utils"
)

const mcpComposeFixture = `name: mcp-fixture
db_services:
  pg:
    driver: postgres
    port: 5432
    user: u
    password: p
    databaseName: d
services:
  api:
    port: 3000
    start:
      - go run .
    depends_on_db:
      - name: pg
`

// mcpComposeWithError has a dangling service dependency -> one validation error.
const mcpComposeWithError = `name: mcp-bad
services:
  api:
    port: 3000
    start:
      - go run .
    depends_on_services:
      - name: ghost
`

func TestMCPValidateParity(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)

	got, err := mcpValidate(validateArgs{})
	if err != nil {
		t.Fatalf("mcpValidate: %v", err)
	}

	// Parity: same result the CLI builder produces for the same compose.
	corgi, err := loadComposeForMCP("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	errs, warns := utils.ValidateCompose(corgi)
	if got.Ok != (len(errs) == 0) {
		t.Errorf("ok mismatch: got %v, errs=%d", got.Ok, len(errs))
	}
	if len(got.Warnings) != len(warns) {
		t.Errorf("warnings count mismatch: got %d want %d", len(got.Warnings), len(warns))
	}
}

func TestMCPValidateReportsError(t *testing.T) {
	chdirToTempCompose(t, mcpComposeWithError)

	got, err := mcpValidate(validateArgs{})
	if err != nil {
		t.Fatalf("mcpValidate: %v", err)
	}
	if got.Ok {
		t.Error("expected ok=false for dangling dependency")
	}
	found := false
	for _, e := range got.Errors {
		if e.Code == utils.ErrDanglingDep {
			found = true
		}
	}
	if !found {
		t.Errorf("expected %s in errors, got %+v", utils.ErrDanglingDep, got.Errors)
	}
}

func TestMCPPlanParity(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)

	got, err := mcpPlan(planArgs{})
	if err != nil {
		t.Fatalf("mcpPlan: %v", err)
	}

	corgi, err := loadComposeForMCP("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := computeDryRunPlan(corgi)

	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Errorf("plan mismatch:\n got=%s\nwant=%s", gotJSON, wantJSON)
	}
}

func TestMCPSchemaMatches(t *testing.T) {
	if mcpSchema() != utils.ComposeJSONSchema() {
		t.Error("corgi_schema does not return ComposeJSONSchema()")
	}
}

func TestMCPPsParity(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)

	// Use a deterministic probe so the result doesn't depend on live ports.
	corgi, err := loadComposeForMCP("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := buildPsRows(corgi, utils.IsPortListening)

	got, err := mcpPs(validateArgs{})
	if err != nil {
		t.Fatalf("mcpPs: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("ps row count mismatch: got %d want %d", len(got), len(want))
	}
}

func TestMCPExecRequiresArgs(t *testing.T) {
	if _, err := mcpExec(execArgs{Service: "", Command: "x"}); err == nil {
		t.Error("expected error for empty service")
	}
	if _, err := mcpExec(execArgs{Service: "x", Command: ""}); err == nil {
		t.Error("expected error for empty command")
	}
}

func TestMCPLogsRequiresService(t *testing.T) {
	if _, err := mcpLogs(logsArgs{Service: ""}); err == nil {
		t.Error("expected error for empty service")
	}
}
