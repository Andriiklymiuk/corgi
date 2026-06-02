---
name: long-running
description: How to invoke `corgi run` (and other long-running corgi commands like `corgi tunnel`) from inside a Claude Code agent loop without hanging the shell. Covers the detached lifecycle (run --detach → ps → stop/restart). Read before ever running them in bash.
---

# Running long-running corgi commands safely from an agent

Two shapes:

- **Foreground** `corgi run` / `corgi tunnel` — block indefinitely, stream logs, watch the compose file. Synchronous in a Bash call → agent hangs until the 10-min timeout. Background them, OR prefer `--detach`.
- **Detached** `corgi run --detach` — starts services in process groups that outlive corgi, persists state, returns immediately. The modern, agent-friendly path. Pairs with `corgi ps` / `corgi stop` / `corgi restart` / `corgi logs`.

For agents: KillShell still works for a backgrounded foreground run, but `--detach` + `corgi stop` is cleaner — no orphaned shell, survives across sessions, and `corgi ps` gives real status.

## Detached lifecycle (preferred)

```
corgi run --detach          # or -d; spawns detached pgroups, returns immediately
corgi ps                    # what's up — reads .state.json, reconciles PIDs/containers
corgi restart               # stop + run --detach
corgi stop                  # tear it all down
```

### `corgi run --detach` / `-d`

- Each service → its own detached process group that survives corgi exiting.
- Persists run-state to `corgi_services/.state.json`, prints a startup summary (JSON with `--json`), returns immediately. No streaming, no watch.
- `--tunnel` cannot combine with `--detach` (tunnels run in-process) — run `corgi tunnel` separately.

Use this instead of `Bash(corgi run, run_in_background: true)` + KillShell.

### `corgi ps` (alias `processes`)

```
corgi ps
corgi ps --json
```

- Reads `.state.json`, reconciles recorded PIDs / db containers, falls back to a port-listening probe where only a port is known.
- Use this instead of "background then `corgi status`".

### `corgi stop [--service <name>]`

```
corgi stop                  # whole stack
corgi stop --service api    # one service, leave the rest running
```

- SIGTERM (→ SIGKILL after 5s) the detached groups, runs `afterStart:` hooks, brings db containers down.
- Idempotent: no run-state / nothing running → prints "nothing to stop", exits 0.

### `corgi restart [--service <name>]`

```
corgi restart
corgi restart --service api --json
```

- `corgi stop` then `corgi run --detach`. `--detach`/`--force` always on.
- `--json` → single startup-summary object (same shape as `run --detach --json`).
- Single-service restart refuses a service that wasn't in the current detached run.

### Logs

```
corgi run --logs            # persist stdout/stderr while running
corgi logs                  # browse/follow afterwards (alias: log)
```

- `--logs` writes `corgi_services/.logs/<service>/<timestamp>.log`, keeps last 10 runs/service (older pruned).
- `corgi logs`: interactive picker → follows like `tail -f`. Flags: `--service <name>`, `--all` (merge newest run of every service, timestamp-sorted), `--prune` (delete all `.logs/`), `--idle <dur>` (exit after dead-air; `0` = tail forever).

### Already-running guard

Second `corgi run --detach` while a run is live → aborts:

```
exit token: E_ALREADY_RUNNING
"corgi is already running for this project — stop or restart first (use --force to override)"
```

`--force` clears stale state (kills any lingering groups + docker runners) and starts anyway.

### CI / JSON

- `corgi run --ci` — suppress spinners/banners/color, implies `--silent`. Auto-on when `CI=true`. Pair with `--runOnce`.
- Global `--json` — machine-readable output for `run` / `ps` / `stop` / `restart` / `logs`.

## Foreground patterns (still valid)

`corgi run` and `corgi tunnel` block and stream until killed. Background them or hand off to the user.

### Pattern A — background, probe with `corgi ps`

```
Bash(command: "corgi run", run_in_background: true)   # returns shell ID; let it boot
Bash(command: "corgi ps")                             # synchronous; what's up
```

- `corgi ps` shows port/PID status without touching the running process.
- Kill the background shell with `KillShell` on that shell ID. Don't orphan it across sessions.
- The background shell lives only while the session is alive. (Prefer `--detach` if it must outlive you.)

### Pattern B — hand off to the user's terminal

If the user is actively developing, or a service uses `interactiveInput: true` (needs a real TTY):

> Run `corgi run` in a separate terminal. I'll `corgi ps` to check health.

Then use the synchronous commands (`doctor`, `ps`, `status`, `clean`, `db`, `pull`, `script`) from the agent.

## `corgi tunnel`

One tunnel subprocess per service with a resolvable `tunnel:` block. Blocks until Ctrl+C (SIGINT/SIGTERM). `corgi run --tunnel` bundles tunnels into the stack (foreground only — not with `--detach`). Same hang risk → background or hand off.

## `--runOnce` / `-o`

Runs the loop once and exits instead of watching. But **services still run their `start:` commands** — if those are long-running (`npm run dev`), `--runOnce` doesn't help. Only useful when every `start:` terminates on its own (e.g. a batch script). For CI, pair `--runOnce --ci`.

## Clean shutdown

- **Foreground:** SIGINT/SIGTERM → kills children, runs `afterStart:`, exits 0. SIGHUP → reloads on compose change. From the agent, `KillShell` the background shell (SIGINT → SIGTERM).
- **Detached:** `corgi stop` (runs `afterStart:`, brings db down). Don't KillShell a detached run — there's no shell to kill; use `corgi stop`.
- If a db container hangs, `docker ps` + `docker kill` it manually. Warn the user.

## What to avoid

- **Never** run a foreground `corgi run` synchronously in a Bash call — boot output gets truncated at the timeout. Use `--detach`, or background + `corgi ps`.
- **Never** pipe a foreground `corgi run` into `head`/etc. — pipe close kills it and your services die.
- **Never** orphan a background `corgi run` shell across sessions without telling the user — silent Go process binding ports.
- **Never** leave a detached run up silently — `corgi ps` to see it, `corgi stop` to clear it.

## TL;DR

- Boot + keep it up for later steps / across sessions → `corgi run --detach` → `corgi ps` → `corgi stop`.
- Quick "does it boot?" inside one session → background `corgi run` + `corgi ps` + KillShell.
- User keeps working → hand off to a separate terminal.
- CI "run then exit" → `corgi run --runOnce --ci` (only if all `start:` terminate).
