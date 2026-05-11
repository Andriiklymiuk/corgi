---
name: corgi
description: Author corgi-compose.yml files, run and debug corgi projects. Use when a corgi-compose.yml is present, when the user mentions corgi (the CLI tool), or when the user asks to start a multi-service project that uses corgi. Corgi is a Go CLI (`brew install andriiklymiuk/homebrew-tools/corgi`) that spins up databases, services, and required tools from one yml file — think docker-compose for services plus databases plus tool checks.
---

# Corgi

Corgi (https://github.com/Andriiklymiuk/corgi) runs multi-service projects from a single `corgi-compose.yml`. It handles: cloning service repos, starting databases in Docker, seeding from dumps, generating `.env` files with cross-service URLs, and running all services concurrently.

When this skill activates, you are the expert on corgi. Do not fall back to generic docker-compose / npm advice if a `corgi-compose.yml` is present — corgi is the authoritative entry point for that project.

## Critical: `corgi run` is long-running

`corgi run` blocks indefinitely, streams logs, and has no `--detach` flag. **Never run it synchronously** — it will hang your shell. See `references/long-running.md` before invoking it.

Safe synchronous probes:
- `corgi doctor` (alias `check`) — preflight: tools installed, Docker up, ports free
- `corgi status` (aliases `health`, `healthcheck`) — post-run: TCP/HTTP probe each port

Both exit 0 on success, 1 on failure. Output is colored text, not JSON.

`corgi tunnel` is also long-running (one tunnel subprocess per service, blocks until Ctrl+C). Background it the same way you background `corgi run`. See `references/long-running.md` if invoking from an agent.

## Routing: when to read what

| Task | Read |
|------|------|
| Writing a new `corgi-compose.yml` | `references/yml-schema.md` then `references/db-drivers.md` |
| Picking a db driver (port, image, env prefix) | `references/db-drivers.md` |
| Adding `healthCheck:` to a service or db | `references/healthchecks.md` |
| `corgi doctor` or `corgi run` failed | `references/debugging.md` |
| Explaining / choosing a CLI flag | `references/commands.md` |
| Setting up webhook tunnels (Stripe/GitHub/e-sign/etc.) | `../../../docs/tunnel.md` (full) or `references/commands.md#corgi-tunnel-services` |
| Producing a service map / relation diagram for the project | run `/corgi-describe` (see `references/describe-output.md`) |
| Running `corgi run` inside an agent loop | `references/long-running.md` |

Load only what the task needs. Do not read every reference every time.

## Common workflows

**Fresh project from scratch:** use the `/corgi-new` slash command, or: write `corgi-compose.yml` → `corgi doctor` → start `corgi run` in background → `corgi status`.

**Document an existing project:** `/corgi-describe` parses `corgi-compose.yml` and writes a detailed Markdown doc (services, dbs, env wiring, tunnels, scripts) plus a Mermaid relationship diagram to `docs/corgi-services.md`. Read-only — does not touch services. Built-in `corgi --describe` only prints per-service JSON during parse (and does **not** short-circuit — the underlying command, e.g. `run`, still executes); the slash command is the richer, side-effect-free alternative.

**Existing repo with `corgi-compose.yml`:** this file is the single source of truth for how services start. Do not invent `npm run dev`, `docker compose up`, or per-service shell commands — use `corgi run`. Look at `db_services:` to know what databases exist and at `services:` to know what service repos are expected.

**User says "start the project" / "run the backend":** check for `corgi-compose.yml` first (`ls corgi-compose.yml` or `ls *.corgi-compose.yml`). If present, corgi is the answer.

## Quick command cheatsheet

```
corgi run                  # start everything (long-running, background it)
corgi doctor               # preflight
corgi status               # health check (one-shot)
corgi status --ready       # block until all healthy / timeout (CI-friendly)
corgi status --watch       # live monitor, transitions only
corgi tunnel               # public HTTPS tunnels (long-running) — default cloudflared
corgi init                 # scaffold db_services/ + cloned repos
corgi create               # interactive yml editor
corgi clean -i db          # stop+remove db containers (also: services, corgi_services, all)
corgi pull                 # git pull in every service dir
corgi version              # show installed version
corgi --describe           # built-in: per-service JSON during parse; does NOT short-circuit. Use /corgi-describe for a rendered doc + Mermaid diagram
```

Full surface in `references/commands.md`.
