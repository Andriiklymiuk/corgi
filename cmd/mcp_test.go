package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
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

func TestBearerAuth(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	cases := []struct {
		name    string
		token   string
		header  string
		want    int
		wantNxt bool
	}{
		{"valid token", "secret", "Bearer secret", http.StatusOK, true},
		{"wrong token", "secret", "Bearer nope", http.StatusUnauthorized, false},
		{"missing header", "secret", "", http.StatusUnauthorized, false},
		{"no-auth passthrough", "", "", http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				okHandler.ServeHTTP(w, r)
			})
			h := bearerAuth(tc.token, next)
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
			if called != tc.wantNxt {
				t.Errorf("next called = %v, want %v", called, tc.wantNxt)
			}
		})
	}
}

func TestGenerateMCPToken(t *testing.T) {
	a := generateMCPToken()
	b := generateMCPToken()
	if a == "" {
		t.Fatal("token is empty")
	}
	if !strings.HasPrefix(a, "corgi_mcp_") {
		t.Errorf("missing corgi_mcp_ prefix: %q", a)
	}
	if a == b {
		t.Error("two tokens should differ")
	}
}

func TestBuildMCPTunnelConfig(t *testing.T) {
	// Quick tunnel: no hostname/name => nil NamedConfig.
	_, named, err := buildMCPTunnelConfig("cloudflared", "", "")
	if err != nil {
		t.Fatalf("quick: %v", err)
	}
	if named != nil {
		t.Errorf("expected nil named config for quick tunnel, got %+v", named)
	}

	// Unknown provider errors.
	if _, _, err := buildMCPTunnelConfig("bogus", "", ""); err == nil {
		t.Error("expected error for unknown provider")
	}

	// ${VAR} expansion in hostname.
	t.Setenv("MCP_TEST_HOST", "mcp.example.com")
	_, named, err = buildMCPTunnelConfig("cloudflared", "${MCP_TEST_HOST}", "my-mcp")
	if err != nil {
		t.Fatalf("named: %v", err)
	}
	if named == nil || named.Hostname != "mcp.example.com" || named.Name != "my-mcp" {
		t.Errorf("expected expanded named config, got %+v", named)
	}

	// Missing var errors.
	if _, _, err := buildMCPTunnelConfig("cloudflared", "${MCP_TEST_MISSING}", ""); err == nil {
		t.Error("expected error for missing env var")
	}
}

func TestResolveMCPToken(t *testing.T) {
	// Plain --http: no token, no tunnel => no-auth (NON-BREAKING).
	if got := resolveMCPToken(mcpHTTPOpts{}); got != "" {
		t.Errorf("plain http should stay no-auth, got %q", got)
	}
	// Explicit token honored.
	if got := resolveMCPToken(mcpHTTPOpts{token: "abc"}); got != "abc" {
		t.Errorf("explicit token = %q, want abc", got)
	}
	// Tunnel without token auto-generates.
	if got := resolveMCPToken(mcpHTTPOpts{tunnel: true}); !strings.HasPrefix(got, "corgi_mcp_") {
		t.Errorf("tunnel should auto-generate token, got %q", got)
	}
	// Insecure disables auth even with tunnel.
	if got := resolveMCPToken(mcpHTTPOpts{tunnel: true, insecure: true, token: "abc"}); got != "" {
		t.Errorf("insecure should disable auth, got %q", got)
	}
}

func TestMCPAddrPort(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want int
	}{
		{":8765", 8765},
		{"127.0.0.1:8765", 8765},
	} {
		got, err := mcpAddrPort(tc.addr)
		if err != nil || got != tc.want {
			t.Errorf("mcpAddrPort(%q) = %d, %v; want %d", tc.addr, got, err, tc.want)
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

const mcpComposeTestPass = `name: mcp-test-pass
services:
  api:
    port: 3000
    start:
      - go run .
    scripts:
      - name: test
        commands:
          - "true"
`

const mcpComposeTestFail = `name: mcp-test-fail
services:
  api:
    port: 3000
    start:
      - go run .
    scripts:
      - name: test
        commands:
          - "false"
`

func TestMCPTestPasses(t *testing.T) {
	chdirToTempCompose(t, mcpComposeTestPass)
	got, err := mcpTest(testArgs{})
	if err != nil {
		t.Fatalf("mcpTest: %v", err)
	}
	if !got.Passed {
		t.Errorf("expected passed=true, got %+v", got)
	}
	if len(got.Services) != 1 || !got.Services[0].Passed {
		t.Errorf("expected one passing service, got %+v", got.Services)
	}
}

func TestMCPTestFails(t *testing.T) {
	chdirToTempCompose(t, mcpComposeTestFail)
	got, err := mcpTest(testArgs{})
	if err != nil {
		t.Fatalf("mcpTest: %v", err)
	}
	if got.Passed {
		t.Errorf("expected passed=false, got %+v", got)
	}
}

func TestMCPDoctorShape(t *testing.T) {
	chdirToTempCompose(t, mcpComposeFixture)
	got, err := mcpDoctor(validateArgs{})
	if err != nil {
		t.Fatalf("mcpDoctor: %v", err)
	}
	if got.Checks == nil {
		t.Error("expected a checks array")
	}
	// ok is the AND of every check; just assert it is consistent with the checks.
	want := true
	for _, c := range got.Checks {
		if !c.OK {
			want = false
		}
	}
	if got.OK != want {
		t.Errorf("ok=%v inconsistent with checks %+v", got.OK, got.Checks)
	}
}

func TestMCPRestartMissingCompose(t *testing.T) {
	chdirToTempCompose(t, "name: x\n")
	// Point at a path that does not exist -> composeLoadError.
	if _, err := mcpRestart(restartArgs{ComposePath: "/no/such/corgi-compose.yml"}); err == nil {
		t.Error("expected error for missing compose")
	}
}

func TestMCPDBQueryRequiresArgs(t *testing.T) {
	if _, err := mcpDBQuery(dbQueryArgs{Service: "", Query: "SELECT 1"}); err == nil {
		t.Error("expected error for empty service")
	}
	if _, err := mcpDBQuery(dbQueryArgs{Service: "pg", Query: ""}); err == nil {
		t.Error("expected error for empty query")
	}
}

func TestMCPDriversResource(t *testing.T) {
	if len(utils.KnownDrivers) == 0 {
		t.Fatal("expected non-empty KnownDrivers")
	}
	found := false
	for _, d := range utils.KnownDrivers {
		if d == "postgres" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected postgres in KnownDrivers, got %v", utils.KnownDrivers)
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
