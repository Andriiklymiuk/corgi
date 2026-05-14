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
| `--describe` | Print a human summary of the compose file, don't run |
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
- `--tunnel` — open public HTTPS tunnels alongside the stack for every service with a `tunnel:` block. Equivalent to a parallel `corgi tunnel`, bundled into the one process.
- `--logs` — persist stdout/stderr of every service and db_service to `corgi_services/.logs/<name>/<timestamp>.log`. Capped 50 MB per file, keeps 10 newest runs per service, older pruned automatically. A `.logs/` entry is auto-added to `corgi_services/.gitignore`. Read back with `corgi logs`.
- `--ci` — CI mode: suppress spinners, banners, color output. Plain log lines only. Auto-enabled when any common CI environment variable is set: `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`, `BUILDKITE`, `JENKINS_URL`, `TEAMCITY_VERSION`, `TRAVIS`, `DRONE`, `BITBUCKET_BUILD_NUMBER`, `CODEBUILD_BUILD_ID`. Pair with `--runOnce` for pipelines.
- `--notify` (default `true`) — send a desktop notification when a service exits non-zero (and corgi is not shutting down). Requires a one-time opt-in via `corgi doctor`; never fires on Ctrl-C. Duplicate notifications with the same title+body are throttled to one per 30 seconds so a crash-looping service can't spam the desktop. Pass `--notify=false` to silence per-run. macOS uses `osascript`, Linux `notify-send`, Windows PowerShell toast.

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
- `-d, --downAll` — stop + remove all db containers
- `-r, --removeAll` — remove all db containers (preserves volumes? verify before destructive use)
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
- `all` — all of the above

Confirm with the user before running `clean -i services` or `clean -i all`. It can delete cloned repos that have local changes.

### `corgi pull`

`git pull` in every service directory. No flags.

### `corgi script` (aliases: `scripts`, `commands`, `asdf`, `asd`)

Run named scripts declared under `services.<name>.scripts`.

- `-n, --names <list>` — comma-separated script names
- `--services <list>` — restrict to specific services
- `--ignore-dependent-services` — skip running on dependents

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
