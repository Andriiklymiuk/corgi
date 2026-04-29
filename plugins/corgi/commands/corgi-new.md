---
description: Scaffold a new corgi-compose.yml for the current project by inspecting existing service directories and asking a few targeted questions.
---

You are helping the user create a `corgi-compose.yml` in their current working directory.

## Step 1 — Survey the project

Before asking anything, look around:

- `ls` the current directory. Note every subdirectory that looks like a service (has its own `package.json`, `go.mod`, `Cargo.toml`, `requirements.txt`, `Gemfile`, etc.).
- `cat` each service's manifest to infer language/runtime and likely port.
- Check for existing `.env`, `docker-compose.yml`, `Makefile` — pull defaults from them.
- Check for an already-existing `corgi-compose.yml`. If present, stop: ask the user whether they want to edit it (offer `/corgi-add-service` if one day that exists, otherwise read `skills/corgi/references/yml-schema.md` and edit directly) rather than overwrite.

Present what you found in 3–5 bullets. Don't proceed blindly.

## Step 2 — Ask only what you can't infer

Ask one question at a time. Skip anything obvious from Step 1. Likely questions:

- **Which databases?** (User may say "postgres" — use references/db-drivers.md to pick the exact driver, default port, env prefix.)
- **Any AWS services needed locally?** (If yes → use `driver: localstack` with `queues:` / `buckets:`.)
- **Which services need to wait on which dbs?** (Becomes `depends_on_db:` with `envAlias`.)
- **Any cross-service calls?** (Becomes `depends_on_services:`.)
- **Any shared secrets or vars one service must publish to another?** (Becomes `exports:` on producer, `${producer.VAR}` reference inside consumer's `environment:`.)
- **Are services in this repo, or should corgi clone them?** (Sets `path:` vs `cloneFrom:`.)

Don't ask about flags the user would never think about (e.g. `manualRun`, `runner.name`) — set sane defaults and note them inline.

## Step 3 — Generate the yml

Use `skills/corgi/references/yml-schema.md` for exact key names. Structure:

```yaml
name: <project>
description: <one line>

required:
  docker:
    why: [container runtime for dbs]
    install: [brew install --cask docker]
    checkCmd: docker --version

db_services:
  # one block per db

services:
  # one block per service
```

Rules of thumb:
- Add `healthCheck:` on every service that has a `/health` or `/_health` route. Otherwise omit (TCP probe is fine).
- Use `envAlias: ""` for the primary db of a service; only set an alias when the service talks to more than one db of the same driver.
- For `services` where the repo is already cloned locally, use `path: ./<dirname>` and omit `cloneFrom:`.
- Don't invent `start:` commands — read each service's `package.json` scripts / Makefile / README to find the right one.

## Step 4 — Verify before handoff

1. Write the file to `corgi-compose.yml` (or user-specified path).
2. Run `corgi doctor` synchronously. Report results.
3. If doctor passes, tell the user:
   > `corgi-compose.yml` is ready. Run `corgi run` in a separate terminal to start everything. Come back and I'll `corgi status` to verify health.

Do **not** run `corgi run` yourself unless the user explicitly asks and understands it will occupy a background shell — see `skills/corgi/references/long-running.md`.

## If the user gets stuck

- Missing tools → `skills/corgi/references/debugging.md`.
- Can't pick a driver → show the relevant rows from `skills/corgi/references/db-drivers.md` with a recommendation.
- Existing `.env` has values corgi would overwrite → set `ignore_env: true` on that service and explain the trade-off.
