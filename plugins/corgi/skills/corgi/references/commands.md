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

### `corgi run` (aliases: `start`, `r`)

Long-running. Starts all db_services + services concurrently, streams logs. **Do not invoke synchronously** — see `long-running.md`.

Notable flags:
- `-s, --seed` — run seed scripts after db boot
- `--omit <list>` — comma-separated service names to skip
- `--services <list>` — whitelist only these services
- `--dbServices <list>` — whitelist only these dbs
- `--pull` — `git pull` in service dirs before starting
- `--no-watch` — disable auto-reload on compose file change

### `corgi doctor` (aliases: `check`, `preflight`)

Preflight — synchronous, safe to run. Checks:
- Every tool in `required:` is installed (runs its `checkCmd`).
- Docker daemon is reachable.
- Every `port:` in the compose is free; if busy, lists the holding process.

Exits 0 / 1. No flags.

### `corgi status` (aliases: `health`, `healthcheck`)

Post-run probe — synchronous, safe to run. TCP/HTTP probe every declared port. See `healthchecks.md`. Exits 0 / 1. No flags.

### `corgi tunnel [services]`

Open public HTTPS tunnels to declared services. Default provider `cloudflared` (Cloudflare Quick Tunnels — free, no signup). Spawns one subprocess per target, prints URLs as they appear, blocks until Ctrl+C.

Flags:
- `--provider {cloudflared|ngrok|localtunnel}` — switch provider (default: cloudflared)
- `--port <int>` — tunnel raw local port (skip compose lookup)

Auth-needing providers (e.g. ngrok) preflight before any tunnel spawns; corgi prints the exact login command and exits without partial state.

When `api` is among the targets, corgi auto-prints the DocuSeal webhook path (`<url>/webhooks/docuseal`) as a hint.

Full docs: [docs/tunnel.md](../../../../docs/tunnel.md).

### `corgi init` (aliases: `initialize`, `clone`)

One-shot setup: clone repos referenced by `cloneFrom:`, generate `corgi_services/db_services/<name>/docker-compose.yml` + `Makefile` per db, run `required:` installs. Idempotent — safe to re-run.

### `corgi create` (aliases: `add`, `new`)

Interactive CLI editor for adding a `db_services`, `services`, or `required` entry to the current compose file. Uses an interactive prompt — don't invoke from an agent unless you can feed stdin.

### `corgi db` (alias: `database`)

Database lifecycle helper. Flags:
- `-s, --stopAll` — stop all db containers
- `-u, --upAll` — start all db containers
- `-d, --downAll` — stop + remove all db containers
- `-r, --removeAll` — remove all db containers (preserves volumes? verify before destructive use)
- `--seedAll` — run seed scripts for all dbs

Without flags, opens an interactive menu — avoid from an agent.

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
