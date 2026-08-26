# Getting it running on a real project

The examples use public repos. Real workspaces have private repos, prerequisites, and first-run hiccups — here's the honest version.

**What you need:** `git`, and Docker (only if you declare `db_services`). Everything else lives in your project's `required:` block. Homebrew is just one way to install corgi itself, not a requirement. corgi runs on macOS, Linux, and Windows (PowerShell or WSL), so a mixed-OS team shares the same `corgi-compose.yml`.

## The `required:` block — a runnable record of what the project needs

That block is more than a checklist — it's a committed, runnable record of everything the workspace needs. Each entry has:

- `why:` — so teammates know what it's for
- `checkCmd:` — how to verify it (check a specific version here if you want)
- `install:` — the commands to get it

`install:` runs whatever it takes: a `brew install`, a `pyenv`/`rbenv` install to pin Python 3.12 or Ruby 3.4, a native lib, a cert via `mkcert -install`. `corgi doctor` runs every `checkCmd`; `corgi doctor --fix` runs the `install:` steps for you — so "what do I need installed?" is answered in the file, not a wiki.

## Private repos just work

corgi clones with plain `git`, so your existing SSH keys or credential helper are used as-is — private GitHub/GitLab services clone fine if your `git` is already set up. There's no corgi-specific auth to configure.

## Joining a team that already uses corgi

`git pull`, then `corgi run`. No `corgi-compose.yml` yet? You don't have to hand-write it — `corgi create` (or `/corgi-new` with Claude) inspects the repos and scaffolds one. Adding corgi is a single committed file, and teammates who don't use it aren't affected, since everything corgi generates is gitignored.

## When the first run trips up

- **Port already in use** — `corgi doctor` names the process holding it; `corgi run --kill-port` frees it.
- **Missing tool, or Docker not running** — `corgi doctor --fix`.
- **A clone failed** — you don't have git access to that repo yet; fix your SSH/token and re-run.
- **Seeding failed** — check the `seedFromFilePath` path and that the dump matches the driver.
- **Want a clean slate?** `corgi stop` tears down a detached run; `corgi clean -i db,corgi_services` drops the databases and generated files (add `services` to also remove the cloned repos).

## Secrets & env files

corgi writes each service's `.env` for you — DB host/port/credentials, sibling-service URLs — and sources it before your commands run. On first init it also adds `.env*` and `corgi_services/*` to your project's `.gitignore`, so **generated env files and any secrets in them never get committed**.

Your own secrets (API keys, tokens) go in a service's env or a tier file like `env/staging/web.env` — also gitignored, also staying on your machine. The `corgi-compose.yml` itself holds config, not secrets, so it's safe to commit and share.

Run `corgi env <service>` to see the exact, fully-resolved set a service will get, and where each value came from.

## Low lock-in

Your services stay ordinary git repos, your databases are standard Docker images (corgi even writes a plain `docker-compose.yml` per database under `corgi_services/db_services/`), and the wiring is just `.env` files. Stop using corgi and you keep all of it.

## Env tiers — local, staging, or a mix

Define an env tier once — a folder of per-service env files, plus whether to skip the local databases:

```yml
envTiers:
  staging:
    dir: env/staging   # you create env/staging/<service>.env with the staging URLs/keys
    dbServices: none   # skip local databases — the staging env already points at staging's
```

Then pick it with a flag — run everything locally, or just the frontend against staging:

```bash
corgi run                                  # everything local
corgi run --tier staging --services web    # only the web app, talking to staging
```

A tier can also set `confirm: true` to prompt before you run against a sensitive one.

## A phone or another device on your LAN

`corgi run --host auto` puts your machine's LAN IP into the service-URL env vars instead of `localhost`, so a real device or simulator can reach your dev API (pass an explicit IP instead of `auto` if you prefer). Databases stay on `localhost`.
