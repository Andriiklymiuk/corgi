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
- `doctor --json` (also `doctor --fix --json` → `{"ok":bool,"fixed":[...],"skipped":[{"check","reason"}]}`)
- `list --json`
- `config --json`, `config path --json`
- `ps --json`
- `open --json` → `{"opened":[{"service","url"}]}`
- `logs --json` → NDJSON, one `{"service","ts","level","line"}` per line (works with `--all` and follow)
- `db shell <svc> -e "<query>" --json` → `{"service","output"}`
- `create --kind ... --json` (non-interactive) → `{"created","kind","name","path"}`
- `restart --service <name> --json` → updated run-state object
- `mission-control --json` (one MissionSnapshot object; `--watch`: one object per refresh)
- `autopilot status/pause/resume/stop/heartbeat --json` → the autopilot loop state object (`{mode, iteration, lastHeartbeat, lastSummary}`)
- `memory list --json` (array of facts), `memory lint --json` (`{"ok":bool,"errors":[...],"warnings":[...]}`), `memory add --json` / `memory index --json` (created/index summary)
- `suggest-history list --json` (`{"version","entries":[...]}`), `suggest-history check --slug <s> --json` (`{"skip":bool,"reason":"filed|dismissed|proposed|rate-limit|...","slug":...}`), `suggest-history record --json` (echoes the written entry), `suggest-history config --json` (`{"autoFileDrafts":bool,"maxPerWeek":n}`)
- `docs --json-schema`

Not yet pure-JSON: `fork` and the `db` bulk lifecycle flags (`--upAll` etc.)
still stream human output; use `corgi_up`/`corgi_down` (MCP) for lifecycle.

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
{"error": {"code": "E_ALREADY_RUNNING", "message": "corgi is already running for this project — stop or restart first (use --force to override)"}}
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
`corgi restart --service x` restarts a **single** detached service, leaving the
rest running. It only acts on a service already present in the detached
run-state — restarting one that was never started returns `E_NOT_RUNNING`:

```json
{"error": {"code": "E_NOT_RUNNING", "message": "service \"web\" is not in the current detached run; start it with corgi run --detach first"}}
```

On success `--json` returns the updated run-state object (same shape as
`corgi run --detach --json`). Single-service restart is detached-only; there is
no control channel into a live foreground `corgi run`.

### Caveats

- No daemon: a crashed detached service is detected lazily on the next
  `ps`/`status`, not via a live notification.
- Windows: detach and liveness checks are best-effort.

## Run a branch or external dir (no compose edit)

For reviewing a PR branch, running an agent's worktree, or pointing a service at a
checkout elsewhere — without touching `path:` in `corgi-compose.yml`. Repeatable,
per-service; any service you don't flag runs from its compose `path:`. Available
on `run`, `exec`, and `test`. All three repoint the service's working dir, so its
env generation, `beforeStart`/`afterStart`, and process all run there.

- `--service-dir <name>=<path>` — run from an existing dir (e.g. a worktree you
  already made). The dir must exist.
- `--service-branch <name>=<branch>` — run on a git branch via a **reused**
  worktree under `corgi_services/.worktrees/<svc>-<branch>`. **Non-destructive**:
  the main checkout is untouched. Re-runs reuse the worktree (deps + uncommitted
  work persist); the branch must exist (local or remote).
- `--service-checkout <name>=<branch>` — `git checkout <branch>` in place in the
  service's `path:`. **Refuses on a dirty tree** (commit/stash, or use
  `--service-branch`). Leaves the repo on that branch.

A service may appear in only one of the three (else an `E_CONFIG` error). Detached
works too — the override is applied before the attached/detached split.

```bash
# run a feature branch of api, rest of the stack from compose path:
corgi run --detach --service-branch api=feature/login

# mix: api on a branch, web from an explicit worktree dir
corgi run --detach --service-branch api=feature/login --service-dir web=/tmp/wt/web

# test / one-off on a branch
corgi test --service api --service-branch api=feature/login
corgi exec api --service-branch api=feature/login --ensure-deps -- npm run migrate
```

Worktrees accumulate one-per-branch under `corgi_services/.worktrees/`. Manage them:

```bash
corgi worktree list     # print created worktree paths
corgi worktree prune    # git worktree remove them all (also done by corgi clean)
```

This is what lets one agent run an isolated branch while another runs `main`, or a
multi-service story verify a producer from its worktree without committing to the
main checkout.

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
| `E_ALREADY_RUNNING` | a detached run is already active | stop/restart first, or `--force` |
| `E_UNSUPPORTED` | operation not supported yet | use the suggested alternative |
| `E_NOT_RUNNING` | no matching detached service to act on | start it with `corgi run --detach` first |
| `E_CONFIG_PATH` | cannot resolve the user-config dir | check `~/.corgi` perms |
| `E_CONFIG_READ` | cannot read the user-config file | check `~/.corgi/config.yml` |
| `E_DUPLICATE_NAME` | a name is used by more than one service/db_service (or duplicated within a section) | rename so every service/db_service name is unique |
| `E_PORT_RANGE` | a configured port is outside 1-65535 | use a port in the valid range |
| `E_MEMORY_SECRET` | a secret-shaped string is in committed memory | remove it — memory is committed; secrets stay in gitignored `.env` |
| `E_MEMORY_TYPE_MISMATCH` | a fact's `type` doesn't match its folder | move the file or fix `type:` |
| `E_MEMORY_BAD_NAME` | `name` isn't kebab-case or doesn't match the filename | rename so `name` == filename stem |
| `E_MEMORY_NO_FRONTMATTER` | a memory fact is missing its `name`/`description` frontmatter | add the frontmatter block |

Validation also emits advisory warnings (not errors): `W_NO_HEALTHCHECK`,
`W_NO_BRANCH`, and `W_UNKNOWN_FIELD` (an unknown/typo'd key was ignored — warn
now, may become an error later). Warnings never abort `run`/`exec`. `corgi memory lint`
also emits `E_MEMORY_DANGLING_LINK` as a warning (a `[[link]]` points at no existing fact).

Note: a few codes were renamed for consistency — `INPUT_REQUIRED` →
`E_INTERACTIVE_REQUIRED`, `config` → `E_CONFIG`, `ALREADY_RUNNING` →
`E_ALREADY_RUNNING`, and `UNSUPPORTED` → `E_UNSUPPORTED`. Every emitted code now
lives in the catalog above.

## Commands that need a flag (or error in non-interactive mode)

These would normally show a picker; non-interactively they exit 2 unless you
pass the flag:

- `logs` → `--service <name>` (or `--all`)
- `db shell` → service-name arg, or `-e "<query>"` for one-shot
- `create` → `--kind <db_service|service|required> --name <name>`
- `fork` → `--all` **or** `--service <name>`, plus `--gitProvider <github|gitlab>`
- `suggest-history check` → `--slug <s>`; `suggest-history record` → `--slug <s> --status <filed|dismissed|proposed|skipped>` (else exit 2). `--workspace <path>` overrides cwd (cron passes an absolute path).
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
if those databases carry no `profiles:` tag). `--profile` accepts a
comma-separated list and runs the **union** of those profiles, e.g.
`corgi run --profile backend,worker`. With no `--profile`, everything runs
(unchanged docker-compose-style behavior). If none of the requested profiles
match anything, corgi warns (`E_UNKNOWN_PROFILE`) and starts nothing rather
than starting everything; a partially-unknown list just uses the matches.

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
corgi run --profile backend --dry-run --json        # preview just the backend profile
corgi run --profile backend,worker --dry-run --json # union of two profiles
corgi test --profile backend --json                 # test only that profile's services
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
  stays literal), silently — so runtime/per-service env, tunnel hostnames, and
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
    password: ${DB_PASSWORD}        # unset & no default -> left literal, silently
    port: ${PG_PORT:-5432}          # defaults to 5432
```

## Safe agent recipe

Use `corgi run --detach` — it returns immediately and the services outlive
corgi. Probe with `status`/`ps`, never by re-running `run` (a second
`run --detach` errors `E_ALREADY_RUNNING`). Tear down with `corgi stop`.

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

## Workspace memory (`corgi memory`)

`.corgi/memory/` is an **opt-in, committed** store of stack decisions, incidents,
domain facts, and recurring fixes — the team/agent's shared memory of *why*, keyed to
this `corgi-compose.yml`. Absent → every subcommand is a no-op (exit 0). One fact per
Markdown file (`<type>/<name>.md`) with `name`/`description`/`type` frontmatter and
`[[links]]`; `index.md` is generated. **Never commit secrets** — `corgi memory lint`
fails the store on a key-shaped string. The agent skills read it before acting and
append to it (confirmed) after a notable fix; a fix `pattern:` seen ≥3× is *proposed*
as a learned skill/template (human-approved, never auto-installed).
