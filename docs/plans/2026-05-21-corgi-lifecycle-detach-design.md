# Corgi Lifecycle (detach / state / stop) — Design

Date: 2026-05-21

## Problem

`corgi run` is foreground-only and tracks service PIDs in memory. A separate
invocation (`corgi ps`, `corgi status`) cannot see what is actually running — it
can only TCP-probe declared ports, so it cannot distinguish "crashed" from
"never started" from "started by something else". Agents driving corgi (and
humans who want a background dev env) have no way to start services detached,
query real state, or stop them later without the original foreground process.

## Goal

Give corgi cross-invocation knowledge of what is running, via a state file and
detached processes — no resident daemon. This unlocks `run --detach`, `stop`,
`restart`, and accurate `ps`/`status`, serving ephemeral, long-lived, and
debugging agent workflows alike.

## Non-goals

- No background daemon / socket API (rejected: too heavy for corgi's "one yml,
  no infra" scope).
- No live crash desktop-notify for detached runs (that needs a resident
  process; detached runs report `crashed` lazily on the next `ps`/`status`).
- No change to foreground `corgi run` behavior. Detach is strictly opt-in.

## Architecture: state file + detached processes

### State file

`corgi_services/.state.json`, one per project, next to the compose file. Atomic
writes (temp + `os.Rename`).

```json
{
  "composePath": "/abs/corgi-compose.yml",
  "startedAt": "2026-05-21T10:00:00Z",
  "updatedAt": "2026-05-21T10:00:30Z",
  "services": [
    {
      "name": "api", "kind": "service",
      "pid": 1234, "pgid": 1234, "port": 8080,
      "command": "npm start",
      "logFile": "corgi_services/.logs/api/2026....log",
      "status": "running",
      "startedAt": "2026-05-21T10:00:00Z",
      "statusChangedAt": "2026-05-21T10:00:00Z",
      "exitCode": null
    }
  ],
  "dbServices": [
    {
      "name": "pg", "kind": "db_service",
      "container": "corgi_pg", "port": 5432,
      "status": "running",
      "startedAt": "2026-05-21T10:00:00Z",
      "statusChangedAt": "2026-05-21T10:00:00Z"
    }
  ]
}
```

- `startedAt` (per entry): when launched.
- `statusChangedAt`: updated only when a liveness probe flips status. Stable
  across reads when nothing changed.
- `updatedAt` (top): last reconcile/write.
- `exitCode`: recorded when a service is found dead, if obtainable.

### `corgi run --detach` (alias `-d`)

Runs the normal startup sequence (preflight, db `make up`, env generation,
beforeStart), then:
- Forces `--logs` on (no console attached → service stdout/stderr must go to log
  files via the existing log-writer machinery).
- Spawns each service start-command as a **detached process group** (survives
  corgi exit), records pid/pgid/port/logFile/command/startedAt.
- Writes `.state.json`, prints the JSON startup summary, returns 0.
- No watch loop, no blocking.

### `corgi stop [--service x]`

Reads `.state.json`, reconciles liveness, then full teardown (symmetric with
foreground Ctrl-C): SIGTERM each service process group (SIGKILL after a grace
timeout), run `afterStart` hooks (reuse `cleanup(corgi)`), bring down
db_service docker containers (`make down`). Removes `.state.json` on full stop.
`--service x` stops one entry and keeps the file. `--json` summary
`{"stopped":[...],"failed":[...]}`. Idempotent: exit 0 when nothing running;
exit 1 on teardown error.

### `corgi restart [--service x]`

`stop` then `run --detach` with the same flags. Convenience for long-lived envs.

### Wiring `ps` / `status` to state

When `.state.json` exists, `ps` reports real `status`/`pid`/`statusChangedAt`
(reconciled at read), not a port guess. With no state file, falls back to
today's declared-topology + port-probe behavior. `status` gains the real
running/crashed signal where state exists, keeping its probe behavior otherwise.

## Liveness reconciliation

A pure function:

```
reconcile(state, pidAlive func(pid int, command string) bool,
                 containerState func(name string) string) -> state
```

For each service: if `pidAlive` is false, set `status=crashed`, set
`statusChangedAt=now`, record `exitCode` if available; else keep `running`.
`pidAlive` verifies the pid is still *our* process (signal 0 + command/startedAt
match) to defend against OS pid reuse. For each db_service: map docker container
state to running/stopped/crashed. Only mutate `statusChangedAt` when status
actually changes. `ps`/`status`/`stop` call this on read and rewrite the file.

## Concurrency / safety

- Atomic write (temp + rename); separate invocations tolerate last-write-wins.
- Stale-pid reuse defended by command/startedAt verification in `pidAlive`.
- Reuse existing `SetProcessGroup` (platform-split: `proc_default.go` /
  `proc_windows.go`) and the process-kill helpers.
- `ALREADY_RUNNING`: `run --detach` with a live state file errors (exit 1,
  `{"error":{"code":"ALREADY_RUNNING"}}`); `--force` does stop-then-start.

## Cross-platform

Detach + signal-0 liveness + pgid kill work on darwin/linux via existing
helpers. Windows differs: if detach cannot be done safely in v1, `run --detach`
errors on Windows with a clear message (foreground run still works there).

## No-regression guarantees

- Foreground `corgi run` path is untouched (detach is a new flag branch).
- `ps`/`status` keep current behavior when no `.state.json` exists.
- `cleanup(corgi)` reused as-is for afterStart; db up/down reuse existing
  `make` execution helpers — no new teardown logic duplicated.
- All new output honors `--json` / `utils.Info` conventions (stdout stays pure
  JSON under `--json`).

## Error codes (extends existing taxonomy)

`ALREADY_RUNNING`, `NOT_RUNNING` (stop with no/empty state — treated as success
unless `--json` strictness needed), `CONFIG`, plus per-service `failed[]`
entries in summaries.

## Testing (TDD)

- Pure `reconcile` with injected `pidAlive`/`containerState` fakes: running→
  crashed flips + sets `statusChangedAt`; stable when unchanged.
- State round-trip marshal/unmarshal; atomic write (temp+rename) leaves valid
  file.
- Detach summary builder; `stop` teardown ordering (procs → afterStart → db
  down); `--service` single-entry path.
- Integration: spawn a `sleep`, record it in state, `ps` sees `running`; kill
  it, `ps` sees `crashed`.

## Build order

1. State model + atomic read/write + pure `reconcile` (utils).
2. `run --detach`: spawn detached, force logs, write state, JSON summary,
   ALREADY_RUNNING guard.
3. Wire `ps` (and `status`) to read+reconcile state when present.
4. `corgi stop` (full teardown, `--service`, `--json`).
5. `corgi restart`.
6. Docs: extend `docs/agents.md` lifecycle recipe; mention in skill.
