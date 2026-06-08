---
name: commands
description: All corgi CLI commands and global flags with aliases, key flags, and use cases. Read when explaining or choosing a corgi command.
---

# Corgi CLI commands

## Global flags (work on any command)

| Flag | Purpose |
|---|---|
| `-f, --filename <path>` | Use a non-default compose file (default: `corgi-compose.yml`) |
| `-t, --fromTemplate <url>` | Download and run a corgi-compose from a URL (e.g. raw GitHub) |
| `--fromTemplateName <name>` | Run a named example from the corgi_examples repo |
| `-l, --exampleList` | Print available examples from the corgi_examples repo |
| `-o, --runOnce` | Run services once and exit rather than loop-watching |
| `-g, --global` | Use a globally-registered compose path by name |
| `--privateToken <token>` | Auth token for cloning private repos listed in `cloneFrom:` |
| `--silent` | Suppress welcome / informational output |
| `--fromScratch` | Wipe `corgi_services/` before running |
| `--describe` | During parse, dump each db/service/required as indented JSON. Does **not** short-circuit — `corgi run --describe` prints then still runs. For a rendered, side-effect-free doc use `/corgi-describe`. |
| `--dockerContext <ctx>` | `default`, `orbctl`, or `colima` |

## Commands

### `corgi run` (aliases: `start`, `r`) {#corgi-run-flags}

Long-running. Starts all db_services + services concurrently, streams logs. **Do not invoke synchronously** — see `long-running.md`.

Notable flags:
- `-s, --seed` — run seed scripts after db boot
- `--omit <list>` — comma-separated service names to skip
- `--services <list>` — whitelist only these services
- `--dbServices <list>` — whitelist only these dbs
- `--pull` — `git pull` in service dirs before starting
- `--no-watch` — disable auto-reload on compose file change
- `--host <ip|auto>` — host substituted for `localhost` in service URL env vars (LAN access from phones, etc.). `auto` picks first non-loopback IPv4. db_services stay on localhost.
- `--tier <name>` — select a compose `envTiers` entry: resolves each service's env from the tier's `dir` (`<dir>/<service>.env`, with `${tier}` substituted in `copyEnvFromFilePath`), and applies the tier's default `dbServices` unless `--dbServices` is passed. A tier with `confirm: true` prompts before running. Also on `corgi env --tier`.
- `--yes` — skip confirmation prompts (e.g. a tier marked `confirm: true`). Required when non-interactive/`--json`.
- `--kill-port` — before starting, if a (non-manual) service's port is already in use, kill the holder and reclaim it. Without this flag a busy service port aborts the run with `E_PORT_CONFLICT` naming the owner. db_services ports are not preflighted (corgi reuses already-running db containers).
- `--no-cache` — ignore beforeStart `cacheKey` fingerprints; run every beforeStart step (otherwise a step whose `cacheKey` files are unchanged is skipped).
- `--with-deps` — with `--services X`: also start X's transitive `depends_on` closure (upstream services + their db_services), instead of needing to list `--dbServices` manually. Narrows db_services to what the selected services need.
- `--open` — open each service's URL in the browser when it passes its `healthCheck`, for services that declare `openOnReady` (replaces `sleep N && open <url>`).
- `--tunnel` — open public HTTPS tunnels alongside the stack for every service with a `tunnel:` block. Equivalent to a parallel `corgi tunnel`, bundled into the one process.
- `--logs` — persist stdout/stderr of every service and db_service to `corgi_services/.logs/<name>/<timestamp>.log`. Capped 50 MB per file, keeps 10 newest runs per service, older pruned automatically. A `.logs/` entry is auto-added to `corgi_services/.gitignore`. Read back with `corgi logs`.
- `--ci` — CI mode: suppress spinners, banners, color output. Plain log lines only. Auto-enabled when any common CI environment variable is set: `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`, `BUILDKITE`, `JENKINS_URL`, `TEAMCITY_VERSION`, `TRAVIS`, `DRONE`, `BITBUCKET_BUILD_NUMBER`, `CODEBUILD_BUILD_ID`. Pair with `--runOnce` for pipelines.
- `--notify` (default `true`) — send a desktop notification when a service exits non-zero (and corgi is not shutting down). Requires a one-time opt-in via `corgi doctor`; never fires on Ctrl-C. Duplicate notifications with the same title+body are throttled to one per 30 seconds so a crash-looping service can't spam the desktop. Pass `--notify=false` to silence per-run. macOS uses `osascript`, Linux `notify-send`, Windows PowerShell toast.
- `--profile <name>` — run only services/db_services whose `profiles:` list contains `<name>`, plus their transitive `depends_on` closure. Accepts a comma-separated list for the union, e.g. `--profile backend,worker`. No `--profile` runs everything. Unknown profile runs nothing (warns); a partially-unknown list uses the matches. Composes with `--services`/`--omit`/`--dbServices` as an intersection.
- `--dry-run` — compute and print the start plan with no side effects (no clone, no `make up`, no spawn, no `.env` writes). Runs validation first. Pair with `--json` for a machine plan: `{valid, order, databases, services, warnings, errors}`. Exit 0 if valid, 1 on validation errors.
- `--gate-deps` — gate startup on dependency readiness for every `depends_on` edge (default: only edges with `condition: ready|started` are gated; otherwise parallel start).
- `--ready-timeout <dur>` — max wait for a db/dependency to become ready (default `15s`, non-fatal on timeout).
- `--service-dir <name=path>` — run the named service from `path` instead of its compose `path:` (repeatable). Points run at an external checkout — e.g. a git worktree — so its env generation, `beforeStart`/`afterStart` and process all happen there; the rest of the stack is untouched. The dir must exist (unknown name or missing dir is a hard error). Opt-in; no flag = unchanged behaviour. Lets the `stories` skill run a worktree'd producer without committing it to the main checkout.
- `--service-branch <name=branch>` — run the named service on a git branch via a **reused** worktree under `corgi_services/.worktrees/<svc>-<branch>` (repeatable). corgi prunes stale entries then reuses the worktree if healthy, or creates it (`git worktree add`) when missing — keeping installed deps and any uncommitted work across runs. **Non-destructive**: the service's main checkout is never touched. The rest of `--service-dir`'s behaviour applies (env/beforeStart/process from the worktree). Clean up with `corgi worktree prune`. The branch must exist (local or remote).
- `--service-checkout <name=branch>` — run the named service on a branch by checking it out **in place** in its compose `path:` (repeatable). **Refuses on a dirty tree** (commit/stash first, or use `--service-branch`). Leaves the repo on that branch afterwards. Use when you want the actual checkout switched, not an isolated worktree.

A service may appear in only one of `--service-dir`/`--service-branch`/`--service-checkout`. All three funnel into the same working-dir override, so env, deps, `beforeStart`/`afterStart`, the process, and `corgi test`/`corgi exec` all operate there.

**Tips — picking the override (none of these edit `corgi-compose.yml`):**
- Want to run a **branch** and keep your checkout intact → `--service-branch svc=branch`. Reused worktree, non-destructive, deps persist. Best default for "try this branch."
- Already have a **checkout/worktree** somewhere → `--service-dir svc=/path`. corgi runs it as-is.
- Want your repo **actually on** the branch (not a worktree) → `--service-checkout svc=branch`. Clean tree only.
- **Mix freely** — flag the few services you're changing, the rest run from their compose `path:`:
  ```bash
  corgi run --detach \
    --service-branch api=feature/login \
    --service-dir web=/tmp/wt/web
  # admin, worker, db_services → compose path:
  ```
- **Compare two branches** of one service side by side: run on branch A in one terminal, point a second stack at branch B (different ports) — each isolated in its own worktree.
- Worktrees accumulate one-per-branch under `corgi_services/.worktrees/`; `corgi worktree prune` clears them. Re-running the same branch reuses the dir (fast, keeps `node_modules`).

`depends_on_db`/`depends_on_services` entries take an optional `condition: ready` (wait for readiness probe) or `condition: started` (wait until launched). Empty = no gating unless `--gate-deps`.

### `corgi validate` (alias: `lint`)

Static semantic checks over `corgi-compose.yml` — no containers, clones, or network. Complements `corgi docs --json-schema` (schema = structure, validate = semantics: dangling deps, dependency cycles, unknown driver, port-without-start, port conflicts).

Flags:
- `--json` — emit `{"ok": bool, "errors": [{code, message, field}], "warnings": [...]}`.
- `--strict` — treat warnings as failures.

Exit 0 clean / 1 on errors (or warnings under `--strict`) / 2 if the compose file fails to load.

### `corgi exec <service> -- <cmd> [args...]`

Run a one-off command in a service's resolved env + working dir; the child's exit code becomes corgi's. The service's `.env` is sourced the same way `start` commands get it.

Flags:
- `--json` — emit `{service, exitCode, durationMs}`; child output is routed to stderr so stdout stays pure JSON.
- `--ensure-deps` — wait for the service's `depends_on_db`/`depends_on_services` to be reachable first.
- `--ready-timeout <dur>` — cap that wait (default `15s`).
- `--service-dir <name=path>` — run from `path` instead of the compose `path:` (repeatable), e.g. a git worktree. The dir must exist.
- `--service-branch <name=branch>` — run on a branch via a reused worktree (see `corgi run`). Non-destructive.
- `--service-checkout <name=branch>` — run on a branch by in-place checkout (refuses on a dirty tree).

Unknown service exits 2 (`E_SERVICE_NOT_FOUND`); readiness timeout exits 1 (`E_READINESS_TIMEOUT`).

```
corgi exec api -- npm run migrate
corgi exec api --ensure-deps -- pytest -q
```

### `corgi test`

Run each selected service's `test` script (a script named `test` under `services.<name>.scripts`) in that service's env + working dir. Does **not** start anything. Services without a `test` script are skipped, not failed. Multi-command scripts run sequentially, stop on first non-zero exit.

Flags:
- `--service <name>` — only this service (unknown name exits 2).
- `--profile <name>` — narrow to a profile first.
- `--ensure-deps` / `--ready-timeout <dur>` — gate on dependency readiness.
- `--service-dir <name=path>` — test from `path` instead of the compose `path:` (repeatable), e.g. a git worktree. The dir must exist.
- `--service-branch <name=branch>` — test on a branch via a reused worktree (see `corgi run`). Non-destructive.
- `--service-checkout <name=branch>` — test on a branch by in-place checkout (refuses on a dirty tree).
- `--json` — emit `{"services": [{name, exitCode, durationMs, passed}|{name, skipped:true}], "passed": bool}`.

Exit 0 if all pass (skips don't count) / 1 if any fail / 2 on unknown `--service`.

### `corgi doctor` (aliases: `check`, `preflight`)

Preflight — synchronous, safe to run. Checks:
- Every tool in `required:` is installed (runs its `checkCmd`).
- Docker daemon is reachable.
- Every `port:` in the compose is free; if busy, lists the holding process.

Exits 0 / 1. No flags.

### `corgi config`

Show global user preferences from `~/.corgi/config.yml`.

```
corgi config       # current settings (notifications, schema version, file path)
corgi config path  # absolute path to the config file
```

The file is schema-versioned (`version: 1`). Older unversioned files auto-migrate on load. Reset everything: delete `~/.corgi/config.yml`.

### `corgi notifications` {#corgi-notifications}

Toggle desktop alerts fired when a service crashes during `corgi run`.

```
corgi notifications        # show current state
corgi notifications on     # enable
corgi notifications off    # disable
corgi notifications test   # fire one test toast (bypasses opt-in)
```

State is persisted in `~/.corgi/config.yml`. After enabling, `corgi run` will alert via `osascript` (macOS), `notify-send` (Linux), or PowerShell toast (Windows) when any managed service exits non-zero. Duplicate alerts within 30 s are throttled.

The first time `corgi run` exits with notifications still disabled, a one-line hint reminds users this command exists. Hint is silent in CI and silent once notifications are on.

### `corgi status` (aliases: `health`, `healthcheck`)

Post-run probe — synchronous, safe to run. TCP/HTTP probe every declared port. See `healthchecks.md`. Exits 0 / 1.

Flags:
- `-w, --watch` — re-probe continuously, alerts on transitions only (kubectl-style). Ctrl+C stops.
- `-i, --interval <dur>` — watch cadence (default `2s`).
- `-r, --ready` (alias `--until-healthy`) — exit 0 when every probed target is up; exit 1 on `--timeout`.
- `--timeout <dur>` — bound the wait for `--ready` (default `5m`).
- `--service <csv>` — narrow probes to listed services (matches both `services.<name>` and `db_services.<name>`).
- `--json` — machine output. One-shot: JSON array. Watch: NDJSON one-per-transition.
- `-q, --quiet` — suppress per-line output; rely on exit code only.

Common patterns:
- CI gate: `corgi status --ready --timeout 3m` blocks until stack ready, fails build on timeout.
- Single-service wait: `corgi status --ready --service api`.
- Live monitor: `corgi status --watch`.
- Pipeline: `corgi status --json | jq '.[] | select(.healthy==false)'`.

### `corgi ps` (alias: `processes`)

Runtime snapshot of a detached run. Reads + reconciles + persists
`corgi_services/.state.json`, falling back to a port probe where only a port is known.

- `--json` — array of rows: `{name, kind, port, status, url, startedAt}`. `port`,
  `url`, `startedAt` are `omitempty` (absent when zero). `startedAt` (RFC3339) gives
  uptime (`now − startedAt`); a service that never spawned is absent from the array.
- `status` reflects PID/container existence, **not** live readiness — use
  `corgi status` for the live TCP/HTTP probe. db_services go `running`/`stopped` only
  (never `crashed`).
- Docker-runner (pid 0) services are confirmed by a port probe; a container still
  booting (port not yet listening) keeps its status for a short grace, then a closed
  port marks it `stopped`.

### `corgi tunnel [services]`

Open public HTTPS tunnels to declared services. Default provider `cloudflared` (Cloudflare Quick Tunnels — free, no signup). Spawns one subprocess per target, prints URLs as they appear, blocks until Ctrl+C.

Flags:
- `--provider {cloudflared|ngrok|localtunnel}` — switch provider (default: cloudflared). CLI flag overrides compose `tunnel.provider`.
- `--port <int>` — tunnel raw local port (skip compose lookup)

Compose `services.<name>.tunnel:` block enables named/static mode (stable URL across restarts). Hostname `${VAR}` substitution reads (in order): shell env → `<service-dir>/.env` (runtime) → `env/source/<svc>.env` (source). Missing vars = strict error.

Auth-needing providers (e.g. ngrok) preflight before any tunnel spawns; corgi prints the exact login command and exits without partial state.

Full docs: [docs/tunnel.md](../../../../docs/tunnel.md).

### `corgi init` (aliases: `initialize`, `clone`)

One-shot setup: clone repos referenced by `cloneFrom:`, generate `corgi_services/db_services/<name>/docker-compose.yml` + `Makefile` per db, run `required:` installs. Idempotent — safe to re-run.

### `corgi create` (aliases: `add`, `new`)

Interactive CLI editor for adding a `db_services`, `services`, or `required` entry to the current compose file. Uses an interactive prompt — don't invoke from an agent unless you can feed stdin.

Reflection-driven prompts cover: scalars (string/int), `[]string` lists, nested structs, `*struct` (e.g. `tunnel:`), `*bool` (e.g. `autoSourceEnv`), `[]struct` (e.g. `depends_on_services`, `authUsers`, `subscriptions`), and `map[string]…` (key=value lines). Heterogeneous slices (`[]map`, `[]slice`) are skipped with a hint to edit yaml directly.

### `corgi db` (alias: `database`)

Database lifecycle helper. Flags:
- `-s, --stopAll` — stop all db containers
- `-u, --upAll` — start all db containers
- `--wait` — with `--upAll`: block until each db with a port accepts connections (replaces manual `sleep`). Hard-fails on timeout (`E_READINESS_TIMEOUT` under `--json`); good for CI gating.
- `-d, --downAll` — stop + remove all db containers **and their volumes** (`docker compose down --volumes`, consistent across all docker drivers; supabase uses `supabase stop`). Destructive: wipes local db data. Use `--stopAll` to keep data.
- `-r, --removeAll` — remove all db containers
- `--seedAll` — run seed scripts for all dbs

Without flags, opens an interactive menu — avoid from an agent.

#### `corgi db shell [service-name]`

Open an interactive shell inside the running container for a db_service. Credentials sourced from the corgi-compose config — no copy-paste passwords. The container must already be running (start it with `corgi run` or `corgi db --upAll`).

Flags:
- `-e, --exec <query>` — run a single query non-interactively, print result, exit with the tool's exit code. CI-friendly. Per-driver flag mapping: psql/ysqlsh `-c`, cockroach `-e`, mysql/mariadb `-e`, mssql `-Q`, mongosh `--quiet --eval`, cqlsh `-e`, redis-family appends the tokenized command. Example: `corgi db shell main-db -e "SELECT count(*) FROM users"`.

Driver → shell mapping:

| Drivers | Shell |
|---|---|
| `postgres`, `postgis`, `pgvector`, `timescaledb` | `psql` |
| `cockroachdb` | `cockroach sql --insecure` |
| `yugabytedb` | `ysqlsh` |
| `redis`, `redis-server`, `keydb`, `dragonfly`, `redict`, `valkey` | `redis-cli` |
| `mongodb` | `mongosh` (URI-encoded user/password) |
| `mysql`, `mariadb` | `mysql` |
| `mssql` | `sqlcmd` |
| `cassandra`, `scylla` | `cqlsh` |

Without an argument, opens an interactive picker of all `db_services` (avoid from an agent — needs stdin). With an argument, jumps straight to the named service. Container lookup is anchored exact-match on `<driver>-<serviceName>` so substrings can't pick the wrong container.

Examples:
```
corgi db shell              # picker
corgi db shell main-db      # open psql for db_services.main-db
```

Errors:
- `no interactive shell defined for driver "<x>"` — that driver isn't in the map. Connect manually with the generated env in `corgi_services/db_services/<name>/.env`.
- `container "<x>" is not running` — start it first: `corgi db --upAll` or `corgi run`.

#### `corgi db snapshot [name] [service]`

Physical snapshot of a Postgres data dir — the *built* state (indexes, populated matviews) as on-disk files. Restore recomputes nothing (vs a logical `dump.sql` seed that rebuilds indexes + re-runs `REFRESH MATERIALIZED VIEW`). Multi-hour reseed → minutes.

- Postgres family only: `postgres`, `postgis`, `pgvector`, `timescaledb`. Any other driver **fails first** (physical format is data-dir specific).
- Writes 2 files to `corgi_services/db_services/<service>/snapshots/`: `<name>.tar.zst` + `<name>.meta.json` (pg version, arch, image, sha256) — both required to be valid. Gitignored; survives `corgi clean` unless `-i snapshots`.
- `name` defaults to a timestamp. `service` optional when one postgres-family db, else required.
- How: clean-stop container → `docker cp` data dir out → zstd in-process → restart. No external tools.

Flags: `--force` (overwrite same name) · `--list` (list snapshots: name, pg, arch, size, age; honors `--json`) · `--rm <name>` (delete a snapshot, both files).

Share: copy the 2 files, then `corgi db restore <path>.tar.zst` (same arch + pg-major + image required).

#### `corgi db restore [name|path] [service]`

Restore a Postgres data dir from a snapshot, then bring the db up on already-built data — matviews included, zero recompute. `name` resolves under the service's `snapshots/`; or pass an explicit `.tar.zst` path (sibling `.meta.json` must be beside it).

- **Destructive**: wipes the current data volume. Prompts unless `-y`/`--yes`.
- Pre-flight, before any wipe: postgres-family driver (else fail first); snapshot pg-major + arch + image must match target (mismatch refused, both sides named; `--force` overrides). Always a decompress-probe; explicit-path snapshots also full-sha256-verified.
- Restored db keeps the **baked credentials** (`POSTGRES_*` apply only on init; compose-cred mismatch is warned). Matviews frozen as of snapshot time.

Flags: `-y, --yes` (skip confirm) · `--force` (override version/arch/image mismatch).

### `corgi logs` (alias: `log`)

Browse and follow per-service logs captured by `corgi run --logs`.

Without flags: two-step interactive picker — choose a service, then a run. The chosen log is streamed to stdout and tails new writes like `tail -f`. Auto-exits after the configured `--idle` window of inactivity (default 30s) or when the producing service stops (mtime-based heuristic). Ctrl-C exits at any time.

Flags:
- `--service <name>` — skip the service picker, jump straight to the run picker for `<name>`.
- `--all` — merge the newest run of every logged service into one timestamp-sorted stream. Lines are prefixed `[service]` so origin is clear. Best for crash forensics across services. Reads each file completely; for live multi-service tailing use separate terminals.
- `--idle <duration>` — exit when the file has been idle this long (default `30s`). Pass `--idle 0` to tail forever. Useful for db_services that idle for long stretches between writes.
- `--prune` — delete every captured log (`corgi_services/.logs/`).

Run picker labels show outcome at a glance:
- `2026-05-14T10-32-01.log  ✅ ok` — service exited cleanly
- `2026-05-14T10-32-01.log  ❌ crashed` — service exited non-zero
- `2026-05-14T10-32-01.log  ⏳ in-progress` — service still running (or corgi was killed mid-run)

Each line written by a corgi-managed service is prefixed with an RFC3339 UTC timestamp at log time, so log files from different services can be merged and correlated chronologically. `corgi logs` strips the prefix for single-file display.

`db_services` are captured differently: their containers run detached, so corgi follows `docker logs -f <driver>-<serviceName>` into the same file. Consequence — the file can include container output from before this `corgi run`, and db runs always show `⏳ in-progress` (the ✅/❌ status suffix tracks service-process exits, not followed containers).

Layout on disk: `corgi_services/.logs/<service>/<ISO-timestamp>.log`. Filenames sort chronologically. Each file is capped at 50 MB; the 10 newest runs per service are kept. Older files are pruned automatically by `corgi run --logs`.

The `.logs/` directory is auto-added to `corgi_services/.gitignore` on the first `--logs` run, so captures never get committed.

Examples:
```
corgi run --logs            # in one terminal — start the stack with capture on
corgi logs                  # in another terminal — pick a service + run
corgi logs --service api    # straight to api's runs
corgi logs --all            # merge newest run of every service, sorted by timestamp
corgi logs --idle 0         # tail forever (only Ctrl-C exits)
corgi logs --prune          # wipe all captures
```

Errors:
- `no log directories found under …/.logs/` — re-run with `corgi run --logs` first.
- `no log files found for <service>` — the service is logged but hasn't produced a file yet (very early in boot), or the name doesn't match.

### `corgi clean` (alias: `clear`)

Required flag: `-i, --items <db|services|corgi_services|all>`.

- `db` — stops + removes db containers
- `services` — removes cloned service repos (**destructive** — can drop uncommitted work)
- `corgi_services` — removes the generated `corgi_services/` folder
- `snapshots` — removes saved db snapshots (`corgi_services/db_services/*/snapshots/`)
- `all` — all of the above **except** `snapshots`

`corgi_services` and `all` preserve `snapshots/` dirs by default (a db snapshot is expensive to rebuild) — delete them deliberately with `clean -i snapshots`.

Confirm with the user before running `clean -i services`, `clean -i snapshots`, or `clean -i all`. It can delete cloned repos that have local changes. `clean` also `git worktree remove`s any worktrees corgi made for `--service-branch` (so source repos don't keep dangling entries).

### `corgi worktree` (alias: `wt`)

Manage the worktrees corgi creates for `run/exec/test --service-branch` (under `corgi_services/.worktrees/`).

- `corgi worktree list` — print each corgi-created worktree path.
- `corgi worktree prune` (alias `clean`) — `git worktree remove` them all and prune the source repos' admin entries. Safe to run anytime; recreated on next `--service-branch`.

### `corgi pull`

`git pull` in every service directory. No flags.

### `corgi script` (aliases: `scripts`, `commands`, `asdf`, `asd`)

Run named scripts declared under `services.<name>.scripts`.

- `-n, --names <list>` — comma-separated script names
- `--services <list>` — restrict to specific services
- `--ignore-dependent-services` — skip running on dependents
- `--continue-on-error` — run the script across all matching services, print a pass/fail summary, and exit non-zero if any failed (replaces hand-rolled lintAll/testAll loops). Without it, exit code is unchanged (0).

### `corgi fork`

Fork service repos to a new GitHub/GitLab account.
- `--all` — fork every service
- `--private` — create private forks
- `--useSameRepoName` — keep original repo names
- `--gitProvider <github|gitlab>`

Interactive — requires user auth.

### `corgi list`

Print all globally-registered compose paths that have been run (corgi tracks them). `--cleanList` clears the registry.

### `corgi docs` (alias: `doc`)

Prints schema reference to stdout. `--generate` regenerates cobra docs (maintainer-only).

### `corgi upgrade` (alias: `update`)

Upgrade via Homebrew to the latest GitHub release. Safe no-op if already current.

### `corgi version` (alias: `-v`, `--version`)

Prints version string, exits 0.

### `corgi help`

Same as `-h` / `--help`. Per-command help available via `corgi <cmd> -h`.

## Choosing the right command in common situations

- User asks **"what's wrong before I start?"** → `corgi doctor`.
- User asks **"is everything running / healthy?"** → `corgi status`.
- User asks **"stop the databases"** → `corgi clean -i db` (non-destructive to volumes in most drivers — verify first if user has irreplaceable data).
- User wants to **try an example** → `corgi run -l` then `corgi run -t <url>`.
- User wants to **reset the whole local state** → confirm scope, then `corgi clean -i corgi_services` (safe) or `-i all` (destructive).
- User asks **"how do I expose <service> publicly for webhooks?"** → `corgi tunnel <service>` (or `corgi tunnel` for all services). Default = Cloudflare Quick Tunnels (no signup). Don't recommend ad-hoc `ngrok http …` — `corgi tunnel --provider ngrok <service>` reuses the same flow + login preflight.
