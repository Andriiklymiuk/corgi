# Driving corgi as an agent

Guide for AI agents and scripts running corgi non-interactively.

## Non-interactive mode

Corgi auto-detects when it must not prompt and either skips the prompt or exits
with a clear error (exit code 2) instead of hanging. It is triggered by any of:

- A CI env var (`CI=true`, etc.).
- An agent env var: `CLAUDECODE`, `CLAUDE_CODE`, `ANTHROPIC_AGENT`.
- No TTY on stdin **or** stdout (piped / redirected).

Force prompts back on with the global `--interactive` flag.

## JSON output

Global `--json` makes stdout pure machine-readable JSON; human/log lines go to
stderr. Commands that emit pure-JSON stdout:

- `status --json` (one-shot: array; `--watch`: NDJSON, one object per transition)
- `doctor --json`
- `list --json`
- `config --json`, `config path --json`
- `ps --json`
- `docs --json-schema`

`run --json` is best-effort: it prints one JSON startup summary
(`{"started":[...],"failed":[...]}`) and then streams service logs to stderr —
stdout is not a single JSON document for the whole run.

Errors under `--json` have the shape:

```json
{"error": {"code": "E_INTERACTIVE_REQUIRED", "message": "..."}}
```

The `code` is a stable string an agent can branch on (see [Error codes](#error-codes)).

## Lifecycle (detached)

`corgi run --detach` (`-d`) starts every service as a detached process group that
survives corgi exiting, persists `corgi_services/.state.json`, and returns
immediately. It forces logs on. Under `--json` it prints the run-state object:

```json
{
  "composePath": "/path/corgi-compose.yml",
  "startedAt": "2026-05-21T10:59:47Z",
  "services": [
    {
      "name": "api",
      "kind": "service",
      "pid": 76960,
      "pgid": 76960,
      "command": "sleep 60",
      "logFile": "/path/corgi_services/.logs/api/2026-05-21T13-59-47.log",
      "status": "running",
      "startedAt": "2026-05-21T10:59:47Z",
      "statusChangedAt": "2026-05-21T10:59:47Z"
    }
  ],
  "dbServices": []
}
```

A second `run --detach` while a run-state exists errors (exit 1):

```json
{"error": {"code": "ALREADY_RUNNING", "message": "corgi is already running for this project — stop or restart first (use --force to override)"}}
```

`--force` replaces the existing run-state **and kills the previously tracked
processes first** (no orphans), then starts fresh.

### Status while detached

There is no daemon. With a state file present, `ps`/`status` report **real**
status (`running`/`crashed`/`stopped`) reconciled live — a dead pid flips to
`crashed` on the next read. Without a state file they fall back to declared
topology + a port probe. `statusChangedAt` lives in `.state.json` / the
`run --detach --json` run-state object, not in the `ps` rows (which carry just
`name`/`kind`/`port`/`status`/`url`).

```json
[{"name": "api", "kind": "service", "status": "running"}]
```

After the pid dies, the very next `corgi --json ps` shows:

```json
[{"name": "api", "kind": "service", "status": "crashed"}]
```

### Stop / restart

`corgi stop [--service <name>] [--json]` reads the state, SIGTERMs each process
group (SIGKILL after a grace period), runs `afterStart` hooks, brings
`db_service` containers fully down, and removes `.state.json`. `--service x`
stops one and keeps the rest. It is idempotent (exit 0 when nothing is running).

```json
{"stopped": ["api"], "failed": []}
```

`corgi restart [--json]` is a full-stack stop + detached start.
`restart --service x` is **not** supported yet — it exits 2:

```json
{"error": {"code": "UNSUPPORTED", "message": "restart --service is not supported yet; use: corgi stop --service api && corgi run --detach"}}
```

### Caveats

- No daemon: a crashed detached service is detected lazily on the next
  `ps`/`status`, not via a live notification.
- Windows: detach and liveness checks are best-effort.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Operational failure (a check/command failed) |
| 2 | Usage / missing required input |

## Error codes

Under `--json`, errors carry a stable `code` string. Branch on the code, not the
message text (messages may change wording). The catalog:

| Code | Meaning | Typical fix |
|------|---------|-------------|
| `E_COMPOSE_NOT_FOUND` | no `corgi-compose.yml` resolved | run from a dir with one, or pass `-f <path>` |
| `E_COMPOSE_PARSE` | YAML failed to parse | fix syntax; validate with `corgi validate` |
| `E_INTERACTIVE_REQUIRED` | a prompt was needed but input is unavailable | pass the flag the message names |
| `E_SERVICE_NOT_FOUND` | named service/db not in compose | check the name against `corgi ps` |
| `E_MISSING_FIELD` | a required field is absent | supply it (message names the field) |
| `E_DANGLING_DEP` | `depends_on` references an unknown name | fix the reference |
| `E_DEPENDENCY_CYCLE` | `depends_on_services` forms a cycle | break the cycle |
| `E_UNKNOWN_DRIVER` | `db_services.driver` is not a known driver | use a supported driver |
| `E_MISSING_START` | a service has a `port` but no `start`/`runner` | add a `start` command or `runner: docker` |
| `E_PORT_CONFLICT` | two services/dbs bind the same host port | give them distinct ports |
| `E_UNHEALTHY` | a readiness/health probe failed | check the service; inspect `corgi logs` |
| `E_READINESS_TIMEOUT` | a dependency wasn't ready before the deadline | raise `--ready-timeout`, or fix the dep |
| `E_DOCKER_DOWN` | the docker daemon is unreachable | start Docker |

Note: the `E_INTERACTIVE_REQUIRED` code was previously emitted as `INPUT_REQUIRED`.
A few command-specific codes also exist outside this catalog: `ALREADY_RUNNING`
(second `run --detach`) and `UNSUPPORTED` (`restart --service`).

## Commands that need a flag (or error in non-interactive mode)

These would normally show a picker; non-interactively they exit 2 unless you
pass the flag:

- `logs` → `--service <name>` (or `--all`)
- `db shell` → service-name arg, or `-e "<query>"` for one-shot
- `create` → `--kind <db_service|service|required> --name <name>`
- `fork` → `--all` **or** `--service <name>`, plus `--gitProvider <github|gitlab>`
- any command needing the compose file → run from a dir containing
  `corgi-compose.yml`, or pass `-f <path>`

Non-interactive scaffolding:

```bash
corgi create --kind db_service --name db --driver postgres --port 5439
corgi create --kind service    --name api --path ./api --port 8080
corgi fork --all --gitProvider github
corgi fork --service api --gitProvider github --newName api-fork --private
```

## Schema

Get a draft-07 JSON Schema for `corgi-compose.yml`:

```bash
corgi docs --json-schema > corgi-compose.schema.json
```

In an editor, point the YAML language server at it with a top-of-file directive:

```yaml
# yaml-language-server: $schema=./corgi-compose.schema.json
```

## Safe agent recipe

Use `corgi run --detach` — it returns immediately and the services outlive
corgi. Probe with `status`/`ps`, never by re-running `run` (a second
`run --detach` errors `ALREADY_RUNNING`). Tear down with `corgi stop`.

```bash
# 1. preflight (exit 1 if a port is taken / docker down)
corgi --json doctor

# 2. launch detached (writes corgi_services/.state.json, returns immediately)
corgi --json run --detach

# 3. block until every probed target is up (exit 1 on timeout)
corgi status --ready --timeout 2m

# 4. inspect real status as JSON (running/crashed/stopped from state)
corgi --json status
corgi --json ps

# 5. read a service's persisted logs (detach forces logs on)
corgi logs --service api --idle 0

# 6. stop the stack (SIGTERM each group, removes the state file)
corgi --json stop

# 7. tear down volumes/containers
corgi clean -i all
```

## JSON output examples

`corgi --json doctor` (object with `ok` + `checks`; `checks` is `null` when no
ports are declared):

```json
{
  "ok": true,
  "checks": [
    {"name": "port:8080", "ok": true}
  ]
}
```

`corgi --json ps` (array of declared targets):

```json
[
  {
    "name": "app",
    "kind": "service",
    "port": 8080,
    "status": "stopped",
    "url": "http://localhost:8080"
  }
]
```

`corgi --json config`:

```json
{
  "version": 1,
  "notifications": true,
  "path": "/Users/you/.corgi/config.yml"
}
```

Top-level keys of the compose schema:

```bash
corgi docs --json-schema | jq '.properties | keys'
# ["afterStart","beforeStart","db_services","description","init",
#  "name","required","services","start","useAwsVpn","useDocker"]
```
