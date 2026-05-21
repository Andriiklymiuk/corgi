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
| `E_CONFIG` | could not load/resolve the compose file | run from a dir with corgi-compose.yml or pass `-f` |
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
| `E_USAGE` | invalid command usage / arguments | fix the command invocation |
| `E_EXEC_FAILED` | the command could not be spawned | check the binary exists / path |
| `E_UNKNOWN_PROFILE` | `run --profile` matched no services/db_services | check the profile name against the compose `profiles:` lists |
| `E_INVALID_CONDITION` | invalid depends_on `condition` (use `ready` or `started`) | fix the value |

Note: the `E_INTERACTIVE_REQUIRED` code was previously emitted as `INPUT_REQUIRED`,
and the compose-load error code changed from `config` to `E_CONFIG`.
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

## Authoring & inspection

These commands inspect or exercise the compose file without booting the full
stack. They share the global `--json` contract (pure JSON on stdout, logs to
stderr) and the [error-code catalog](#error-codes).

### `corgi validate` (alias `lint`)

Static semantic checks over `corgi-compose.yml` — no containers, clones, or
network. Pairs with `docs --json-schema`: the schema checks *structure*,
`validate` checks *semantics* (dangling deps, cycles, unknown driver, a port
without a start command, port conflicts).

Flags:

- `--json` — emit the report object (below).
- `--strict` — treat warnings as failures.

```json
{"ok": true, "errors": [], "warnings": [{"code": "...", "message": "...", "field": "..."}]}
```

Each issue is `{code, message, field}`. Exit 0 when clean, 1 when there are
errors (or warnings under `--strict`), 2 when the compose file fails to load.

### `corgi run --dry-run`

Compute the start plan with **no side effects** — no clone, no `make up`, no
process spawn, no `.env` writes. Runs validation first, then reports the
resolved order and per-item details. Composes with `--profile` /
`--services` / `--omit` to preview a narrowed run. Exit 0 if valid, 1 if
validation finds errors.

```json
{
  "valid": true,
  "order": ["db:main", "svc:api"],
  "databases": [{"name": "main", "driver": "postgres", "port": 5432, "willStart": true}],
  "services": [
    {"name": "api", "port": 8080, "willClone": false, "dependsOn": ["db:main"], "envKeys": ["DATABASE_URL"]}
  ],
  "warnings": [],
  "errors": []
}
```

`order` ids are `db:<name>` / `svc:<name>`. `errors` is omitted when empty.

### `corgi exec <service> -- <cmd> [args...]`

Run a one-off command in a service's resolved environment and working
directory (its `.env` is sourced the same way `start` commands get it). The
child's exit code becomes corgi's exit code.

Flags:

- `--json` — emit `{"service": "...", "exitCode": 0, "durationMs": 12}`; child
  stdout/stderr are routed to stderr so stdout stays pure JSON.
- `--ensure-deps` — wait for the service's `depends_on_db` /
  `depends_on_services` to be reachable first.
- `--ready-timeout <dur>` — cap that wait (default `15s`).

```bash
corgi exec api -- npm run migrate
corgi exec api --ensure-deps -- pytest -q
```

An unknown service exits 2 with `E_SERVICE_NOT_FOUND`; a readiness timeout
under `--ensure-deps` exits 1 with `E_READINESS_TIMEOUT`.

### `corgi test`

Run each selected service's `test` script (a script named `test` under
`services.<name>.scripts`) in that service's env and working dir. It does
**not** start anything — that's `run`'s job. Services without a `test` script
are **skipped, not failed**. Multi-command scripts run sequentially and stop on
the first non-zero exit.

Flags:

- `--service <name>` — only this service (unknown name exits 2).
- `--profile <name>` — narrow to a profile first (see [Profiles](#profiles)).
- `--ensure-deps` / `--ready-timeout <dur>` — gate on dependency readiness, as
  in `exec`.
- `--json` — emit the results object.

```json
{
  "services": [
    {"name": "api", "exitCode": 0, "durationMs": 1840, "passed": true},
    {"name": "worker", "skipped": true}
  ],
  "passed": true
}
```

Exit 0 if every run test passes (skips don't count), 1 if any fail, 2 on an
unknown `--service`.

## Profiles

`corgi run --profile <name>` runs only the services/db_services whose
`profiles:` list contains `<name>`, **plus their transitive `depends_on`
closure** (so a profile still brings up the databases its services need, even
if those databases carry no `profiles:` tag). With no `--profile`, everything
runs (unchanged docker-compose-style behavior). An unknown profile matches
nothing and warns (`E_UNKNOWN_PROFILE`) rather than starting everything.

`profiles:` is a string array on entries under `services` and `db_services`.
`--profile` composes with `--services` / `--dbServices` / `--omit` as an
**intersection** (the profile narrows first, then the other filters apply). It
also works with `--dry-run` to preview a profile's plan.

```yaml
services:
  api:
    profiles: [backend]
db_services:
  main:
    driver: postgres
    profiles: [backend]
```

```bash
corgi run --profile backend --dry-run --json   # preview just the backend profile
corgi test --profile backend --json            # test only that profile's services
```

## Dependency readiness gating

`depends_on_db` and `depends_on_services` entries accept an optional
`condition`:

- `condition: ready` — wait until the dependency's readiness probe passes.
- `condition: started` — wait only until corgi has launched the dependency.

By default (no `condition`, no flag) services start in **parallel** — no
waiting (unchanged). corgi waits before starting a dependent only when an edge
sets `condition`, or when `run --gate-deps` is passed (which gates *every*
edge). `--ready-timeout <dur>` (default `15s`) bounds each wait; a timeout is
non-fatal — corgi proceeds anyway and emits `E_READINESS_TIMEOUT`.

```yaml
services:
  api:
    depends_on_db:
      - name: main
        condition: ready
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

## Environment interpolation

`${VAR}` placeholders in `corgi-compose.yml` are expanded in the raw file
**before** YAML parsing, so they work in any string field (passwords, ports,
paths, image refs, environment entries).

- `${VAR}` — replaced with the value of `VAR`.
- `${VAR:-default}` — value of `VAR`, or `default` if `VAR` is unset/empty.
- `$${LITERAL}` — escapes to the literal `${LITERAL}` (not expanded).
- Only **braced** forms are expanded. Bare `$VAR` is left untouched (so shell
  snippets in `start` commands are safe).
- An unset var with **no default** is left **unresolved** (the `${VAR}` token
  stays literal) and corgi prints one warning per name — so tunnel hostnames and
  cross-service `${producer.VAR}` refs that resolve later still work. Use
  `${VAR:-default}` for an explicit fallback. corgi never silently substitutes
  empty.
- Dotted forms like `${producer.VAR}` are **not** touched by this global pass
  (only simple `${NAME}` is) — they are resolved later from per-service env.
- This pass runs everywhere, **including inside `start`/`beforeStart`/`afterStart`
  and `scripts` command strings**. A braced `${VAR}` / `${VAR:-default}` there is
  resolved at **load time** (against process env + sibling `.env`), not by the
  runtime shell. To defer expansion to the runtime shell instead (e.g. a var only
  defined in the service's own runtime env), escape it as `$${VAR}`, which becomes
  the literal `${VAR}` for the shell to expand.

Values come from the process environment first, then an optional `.env` file
in the same directory as the compose file (process env wins). The `.env`
parser is minimal: `KEY=value` lines, `#` comments and blank lines ignored,
surrounding quotes trimmed.

```yaml
db_services:
  pg:
    driver: postgres
    password: ${DB_PASSWORD}        # unset & no default -> left literal + warning
    port: ${PG_PORT:-5432}          # defaults to 5432
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
