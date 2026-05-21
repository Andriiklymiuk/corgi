# Corgi Agent Usability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make corgi safe and useful for AI agents — never hang on prompts, emit machine-readable JSON from every read command, and let agents discover the compose schema and scaffold services non-interactively.

**Architecture:** Three new primitives in `utils/` (`IsTTY`, `NonInteractive`/`DetectMode`, `JSONOutput` + JSON helpers) registered via two global persistent flags (`--json`, `--interactive`). Every behavior change rides on these primitives. Humans at a real TTY see identical behavior; agents (no TTY or agent/CI env) get auto non-interactive + parseable output.

**Tech Stack:** Go 1.25, cobra (commands/flags), promptui (prompts being guarded), standard `encoding/json`, table tests via `go test ./...`.

**Design doc:** `docs/plans/2026-05-21-corgi-agent-usability-design.md`

**Conventions in this repo:**
- Tests live next to source: `cmd/foo_test.go`, `utils/foo_test.go`. Table-driven, `cimode_test.go` is the model.
- Run all tests: `go test ./...`. Single: `go test ./utils/ -run TestName -v`.
- Build: `go build -o /tmp/corgi .` then exercise the binary.
- Global flags registered in `cmd/root.go` `init()` via `rootCmd.PersistentFlags()`.
- `utils.CIMode` + `utils.DetectCIMode()` already exist in `utils/cimode.go`. Do NOT delete `CIMode`; extend alongside it.
- `isStdoutTTY()` already exists at `cmd/status.go` (~line 371) — promote, don't duplicate.

---

## Phase 1 — Foundation primitives

### Task 1: `utils.IsTTY()`

**Files:**
- Create: `utils/tty.go`
- Test: `utils/tty_test.go`
- Modify later: `cmd/status.go` (replace local `isStdoutTTY`)

**Step 1: Write the failing test**

```go
package utils

import (
	"os"
	"testing"
)

func TestIsTTYWithPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	// A pipe is not a character device, so it must report false.
	if fileIsTTY(r) {
		t.Errorf("pipe reported as TTY, want false")
	}
}
```

**Step 2: Run test, verify it fails**

Run: `go test ./utils/ -run TestIsTTYWithPipe -v`
Expected: FAIL — `undefined: fileIsTTY`.

**Step 3: Write minimal implementation**

```go
package utils

import "os"

// fileIsTTY reports whether f is a character device (an interactive terminal).
func fileIsTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// IsTTY reports whether stdout is attached to an interactive terminal.
func IsTTY() bool {
	return fileIsTTY(os.Stdout)
}

// StdinIsTTY reports whether stdin is attached to an interactive terminal.
func StdinIsTTY() bool {
	return fileIsTTY(os.Stdin)
}
```

**Step 4: Run test, verify it passes**

Run: `go test ./utils/ -run TestIsTTY -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add utils/tty.go utils/tty_test.go
git commit -m "feat: add utils.IsTTY terminal detection"
```

---

### Task 2: `NonInteractive` + `DetectMode`

**Files:**
- Modify: `utils/cimode.go`
- Modify: `utils/cimode_test.go`

**Step 1: Write the failing test** (append to `utils/cimode_test.go`)

```go
func TestDetectModeNonInteractive(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantNI  bool
	}{
		{"agent env CLAUDECODE", map[string]string{"CLAUDECODE": "1"}, true},
		{"ci env", map[string]string{"CI": "true"}, true},
		{"clean env", map[string]string{}, false}, // note: under `go test` stdout is a pipe, so see Step 3
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range append(ciEnvVars, agentEnvVars...) {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			NonInteractive = false
			CIMode = false
			detectFromEnv() // env-only check, TTY-independent, see Step 3
			if NonInteractive != tc.wantNI {
				t.Errorf("NonInteractive = %v, want %v", NonInteractive, tc.wantNI)
			}
		})
	}
}
```

**Step 2: Run test, verify it fails**

Run: `go test ./utils/ -run TestDetectModeNonInteractive -v`
Expected: FAIL — `undefined: agentEnvVars`, `undefined: detectFromEnv`.

**Step 3: Write minimal implementation** (add to `utils/cimode.go`)

Split env detection (testable, deterministic) from TTY detection (depends on real fds). `DetectMode` combines both; `detectFromEnv` is the unit-testable core.

```go
// NonInteractive is true when corgi must not open interactive prompts:
// a CI or agent environment, or stdin/stdout is not a terminal.
// When true, prompts are skipped and commands that need interactive input
// error out instead of blocking.
var NonInteractive bool

// agentEnvVars mark an AI agent driving corgi programmatically.
var agentEnvVars = []string{
	"CLAUDECODE",
	"CLAUDE_CODE",
	"ANTHROPIC_AGENT",
}

func anyEnvSet(keys []string) bool {
	for _, k := range keys {
		v := os.Getenv(k)
		if v == "" || v == "false" || v == "0" {
			continue
		}
		return true
	}
	return false
}

// detectFromEnv sets CIMode and the env-driven part of NonInteractive.
func detectFromEnv() {
	if anyEnvSet(ciEnvVars) {
		CIMode = true
		NonInteractive = true
	}
	if anyEnvSet(agentEnvVars) {
		NonInteractive = true
	}
}

// DetectMode auto-detects CI and non-interactive mode from environment and TTY.
func DetectMode() {
	detectFromEnv()
	if !IsTTY() || !StdinIsTTY() {
		NonInteractive = true
	}
}

// SetInteractive forces interactive mode on (used by the --interactive flag).
func SetInteractive() {
	NonInteractive = false
}
```

Keep the existing `DetectCIMode` as a thin alias so nothing breaks:

```go
// DetectCIMode is retained for compatibility; prefer DetectMode.
func DetectCIMode() { detectFromEnv() }
```

**Step 4: Run test, verify it passes**

Run: `go test ./utils/ -run TestDetectMode -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add utils/cimode.go utils/cimode_test.go
git commit -m "feat: add NonInteractive mode detection (CI/agent/no-TTY)"
```

---

### Task 3: JSON output helpers

**Files:**
- Create: `utils/jsonout.go`
- Test: `utils/jsonout_test.go`

**Step 1: Write the failing test**

```go
package utils

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPrintJSONTo(t *testing.T) {
	var buf bytes.Buffer
	PrintJSONTo(&buf, map[string]int{"a": 1})
	var got map[string]int
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if got["a"] != 1 {
		t.Errorf("got %v, want a=1", got)
	}
}

func TestJSONErrorShape(t *testing.T) {
	var buf bytes.Buffer
	WriteJSONError(&buf, "PORT_BUSY", "port 5432 in use")
	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Error.Code != "PORT_BUSY" || got.Error.Message != "port 5432 in use" {
		t.Errorf("got %+v", got.Error)
	}
}
```

**Step 2: Run test, verify it fails**

Run: `go test ./utils/ -run 'TestPrintJSONTo|TestJSONErrorShape' -v`
Expected: FAIL — undefined functions.

**Step 3: Write minimal implementation**

```go
package utils

import (
	"encoding/json"
	"io"
	"os"
)

// JSONOutput is true when the global --json flag is set.
var JSONOutput bool

// PrintJSONTo writes v as indented JSON to w.
func PrintJSONTo(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// PrintJSON writes v as indented JSON to stdout.
func PrintJSON(v any) { PrintJSONTo(os.Stdout, v) }

type jsonErr struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// WriteJSONError writes a structured error object to w.
func WriteJSONError(w io.Writer, code, message string) {
	var e jsonErr
	e.Error.Code = code
	e.Error.Message = message
	PrintJSONTo(w, e)
}

// JSONError prints a structured error to stdout (used on error paths when --json is set).
func JSONError(code, message string) { WriteJSONError(os.Stdout, code, message) }
```

**Step 4: Run test, verify it passes**

Run: `go test ./utils/ -run 'TestPrintJSONTo|TestJSONErrorShape' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add utils/jsonout.go utils/jsonout_test.go
git commit -m "feat: add JSON output + structured error helpers"
```

---

### Task 4: Register global `--json` / `--interactive`; wire `DetectMode` in main

**Files:**
- Modify: `cmd/root.go` (`init()` adds two persistent flags; `PersistentPreRun` reads them)
- Modify: `main.go` (call `utils.DetectMode()` instead of `DetectCIMode()`)
- Modify: `cmd/status.go` (delete local `isStdoutTTY`, use `utils.IsTTY`)
- Test: `cmd/root_flags_test.go` (new)

**Step 1: Write the failing test**

```go
package cmd

import (
	"testing"

	"andriiklymiuk/corgi/utils"
)

func TestGlobalJSONFlagSetsUtil(t *testing.T) {
	utils.JSONOutput = false
	rootCmd.SetArgs([]string{"--json", "docs"})
	// PersistentPreRun should flip the util based on the flag.
	applyGlobalFlags(rootCmd, []string{"--json"})
	if !utils.JSONOutput {
		t.Errorf("JSONOutput not set by --json")
	}
}
```

(Adjust to a small testable helper `applyGlobalFlags(cmd, flags)` that reads the flags off `rootCmd` and sets the utils. Keep it pure so it is unit-testable.)

**Step 2: Run test, verify it fails**

Run: `go test ./cmd/ -run TestGlobalJSONFlag -v`
Expected: FAIL — `undefined: applyGlobalFlags`.

**Step 3: Write minimal implementation**

In `cmd/root.go` `init()` add:

```go
rootCmd.PersistentFlags().Bool("json", false, "Emit machine-readable JSON output")
rootCmd.PersistentFlags().Bool("interactive", false, "Force interactive prompts even when no TTY/agent detected")
```

Add a `PersistentPreRun` (or augment existing) on `rootCmd`:

```go
func applyGlobalFlags(cmd *cobra.Command, _ []string) {
	if j, _ := cmd.Flags().GetBool("json"); j {
		utils.JSONOutput = true
	}
	if i, _ := cmd.Flags().GetBool("interactive"); i {
		utils.SetInteractive()
	}
}
```

Wire it: `rootCmd.PersistentPreRun = applyGlobalFlags`. In `main.go` replace `utils.DetectCIMode()` with `utils.DetectMode()`. In `cmd/status.go` delete `isStdoutTTY` and replace calls with `utils.IsTTY()`.

**Step 4: Run, verify pass + full suite green**

Run: `go test ./... ` then `go build -o /tmp/corgi . && /tmp/corgi --json docs --json-schema || true`
Expected: tests PASS; build OK.

**Step 5: Commit**

```bash
git add cmd/root.go main.go cmd/status.go cmd/root_flags_test.go
git commit -m "feat: register global --json/--interactive, wire DetectMode"
```

---

## Phase 2 — Hang-killers

### Task 5: Skip continuation prompt when non-interactive

**Files:**
- Modify: `main.go` (the `runCli` loop, lines ~16-42)
- Test: `main_test.go` (new) — extract decision into a testable function.

**Step 1: Write the failing test**

```go
package main

import "testing"

func TestShouldContinuePromptSkippedNonInteractive(t *testing.T) {
	if shouldPromptToContinue(true /*nonInteractive*/) {
		t.Errorf("must not prompt to continue when non-interactive")
	}
	if !shouldPromptToContinue(false) {
		t.Errorf("interactive mode should allow the prompt")
	}
}
```

**Step 2: Run, verify it fails**

Run: `go test . -run TestShouldContinuePrompt -v`
Expected: FAIL — `undefined: shouldPromptToContinue`.

**Step 3: Implement**

```go
// shouldPromptToContinue reports whether the post-command "continue?" prompt
// should run. Never prompt when non-interactive (agent/CI/no TTY).
func shouldPromptToContinue(nonInteractive bool) bool {
	return !nonInteractive
}
```

In `runCli`, guard the promptui block:

```go
if !shouldPromptToContinue(utils.NonInteractive) {
	showFinalMessage()
	return
}
```

**Step 4: Run, verify pass**

Run: `go test . -run TestShouldContinuePrompt -v`
Expected: PASS. Also: `CLAUDECODE=1 /tmp/corgi db --help` returns immediately (no hang).

**Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "fix: skip continuation prompt in non-interactive mode"
```

---

### Task 6: Guard interactive pickers (logs, db shell)

**Files:**
- Modify: `cmd/logs.go` (picker at ~64-80)
- Modify: `cmd/db.go` (`db shell` picker at ~70)
- Test: `cmd/logs_test.go`, `cmd/db_test.go`

**Step 1: Write the failing test** (logs)

```go
func TestLogsRequiresServiceNonInteractive(t *testing.T) {
	err := requireServiceForLogs("", true /*nonInteractive*/, []string{"api", "worker"})
	if err == nil {
		t.Fatal("expected error when no --service under non-interactive")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("error should list available services, got %q", err.Error())
	}
	if requireServiceForLogs("api", true, []string{"api"}) != nil {
		t.Error("explicit service should pass")
	}
}
```

**Step 2: Run, verify fails**

Run: `go test ./cmd/ -run TestLogsRequiresService -v`
Expected: FAIL — undefined.

**Step 3: Implement**

```go
// requireServiceForLogs errors (instead of opening a picker) when no service
// is given under non-interactive mode, naming the valid services.
func requireServiceForLogs(service string, nonInteractive bool, available []string) error {
	if service != "" || !nonInteractive {
		return nil
	}
	return fmt.Errorf("no terminal for the service picker; pass --service <name> (available: %s)",
		strings.Join(available, ", "))
}
```

Call it before the picker in `logs.go`; on error, if `utils.JSONOutput` use `utils.JSONError("INPUT_REQUIRED", err.Error())`, else print to stderr; `os.Exit(2)`. Apply the same pattern for `db shell` with `requireServiceForDBShell`.

**Step 4: Run, verify pass**

Run: `go test ./cmd/ -run 'TestLogsRequiresService|TestDBShellRequiresService' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/logs.go cmd/db.go cmd/logs_test.go cmd/db_test.go
git commit -m "fix: error instead of prompting for logs/db-shell picker when non-interactive"
```

---

## Phase 3 — JSON output rollout

> For each command: define a result struct, render JSON when `utils.JSONOutput`, else keep existing human output. One command per task. Pattern below repeats; example shown for `doctor`, repeat for `list`, `config`, `docs`, `run` summary.

### Task 7: `doctor --json`

**Files:**
- Modify: `cmd/doctor.go`
- Test: `cmd/doctor_test.go`

**Step 1: Failing test**

```go
func TestDoctorJSONResult(t *testing.T) {
	res := doctorResult{
		Checks: []doctorCheck{{Name: "docker", OK: true}, {Name: "port:5432", OK: false, Detail: "in use"}},
	}
	res.computeOK()
	if res.OK {
		t.Error("overall OK must be false when any check fails")
	}
}
```

**Step 2: Run, verify fails** — `go test ./cmd/ -run TestDoctorJSON -v` → FAIL.

**Step 3: Implement**

```go
type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}
type doctorResult struct {
	OK     bool          `json:"ok"`
	Checks []doctorCheck `json:"checks"`
}
func (r *doctorResult) computeOK() {
	r.OK = true
	for _, c := range r.Checks {
		if !c.OK {
			r.OK = false
			return
		}
	}
}
```

In the command body, collect checks into `doctorResult`; if `utils.JSONOutput` → `utils.PrintJSON(res)`, else existing text. Exit 1 when `!res.OK`.

**Step 4: Run, verify pass** + `/tmp/corgi --json doctor` emits a JSON object.

**Step 5: Commit**

```bash
git add cmd/doctor.go cmd/doctor_test.go
git commit -m "feat: doctor --json output"
```

### Task 8: `list --json`
Same pattern. Struct `[]listEntry{Path, Exists}`. Suppress spinner when `utils.JSONOutput || utils.CIMode`. Test the entry serialization. Commit `feat: list --json output`.

### Task 9: `config --json`
Emit the loaded `~/.corgi/config.yml` (version, notifications) as JSON. Test marshal shape. Commit `feat: config --json output`.

### Task 10: `run` summary `--json`
At end of a `--runOnce` run, emit `{started:[...], failed:[...]}` summary when `utils.JSONOutput`. Keep streaming logs as-is (human) — JSON summary printed last to stdout. Test the summary struct. Commit `feat: run --json summary`.

---

## Phase 4 — `corgi ps`

### Task 11: `ps` command

**Files:**
- Create: `cmd/ps.go`
- Test: `cmd/ps_test.go`

**Step 1: Failing test**

```go
func TestPsSnapshotJSON(t *testing.T) {
	rows := []psRow{{Name: "api", Kind: "service", PID: 123, Port: 8080, Status: "running"}}
	var buf bytes.Buffer
	utils.PrintJSONTo(&buf, rows)
	if !strings.Contains(buf.String(), `"name": "api"`) {
		t.Errorf("missing name field: %s", buf.String())
	}
}
```

**Step 2: Run, verify fails** — `go test ./cmd/ -run TestPsSnapshot -v` → FAIL (undefined `psRow`).

**Step 3: Implement**

```go
type psRow struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"` // "service" | "db_service"
	PID    int    `json:"pid,omitempty"`
	Port   int    `json:"port,omitempty"`
	Status string `json:"status"` // running | stopped | unknown
	URL    string `json:"url,omitempty"`
}
```

Build rows from `utils.StoredProcesses` (running services + pids) and db container/port config from the loaded compose file. Register `psCmd` in `init()`. Default: aligned text table. `--json` (global): `utils.PrintJSON(rows)`.

**Step 4: Run, verify pass** + `/tmp/corgi --json ps` after a `run`.

**Step 5: Commit**

```bash
git add cmd/ps.go cmd/ps_test.go
git commit -m "feat: add corgi ps runtime snapshot (human + --json)"
```

---

## Phase 5 — Non-interactive create/fork flag surfaces

### Task 12: `create` flag surface + guard

**Files:**
- Modify: `cmd/create.go`
- Test: `cmd/create_test.go`

**Step 1: Failing test**

```go
func TestCreateRequiresFieldsNonInteractive(t *testing.T) {
	err := validateCreateFlags(createFlags{kind: ""}, true /*nonInteractive*/)
	if err == nil {
		t.Fatal("expected error when --kind missing under non-interactive")
	}
	if validateCreateFlags(createFlags{kind: "service", name: "api"}, true) != nil {
		t.Error("complete flags should pass")
	}
}
```

**Step 2: Run, verify fails** — FAIL undefined.

**Step 3: Implement**
Add flags mirroring every prompt: `--kind` (db_service|service|required), `--name`, `--image`, `--port`, plus the lifecycle/required fields the prompts collect. Add:

```go
type createFlags struct{ kind, name, image string; port int /* ...lifecycle fields */ }

func validateCreateFlags(f createFlags, nonInteractive bool) error {
	if !nonInteractive {
		return nil // prompts will fill gaps
	}
	if f.kind == "" || f.name == "" {
		return fmt.Errorf("non-interactive create needs at least --kind and --name")
	}
	return nil
}
```

Branch in the command: if `utils.NonInteractive` use flags + `validateCreateFlags` (error+exit 2 on failure, JSONError when `--json`); else existing promptui flow.

**Step 4: Run, verify pass** + `CLAUDECODE=1 /tmp/corgi create --kind service --name api ...` writes/updates compose without prompting.

**Step 5: Commit** `feat: non-interactive corgi create via flags`.

### Task 13: `fork` flag surface + guard
Same pattern. Flags already partly exist (`--all`, `--private`, `--useSameRepoName`, `--gitProvider`); add `--service` (CSV) and `--newName`. Add `validateForkFlags`. Guard `PickItemFromListPrompt` calls behind `utils.NonInteractive`. Test `validateForkFlags`. Commit `feat: non-interactive corgi fork via flags`.

---

## Phase 6 — Schema export

### Task 14: `corgi docs --json-schema`

**Files:**
- Create: `utils/schema.go` (embedded JSON Schema string + accessor)
- Create: `utils/schema_test.go`
- Modify: `cmd/docs.go` (add `--json-schema` flag)

**Step 1: Failing test**

```go
func TestComposeJSONSchemaValid(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal([]byte(ComposeJSONSchema()), &m); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if m["$schema"] == nil {
		t.Error("schema missing $schema")
	}
	props, _ := m["properties"].(map[string]any)
	if props["services"] == nil || props["db_services"] == nil {
		t.Error("schema missing top-level services/db_services")
	}
}
```

**Step 2: Run, verify fails** — FAIL undefined `ComposeJSONSchema`.

**Step 3: Implement**
`utils/schema.go`: a `//go:embed corgi-compose.schema.json` string + `func ComposeJSONSchema() string`. Author `utils/corgi-compose.schema.json` (draft-07) covering top-level keys, `db_services`, `services`, `required` — fields mirrored from the structs in `utils/config.go`, with `description`/`examples` per field. Include `$id` so editors can reference it. In `cmd/docs.go` add `--json-schema` bool: when set, print `utils.ComposeJSONSchema()` and return (ignores `--generate`).

**Step 4: Run, verify pass** + `/tmp/corgi docs --json-schema | jq .` parses.

**Step 5: Commit**

```bash
git add utils/schema.go utils/schema_test.go utils/corgi-compose.schema.json cmd/docs.go
git commit -m "feat: corgi docs --json-schema exports compose JSON Schema"
```

---

## Phase 7 — Agent docs

### Task 15: `docs/agents.md` + skill pointer

**Files:**
- Create: `docs/agents.md`
- Modify: corgi skill SKILL.md (add a short "Driving corgi as an agent" section linking `docs/agents.md`)

**Step 1:** No code test. Write `docs/agents.md` covering:
- Auto non-interactive: corgi detects no-TTY / `CLAUDECODE` / CI and skips prompts; `--interactive` overrides.
- Safe recipe block (doctor → run --runOnce --json → status --ready --json → ps --json → logs --service X → clean -i all).
- `--json` examples with sample output for `status`, `doctor`, `ps`, `docs --json-schema`.
- Exit-code table (0/1/2) and the `{"error":{code,message}}` shape.
- "Needs a flag or it errors" list: `create` (--kind/--name), `fork` (--service/--all), `logs` (--service), `db shell` (service name or -e).

**Step 2: Verify** the recipes against the built binary: run each command from a non-TTY context (`CLAUDECODE=1`), confirm none hang and JSON parses.

**Step 3: Commit**

```bash
git add docs/agents.md <skill SKILL.md path>
git commit -m "docs: add agent usage guide and link from corgi skill"
```

---

## Final verification (before finishing the branch)

1. `go test ./...` — all green.
2. `go vet ./...` — clean.
3. `go build -o /tmp/corgi .` — builds.
4. Non-interactive smoke (no hangs, JSON parses):
   ```bash
   CLAUDECODE=1 /tmp/corgi --json doctor | jq .
   CLAUDECODE=1 /tmp/corgi --json ps | jq .
   CLAUDECODE=1 /tmp/corgi docs --json-schema | jq .
   CLAUDECODE=1 /tmp/corgi logs   # must error (exit 2), not hang
   echo $?
   ```
5. Human regression: in a real terminal, `corgi db` still shows banner + continuation prompt.

Then: superpowers:finishing-a-development-branch.
