---
name: run
description: Use when the user wants to bring up a corgi-compose stack — start, launch, run, spin up, or boot the whole stack or a slice (e.g. "run the stack", "run with tunnel and logs", "run web + mobile against the remote backend", "run just the api", "run for the android emulator"). Boots detached, waits until healthy with a timeout, flags anything stuck, reports URLs + how to stop. NOT for authoring corgi-compose.yml (use the corgi skill), shipping tickets/features (stories), or diagnosing an already-broken stack (use the debug skill).
---

# Corgi run

Bring up a corgi stack — or a slice — from chat. Boot **detached**, wait until
**healthy (timeout)**, flag stuck, report URLs + how to stop. What runs and how =
read from the repo (`corgi-compose.yml`, `Makefile`, `README`); never hard-coded,
never company-specific.

## Guardrails (non-negotiable)

- **Detached only — never a synchronous foreground `corgi run`.** Blocks
  indefinitely streaming logs; a sync Bash call hangs until the 10-min timeout and
  truncates boot output. Use `--detach`; on a cold stack launch with
  `run_in_background: true` (Phase 2). See `../corgi/references/long-running.md`.
- **Never start a service the user wants pointed remote** — a local run overrides
  the remote URL.
- **Don't `--force`/`restart` a live run without asking** — `corgi ps` first.
- **Diagnosis = the debug skill.** Boot + quick stuck-triage here; real
  investigation → `debug`.

## Phase 0 — Locate + first-run preflight

cwd must hold `corgi-compose.yml` (`ls corgi-compose.yml *.corgi-compose.yml`). None
→ tell the user to open the stack folder; don't guess a layout. Read only needed
keys — `services.<name>.{path,cloneFrom,depends_on_services,depends_on_db,start,port,
tunnel,manualRun,runner}`, `db_services`, `envTiers`, `useAwsVpn`, `useDocker`,
`init`, `required` (schema: `../corgi/references/yml-schema.md`).

- **First run → `corgi init` first.** `corgi run` clones missing repos + runs the
  *compose-level* `beforeStart`, but NOT the `init:` block (env gen, config copy) or
  `required:` installs — those run only on `corgi init`. Most stacks: run target =
  `corgi init && corgi run`. Service dirs missing OR env files / generated config
  absent → `corgi init` first (cold machine = long: clones + installs).
- **corgi auto-handles VPN + Docker.** Compose sets `useAwsVpn`/`useDocker` (or any
  service uses the `docker` runner) → `corgi run` starts VPN + Docker itself in
  preflight. Don't add a manual VPN/docker step.

## Phase 1 — Resolve intent → command

Sources, priority: **`corgi-compose.yml` authoritative** (services/dbs/tunnels/tiers
+ each `start:`); **`Makefile` targets** = the team's named shortcuts
(`grep -E '^[a-zA-Z][a-zA-Z0-9_-]*:' Makefile`); **`README`** = remote URLs + host
quirks. Match the user's words → translate to a **detached** command.

| Intent | Command |
|--------|---------|
| Whole stack | `corgi run --detach` |
| A subset | `corgi run --services <a,b> --detach` (`--with-deps` pulls each one's deps + dbs) |
| Frontend(s) → **remote** backend | `corgi run --services <frontends> --dbServices none --detach`. Backend excluded → its `depends_on_services` alias is **not** generated — the frontend uses ONLY its own env file. **Read that env file** (`Read` the frontend's source `.env`) and **confirm the API URL is the remote one**, not a `localhost` default `corgi init` seeded; if localhost, user sets the remote URL there before you declare success. (Don't use `corgi env <frontend>` for this — it ignores the run set and annotates vars with `depends_on` sources for the backend you excluded, so it misleads.) |
| + captured logs | add `--logs` |
| + webhook tunnel | can't combine with `--detach` — see **Tunnel**, Phase 2 |
| Staging/prod tier | `--tier <name>` if `envTiers:` defines it; else the repo's `run<X>Staging` make target's underlying corgi command (tier run usually `--dbServices none` — no local dbs) |
| One DB only | `corgi run --dbServices <name> --services none --detach` |

**Names match compose keys exactly.** `--services`/`--dbServices` = exact-name match.
A Makefile target may pass a non-key name (e.g. `--dbServices supabase` when the key
is `<name>-supabase` → selects **nothing**, stack boots with a dead db URL). Resolve
every name against `corgi-compose.yml` keys; or `--with-deps` to let corgi resolve
the db closure from `depends_on_db` instead of hand-listing.

**Omitting a dependency from a hand-picked slice hard-fails preflight** with
`E_DANGLING_DEP: service "<x>" depends on unknown service/db_service "<y>"` — corgi
validates the *whole* selected graph, and `--services`/`--dbServices` do **not**
auto-pull what the named services `depends_on`. Don't chase it by hand-listing every
dependency; `corgi run --services <x> --with-deps --detach` resolves the full
`depends_on` closure (services + their dbs) in one command. A `runOnly<X>` make target
that hand-lists `--dbServices … --services <x>` without `--with-deps` dangles the
moment `<x>` gains a new `depends_on` — prefer `--with-deps`.

**Carry the Makefile target's env-var prefix verbatim.** `WEB_DEV_CMD=dev:host
MOBILE_PLATFORM=android corgi run --services …` sets *process* env vars a service's
`start:` reads (`yarn ${WEB_DEV_CMD:-dev}`, `bun ${MOBILE_PLATFORM:-ios}`). corgi
can't inject these (sources only the env *file*), so dropping the prefix silently
changes behavior — and `corgi env` won't show them (process env, not env-file). Grep
the matched `start:` for `${VAR` placeholders; reproduce every prefix the target sets.

**Host (mobile/device/emulator).** Default `localhost`. `corgi run --host <ip>` =
blanket replace of every `localhost` in the service's generated `.env` — the API URL
AND its db-dependency URLs (e.g. Supabase). The db *containers* stay bound to the
host; only the URLs the service consumes are rewritten. Does NOT launch a
simulator/emulator, does NOT change `start:`.
- **Android emulator** → literal `--host 10.0.2.2` (host-loopback alias) so both API
  and supabase resolve from inside the emulator — not `auto`.
- **Real device / LAN** → an explicit IP the user names; API + any db it points at
  must be reachable on that interface. `--host auto` = first physical LAN IPv4
  (en0/en1/eth0/wlan0), **skips VPN/Docker/tunnel interfaces** → a VPN address won't
  be picked by `auto`; pass it explicitly.
- A "mobile" service whose `start:` is `expo start --web` / `bun web` is a **web**
  target — corgi only gives you that. Real native run:
  `corgi run --services <mobile> --with-deps --host 10.0.2.2 --detach` (`--with-deps`
  lets corgi resolve the exact db key from `depends_on_db` — a hand-typed
  `--dbServices` token must be the exact compose key or it selects nothing; see the
  name rule above) for env + deps, then launch the native build yourself
  (`cd <mobile> && expo run:android`) or via a corgi script if one exists. corgi
  brought up env + deps + the **web** bundler only — the emulator launches separately
  and is **not** covered by the ready gate; don't claim it's running off a green web
  port.
- Verify a host rewrite by **Reading the written `<mobile-dir>/.env`** (Read
  `limit: ~40`) — `corgi env` does NOT apply `--host` (shows localhost by design).
  Use `corgi env` only for the non-host remote-backend case above.

### Run a branch / worktree — a feature/PR branch or an existing worktree

Run a service from **code other than its compose `path:`** — a feature/PR branch, or
an existing worktree dir — without touching the main checkout. Per-service,
repeatable, `<svc>=<value>`, mixable (named svcs from branch/dir, the rest from
compose `path:`); combine with `--detach`, `--with-deps`, `--services`.
**Flag-existence guard** (newer flags): `corgi run --help | grep service-branch` —
missing → no fallback; use `git checkout <branch> && corgi run --services <svc>`.

- **Committed/pushed branch** — `--service-branch <svc>=<branch>`. corgi makes (or
  reuses) its **own non-destructive worktree** off that branch under
  `corgi_services/.worktrees/`; main checkout untouched. Reviewer-facing shape
  (the `stories` Phase 5 "Run line"). Branch must exist locally; source must be a git
  repo (clones first if needed). **Gotcha** — fails if that branch is currently
  checked out in the main repo (git one-branch-one-worktree) → switch the main repo
  off it. Fresh worktree has **no `node_modules`** (gitignored) → first run needs the
  service's `beforeStart` (e.g. `npm install`); reused worktrees keep it.
- **Existing worktree dir** — `--service-dir <svc>=<path>`. Runs that exact dir's env
  + beforeStart/afterStart + process; pure cwd swap, no git touch, no clone. Dir must
  exist. For **uncommitted/live** code in a known worktree (`stories` Phase 3 shape,
  `/tmp/corgi-wt/<wt-id>-<svc>`). (`--service-checkout <svc>=<branch>` = in-place
  checkout of the service's own dir; refuses on a dirty tree, leaves the dir on that
  branch.)
- `--with-deps` only expands the `--services` set through `depends_on` — it does
  **not** auto-worktree pulled-in deps; you get a worktree/dir only for svcs you name.

```
# impacted services on a pushed branch (reviewer Run line)
corgi run --with-deps --detach \
  --service-branch api=feature/ABC-200/user-phone \
  --service-branch web=feature/ABC-200/user-phone
# live worktree code for some, compose path: for the rest
corgi run --detach \
  --service-dir api=/tmp/corgi-wt/ABC-200-api \
  --service-dir web=/tmp/corgi-wt/ABC-200-web
# admin, worker, db_services (unnamed) run from their compose path:
```

**Cleanup** (only `--service-branch` worktrees; `--service-dir`/in-place checkouts
leave nothing): `corgi worktree list` shows corgi-made worktrees, `corgi worktree
prune` removes them + their git entries (`corgi run --fromScratch` / `corgi clean`
prune too).

## Phase 2 — Launch detached

```
corgi run <flags> --detach [--logs] --json
```

**Not instant on a cold stack.** `corgi run --detach` runs every service's
`beforeStart` (bundle/yarn install, migrations, webpack, image pulls)
**synchronously before it writes state and returns** — minutes to tens of minutes on
a first run. Launch with `run_in_background: true` (or Bash `timeout` ≥ 600000ms),
poll `corgi ps --json`; don't assume a quick return or treat the wait as a hang.

**Output:** `--detach --json` prints the run-state object — `services[]` each
`name/pid/port/status`, plus `dbServices[]`; a service crashed on spawn shows
`status:"crashed"` in that array (no separate `failed[]`). `E_ALREADY_RUNNING` → a
run is already live: `corgi ps` to show it; ask before `--force`/`corgi restart`.
(Stale `.state.json` after a crash also triggers it — `corgi ps` reconciles,
`--force` clears it.)

**Tunnel** (if requested): `--tunnel` can't combine with `--detach`. Detach
services first, then — only after confirming the tunnel's hostname env resolves
(`corgi env <svc>`, from `tunnel.hostname: ${VAR}`) AND provider auth is done
(ngrok/cloudflared; `corgi doctor`) — background `corgi tunnel <svc>`
(`run_in_background: true`). Standalone `corgi tunnel` **hard-fails (exit 1)** on a
missing hostname var or missing auth (unlike `corgi run --tunnel`, which only
skips-with-warning) — don't background one that will die. After backgrounding, read
the shell output until `✓ <svc> :PORT → https://…` (live) or `✗ …`/process-exit
(died — fix env/auth); only then report the public URL.

## Phase 3 — Ready gate (with a timeout)

```
corgi status --ready --json --timeout <T> [--service <started ones>]
```

`<T>` = a Go duration — always `120s`/`300s`/`10m`, never a bare number. Size it:
warm stack ~`120s`; a first cold boot (clones repos / pulls images / builds a venv)
**7–10 min+** → `600s+`, or skip the strict gate and poll `corgi status --json`
every ~30s comparing the healthy count (rising = progress, not stuck). A multi-hour
DB dump/restore (`make initDatabase`-style) is **separate, out-of-scope** — never
block the gate on it.

- **Healthy (exit 0)** → report. Uptime (`startedAt`)/ports from `corgi ps --json`.
  **But the gate only probes targets with a declared `port:`.** No port-bearing
  service in the started slice (status prints *"No services with ports declared —
  nothing to check"*, exits 0) → the gate verified **nothing**: fall back to
  `corgi ps --json` (status running + a live pid) + a bounded
  `corgi logs --service <x> --idle 2s`. A port-less db whose port comes from a driver
  default (notably the `supabase` driver) is invisible to the gate even when others
  are probed — cross-check out-of-band (`curl` its health URL / `corgi ps --json` /
  `docker ps`). **Never report ready on a no-op exit 0.**
- **Timeout (exit 1) — don't hang, don't retry blindly.** Final JSON lists each
  target `healthy:false`. Before handing off, separate **crashed-after-boot** from
  **never-opened-its-port**: `corgi ps --json` `status:"crashed"` only for
  pid-tracked services — db_services and docker-runner/pid-0 services never show
  "crashed", judge those by `corgi status` `healthy:false` + `docker ps`. Triage:
  `corgi doctor` + a bounded `corgi logs --service <X> --idle 2s`. Name the likely
  cause, pass the crashed-vs-never-opened distinction along, **hand a real
  investigation to the `debug` skill**.
- **Partial ready** → backends healthy but a frontend bundler (Expo/Vite first
  build) still warming: report what's up + what's warming rather than failing the
  whole gate; re-poll the warming one.

## Phase 4 — Report

- What's up (services + ports + URLs), what's down/warming.
- Some services open a browser themselves in `start:` (`open -a Chrome <url>`) — note
  it so the user isn't surprised (harmless headless).
- Watch logs: `corgi logs --service <x>` (detached always captures logs). Stop:
  `corgi stop` (or `corgi stop --service <x>`; runs `afterStart`, brings dbs down).
  Unhealthy → "run the **debug** skill / `/corgi-debug`".
