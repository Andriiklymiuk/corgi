# Corgi Lifecycle (detach / state / stop) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let corgi start services detached, persist what is running to a state file, and stop/restart them from any later invocation — making `ps`/`status` report real state. No daemon.

**Architecture:** A `corgi_services/.state.json` file (atomic write) records service PIDs + db containers with per-entry timestamps. `run --detach` spawns detached process groups and writes it; a pure `reconcile()` re-checks liveness on read; `stop`/`restart`/`ps`/`status` use it. Foreground `run` is untouched; `ps`/`status` fall back to today's port-probe when no state file exists.

**Tech Stack:** Go 1.25, cobra, standard `os`/`os/exec`/`syscall`/`encoding/json`. Reuse existing helpers: `utils.SetProcessGroup` (proc_default.go/proc_windows.go), `utils.addProcess`/`KillAllStoredProcesses`, log writers (`setupLogWriters`/`getLogWriter`/`CloseAllLogWriters`), `cleanup(corgi)` (afterStart), `runDatabaseServices`, db `make down`.

**Design doc:** `docs/plans/2026-05-21-corgi-lifecycle-detach-design.md`

**Conventions (carry over from the agent-usability work):**
- Tests next to source; table-driven; `go test ./...`.
- `--json` stdout must stay pure JSON: human/log lines via `utils.Info`/`Infof`; payload via `utils.PrintJSON`; errors via `utils.JSONError(code, msg)`.
- Non-interactive auto-detected (`utils.NonInteractive`); never add a prompt that can hang an agent.
- **NO `Co-Authored-By` / AI-attribution trailer in commit messages.** Plain messages only.
- Exit codes: 0 ok, 1 operational failure, 2 usage/missing-input.
- Reuse existing logic; do not duplicate teardown/spawn code.

**No-regression rule (applies to every task):** after each task run the FULL suite `go test ./...` and `go vet ./...`; foreground `corgi run`, `ps`, `status` must behave exactly as before when no `.state.json` is present.

---

## Phase 1 — State model

### Task 1: State types + atomic read/write

**Files:**
- Create: `utils/runstate.go`
- Test: `utils/runstate_test.go`

**Step 1: Write failing test** (`utils/runstate_test.go`):

```go
package utils

import (
	"path/filepath"
	"testing"
)

func TestRunStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".state.json")
	in := RunState{
		ComposePath: "/x/corgi-compose.yml",
		Services: []RunStateEntry{
			{Name: "api", Kind: "service", PID: 1234, Port: 8080, Status: "running"},
		},
	}
	if err := WriteRunState(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadRunState(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got.Services) != 1 || got.Services[0].Name != "api" || got.Services[0].PID != 1234 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestReadRunStateMissingFile(t *testing.T) {
	_, err := ReadRunState(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Error("expected error for missing state file")
	}
}
```

**Step 2: Run, verify fail**

Run: `go test ./utils/ -run TestRunState -v`
Expected: FAIL — undefined `RunState`/`WriteRunState`/`ReadRunState`.

**Step 3: Implement** (`utils/runstate.go`):

```go
package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type RunStateEntry struct {
	Name            string    `json:"name"`
	Kind            string    `json:"kind"` // "service" | "db_service"
	PID             int       `json:"pid,omitempty"`
	PGID            int       `json:"pgid,omitempty"`
	Port            int       `json:"port,omitempty"`
	Container       string    `json:"container,omitempty"`
	Command         string    `json:"command,omitempty"`
	LogFile         string    `json:"logFile,omitempty"`
	Status          string    `json:"status"` // running | stopped | crashed | unknown
	StartedAt       time.Time `json:"startedAt,omitempty"`
	StatusChangedAt time.Time `json:"statusChangedAt,omitempty"`
	ExitCode        *int      `json:"exitCode,omitempty"`
}

type RunState struct {
	ComposePath string          `json:"composePath"`
	StartedAt   time.Time       `json:"startedAt,omitempty"`
	UpdatedAt   time.Time       `json:"updatedAt,omitempty"`
	Services    []RunStateEntry `json:"services"`
	DBServices  []RunStateEntry `json:"dbServices"`
}

// RunStatePath returns the state-file path for the project holding composePath.
func RunStatePath(composeDir string) string {
	return filepath.Join(composeDir, "corgi_services", ".state.json")
}

// WriteRunState writes s atomically (temp + rename).
func WriteRunState(path string, s RunState) error {
	s.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadRunState reads and parses the state file.
func ReadRunState(path string) (RunState, error) {
	var s RunState
	data, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(data, &s)
	return s, err
}
```

**Step 4: Run, verify pass**

Run: `go test ./utils/ -run TestRunState -v` then `go test ./...`
Expected: PASS; full suite green.

**Step 5: Commit**

```bash
git add utils/runstate.go utils/runstate_test.go
git commit -m "feat: add run-state model with atomic read/write"
```

---

### Task 2: Pure liveness `reconcile`

**Files:**
- Modify: `utils/runstate.go`
- Test: `utils/runstate_test.go`

**Step 1: Write failing test** (append):

```go
func TestReconcileMarksCrashed(t *testing.T) {
	s := RunState{Services: []RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Status: "running"},
		{Name: "web", Kind: "service", PID: 2, Status: "running"},
	}}
	alive := func(pid int, command string) bool { return pid == 1 } // web (pid 2) is dead
	container := func(name string) string { return "" }
	out := ReconcileRunState(s, alive, container)
	if out.Services[0].Status != "running" {
		t.Errorf("api should stay running, got %q", out.Services[0].Status)
	}
	if out.Services[1].Status != "crashed" {
		t.Errorf("web should be crashed, got %q", out.Services[1].Status)
	}
	if out.Services[1].StatusChangedAt.IsZero() {
		t.Error("statusChangedAt should be set when status flips")
	}
}

func TestReconcileStableWhenUnchanged(t *testing.T) {
	t0 := time.Now().Add(-time.Hour).UTC()
	s := RunState{Services: []RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Status: "running", StatusChangedAt: t0},
	}}
	out := ReconcileRunState(s, func(int, string) bool { return true }, func(string) string { return "" })
	if !out.Services[0].StatusChangedAt.Equal(t0) {
		t.Error("statusChangedAt must not change when status is unchanged")
	}
}
```

**Step 2: Run, verify fail** — `go test ./utils/ -run TestReconcile -v` → FAIL undefined.

**Step 3: Implement** (add to `utils/runstate.go`):

```go
// ReconcileRunState re-checks liveness and flips statuses. pidAlive verifies a
// service pid still belongs to our process; containerState maps a db container
// name to "running"/"stopped"/"" (unknown). statusChangedAt updates only on a
// real status change.
func ReconcileRunState(
	s RunState,
	pidAlive func(pid int, command string) bool,
	containerState func(name string) string,
) RunState {
	now := time.Now().UTC()
	for i := range s.Services {
		e := &s.Services[i]
		newStatus := "running"
		if !pidAlive(e.PID, e.Command) {
			newStatus = "crashed"
		}
		if newStatus != e.Status {
			e.Status = newStatus
			e.StatusChangedAt = now
		}
	}
	for i := range s.DBServices {
		e := &s.DBServices[i]
		newStatus := e.Status
		switch containerState(e.Container) {
		case "running":
			newStatus = "running"
		case "stopped":
			newStatus = "stopped"
		}
		if newStatus != e.Status {
			e.Status = newStatus
			e.StatusChangedAt = now
		}
	}
	return s
}
```

**Step 4: Run, verify pass** — `go test ./utils/ -run TestReconcile -v`, then `go test ./...`.

**Step 5: Commit**

```bash
git add utils/runstate.go utils/runstate_test.go
git commit -m "feat: add pure run-state liveness reconcile"
```

---

### Task 3: Real liveness probes (pid + container)

**Files:**
- Create: `utils/liveness.go`
- Test: `utils/liveness_test.go`

**Step 1: Failing test** — verify a live pid (the test process itself) reads alive and an almost-certainly-dead pid reads not-alive:

```go
package utils

import (
	"os"
	"testing"
)

func TestPidAliveSelf(t *testing.T) {
	if !PidAlive(os.Getpid(), "") {
		t.Error("current process should be alive")
	}
}

func TestPidAliveDead(t *testing.T) {
	// PID 0 is not a normal user process; signal 0 should fail for it.
	if PidAlive(0, "") {
		t.Error("pid 0 should not report alive")
	}
}
```

**Step 2: Run, verify fail** — FAIL undefined `PidAlive`.

**Step 3: Implement** (`utils/liveness.go`). Use `syscall.Kill(pid, 0)` on unix (guard build tags consistent with proc_default/proc_windows). Provide a Windows fallback. Keep `command` param for future cmdline verification; v1 may ignore it (document) or do a best-effort check.

```go
//go:build !windows

package utils

import "syscall"

// PidAlive reports whether pid is a live process we can signal.
func PidAlive(pid int, command string) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
```

Add `utils/liveness_windows.go` with a `//go:build windows` stub returning a best-effort check (e.g. `os.FindProcess` + a tasklist query, or conservatively `true`/`false` — document the limitation). Also add `ContainerRunning(name string) string` (reuse existing docker helpers if present — check `utils/docker.go` for a container-state function before writing a new one; prefer reuse).

**Step 4: Run, verify pass** — targeted + `go test ./...` + `go vet ./...`.

**Step 5: Commit**

```bash
git add utils/liveness.go utils/liveness_windows.go utils/liveness_test.go
git commit -m "feat: add pid + container liveness probes"
```

---

## Phase 2 — `run --detach`

### Task 4: Detached spawn + state write

**Files:**
- Modify: `cmd/run.go`
- Test: `cmd/run_test.go`

**REQUIRED reading before coding:** `cmd/run.go` `runService`/`startServiceProcess`/`runDatabaseServices`; `utils/execute.go` `runManaged`/`SetProcessGroup`/`addProcess`/`getLogWriter`; how a service start command is currently built and run (it currently `cmd.Start()` then `cmd.Wait()` — detach must NOT wait).

**Step 1: Failing test** — a pure builder that turns started results into `utils.RunState`:

```go
func TestBuildDetachState(t *testing.T) {
	started := []detachedProc{{name: "api", port: 8080, pid: 4321, command: "npm start", logFile: "x.log"}}
	dbs := []utils.RunStateEntry{{Name: "pg", Kind: "db_service", Container: "corgi_pg", Port: 5432, Status: "running"}}
	st := buildDetachState("/x/corgi-compose.yml", started, dbs)
	if st.Services[0].Name != "api" || st.Services[0].PID != 4321 || st.Services[0].Status != "running" {
		t.Errorf("bad service entry: %+v", st.Services[0])
	}
	if st.Services[0].StartedAt.IsZero() || st.Services[0].StatusChangedAt.IsZero() {
		t.Error("timestamps should be set")
	}
	if st.DBServices[0].Container != "corgi_pg" {
		t.Errorf("db entry not carried: %+v", st.DBServices)
	}
}
```

**Step 2: Run, verify fail** — FAIL undefined.

**Step 3: Implement**
- Add `--detach`/`-d` and `--force` flags to `runCmd`.
- Define `type detachedProc struct{ name, command, logFile string; port, pid, pgid int }` and `buildDetachState(composePath string, procs []detachedProc, dbs []utils.RunStateEntry) utils.RunState` (sets Status "running", StartedAt/StatusChangedAt = now).
- In `runRun`, when `--detach`: run the same startup up through db start + env gen + beforeStart, force `--logs` on (call `setupLogWriters`), then for each service spawn the start command **detached**: build the `exec.Cmd`, set `cmd.Stdout/Stderr` to the service log writer, `utils.SetProcessGroup(cmd)`, `cmd.Start()` (NO `cmd.Wait()`), capture `cmd.Process.Pid`. Collect `detachedProc`. Build db entries from the db_services that were started (Container name = however corgi names them — find the real naming; check db Makefile/compose generation). Write state via `utils.WriteRunState(utils.RunStatePath(composeDir), st)`. Print JSON startup summary (reuse `buildRunSummary` shape or the new state) and return 0.
- Guard `ALREADY_RUNNING`: before spawning, if state file exists and reconciled has any `running` service, error (`utils.JSONError("ALREADY_RUNNING", ...)` / stderr) exit 1, unless `--force` (then run stop logic first — may depend on Task 6; for now `--force` can delete the stale state file and proceed, with a TODO to call full stop once Task 6 lands).
- Foreground path (no `--detach`) unchanged.

If spawning detached cleanly requires refactoring the existing `runManaged` (which waits), prefer adding a sibling `StartDetached(...)` in `utils/execute.go` that Starts without Waiting and returns the `*os.Process` — do NOT change `runManaged`'s foreground semantics. Report the approach taken.

**Step 4: Run, verify pass** — targeted test; `go test ./...`; `go vet ./...`; `go build -o /tmp/corgi .`. Live sanity in a temp dir with a service whose start is `sleep 60`:
`/tmp/corgi --json run --detach` → exits 0, prints JSON, writes `corgi_services/.state.json` with the pid; `ps aux | grep sleep` shows it alive after corgi exits. Kill the sleep manually to clean up.

**Step 5: Commit**

```bash
git add cmd/run.go cmd/run_test.go utils/execute.go
git commit -m "feat: corgi run --detach with persisted run-state"
```

---

## Phase 3 — wire ps / status

### Task 5: `ps`/`status` read + reconcile state

**Files:**
- Modify: `cmd/ps.go` (and `cmd/status.go` if low-risk)
- Test: `cmd/ps_test.go`

**Step 1: Failing test** — `ps` rows derive from state when present:

```go
func TestPsRowsFromState(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Port: 8080, Status: "running"},
	}}
	rows := psRowsFromState(st)
	if rows[0].Name != "api" || rows[0].Status != "running" || rows[0].Port != 8080 {
		t.Errorf("bad row from state: %+v", rows[0])
	}
}
```

**Step 2: Run, verify fail** — FAIL undefined `psRowsFromState`.

**Step 3: Implement**
- Add `psRowsFromState(st utils.RunState) []psRow` mapping reconciled state entries → `psRow` (carry status, pid->(implied), port, url, plus statusChangedAt if you extend psRow — optional; keep psRow stable, consider adding `StatusChangedAt` omitempty).
- In `ps` run: compute state path from compose dir; if the file exists, `ReadRunState` → `ReconcileRunState(st, utils.PidAlive, utils.ContainerRunning)` → `WriteRunState` (persist reconciliation) → render `psRowsFromState`. If absent, keep today's declared-topology + port-probe path (unchanged).
- Optionally do the same read+reconcile in `status` for the real signal; if `status`'s logic is more entangled, scope this task to `ps` and note status as a follow-up.

**Step 4: Run, verify pass** — targeted; `go test ./...`; build. Sanity: with the Task-4 detached `sleep` running, `/tmp/corgi --json ps` shows it `running`; after killing the sleep, `/tmp/corgi --json ps` shows it `crashed`. With no state file (fresh dir), `ps` still works as before.

**Step 5: Commit**

```bash
git add cmd/ps.go cmd/ps_test.go
git commit -m "feat: ps reads and reconciles run-state when present"
```

---

## Phase 4 — stop / restart

### Task 6: `corgi stop`

**Files:**
- Create: `cmd/stop.go`
- Test: `cmd/stop_test.go`

**REQUIRED reading:** `cmd/run.go` `cleanup(corgi)` (afterStart hooks), how db `make down` is invoked (`utils.ExecuteForEachService("down")` / `CheckForFlagAndExecuteMake`), `utils.KillAllStoredProcesses` and the pgid-kill helper.

**Step 1: Failing test** — pure teardown planner: given reconciled state + a `--service` filter, returns which entries to stop:

```go
func TestStopTargets(t *testing.T) {
	st := utils.RunState{Services: []utils.RunStateEntry{
		{Name: "api", Kind: "service", PID: 1, Status: "running"},
		{Name: "web", Kind: "service", PID: 2, Status: "crashed"},
	}}
	all := stopTargets(st, "")
	if len(all) != 2 {
		t.Errorf("want 2 targets, got %d", len(all))
	}
	one := stopTargets(st, "api")
	if len(one) != 1 || one[0].Name != "api" {
		t.Errorf("want only api, got %+v", one)
	}
}
```

**Step 2: Run, verify fail** — FAIL undefined.

**Step 3: Implement**
- `stopCmd` (`corgi stop`), `--service` string, `--json`. Register in init.
- `stopTargets(st, service) []utils.RunStateEntry` (pure): all entries, or just the named one.
- Run body: load state (if none / empty → idempotent success, summary `{"stopped":[],"failed":[]}`, exit 0). Reconcile. For each target service: SIGTERM its process group (reuse the pgid-kill helper; SIGKILL after a grace timeout). Then, when stopping all: run `cleanup(corgi)` for afterStart hooks and bring down db_services (`make down` via the existing helper). On full stop, delete `.state.json`; on `--service`, rewrite state with that entry marked `stopped` (or removed). Emit `{"stopped":[...],"failed":[...]}` under `--json`. Exit 1 only on teardown error.
- Non-interactive safe (no prompts). Human/log lines via `utils.Info`.

**Step 4: Run, verify pass** — targeted; `go test ./...`; build. Sanity: detached `sleep` via Task 4 → `/tmp/corgi stop` kills it, removes state file; `/tmp/corgi stop` again → idempotent exit 0. `corgi --json stop` pure JSON.

**Step 5: Commit**

```bash
git add cmd/stop.go cmd/stop_test.go
git commit -m "feat: corgi stop tears down detached services + dbs"
```

---

### Task 7: `corgi restart`

**Files:**
- Create: `cmd/restart.go`
- Test: `cmd/restart_test.go`

**Step 1: Failing test** — thin; assert the command is registered and `--service` flag exists (or test a small helper if restart composes stop+detach via exported funcs).

**Step 2–4:** Implement `restartCmd` = run `stop` logic then `run --detach` with the same flags (`--service` scopes both). Reuse the stop and detach entry points (call the functions, don't shell out). Keep `--json` summary (emit the post-restart state/summary). `go test ./...`; build; sanity restart of the `sleep` service yields a new pid in state.

**Step 5: Commit**

```bash
git add cmd/restart.go cmd/restart_test.go
git commit -m "feat: corgi restart (stop then detached run)"
```

---

## Phase 5 — docs

### Task 8: Extend agent docs

**Files:**
- Modify: `docs/agents.md`
- Modify (in-repo skill): `plugins/corgi/skills/corgi/SKILL.md`

**Step 1:** Add a "Lifecycle (detached)" section to `docs/agents.md`: `run --detach` + `--force`, `stop [--service]`, `restart`, that `ps`/`status` now report real `status`/`statusChangedAt` from `.state.json`, the `ALREADY_RUNNING` error, and an updated safe recipe using `run --detach` instead of shell-backgrounding. Note the no-daemon model (crash detected on next `ps`/`status`) and the Windows caveat. Add a short pointer line in SKILL.md.

**Step 2: Verify** recipes against `/tmp/corgi` (detach → ps → stop), capture real output for the doc.

**Step 3: Commit**

```bash
git add docs/agents.md plugins/corgi/skills/corgi/SKILL.md
git commit -m "docs: document corgi detach/stop/restart lifecycle for agents"
```

---

## Final verification (before finishing the branch)

1. `go test ./...` green; `go vet ./...` clean; `go build -o /tmp/corgi .`.
2. No-regression: in a dir with a normal compose and NO `.state.json`, `corgi run` (foreground), `corgi ps`, `corgi status` behave exactly as before.
3. Detached lifecycle e2e: `run --detach` → `ps` shows running → kill one proc → `ps` shows crashed → `stop` cleans up + removes state → `stop` again idempotent.
4. `--json` purity on stdout for `ps`/`stop`/`run --detach` (`| jq` clean).
5. `ALREADY_RUNNING` guard fires on a second `run --detach`.

Then: superpowers:finishing-a-development-branch.
