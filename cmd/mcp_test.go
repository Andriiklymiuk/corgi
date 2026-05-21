package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
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

// mcpComposeWithProfiles has services tagged with profiles and a db pulled in
// transitively by the backend service.
const mcpComposeWithProfiles = `name: mcp-profiles
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
    profiles:
      - backend
    start:
      - go run .
    depends_on_db:
      - name: pg
  web:
    port: 3001
    profiles:
      - frontend
    start:
      - npm start
`

func TestMCPPlanProfileParity(t *testing.T) {
	chdirToTempCompose(t, mcpComposeWithProfiles)

	const profile = "backend"

	got, err := mcpPlan(planArgs{Profile: profile})
	if err != nil {
		t.Fatalf("mcpPlan: %v", err)
	}

	// Parity: same plan the CLI's `run --profile backend --dry-run` produces,
	// i.e. computeDryRunPlan over the profile-narrowed compose.
	corgi, err := loadComposeForMCP("")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	filterByProfile(corgi, profile)
	want := computeDryRunPlan(corgi)

	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Errorf("profile plan mismatch:\n got=%s\nwant=%s", gotJSON, wantJSON)
	}

	// Sanity: the backend profile must narrow out the frontend-only service.
	for _, s := range corgi.Services {
		if s.ServiceName == "web" {
			t.Errorf("expected web (frontend) to be excluded by backend profile, plan covered %d services", len(corgi.Services))
		}
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

// TestMCPWithStdoutToStderr verifies the stdout swap keeps incidental prints off
// the real stdout (the JSON-RPC channel) and restores os.Stdout afterward.
func TestMCPWithStdoutToStderr(t *testing.T) {
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	withStdoutToStderr(func() {
		fmt.Println("SHOULD_NOT_APPEAR")
	})

	// (b) os.Stdout must be restored to what it was before the swap.
	if os.Stdout != w {
		t.Errorf("os.Stdout not restored: got %v want %v", os.Stdout, w)
	}
	os.Stdout = orig

	// (a) nothing should have reached the captured real stdout.
	w.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected nothing on real stdout during swap, got %q", string(data))
	}
}
