<div align="center">
  <img width="300" height="300" src="./resources/corgi.png">

  # 🐶 CORGI 🐶

  **One file to run your whole local stack — repos cloned, databases seeded, env wired, every service up. Friendly to humans and AI agents alike.**

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
  [![Homebrew](https://img.shields.io/badge/install-brew-orange.svg)](#install)

  [![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=reliability_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Bugs](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=bugs)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=security_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Vulnerabilities](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=vulnerabilities)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Lines of Code](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=ncloc)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Technical Debt](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=sqale_index)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=coverage)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Duplicated Lines (%)](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=duplicated_lines_density)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

</div>

Corgi is how you run your project locally — every day, with one command. You describe the stack once in a `corgi-compose.yml`, and `corgi run` brings the whole thing up: repos cloned, databases seeded, `.env` files written, every service started. It even starts Docker for you, so the infra just happens and you stop thinking about it.

Onboarding falls out for free: hand a teammate the same file and they go from nothing to a running stack in minutes — no setup call, no digging through old READMEs, no "works on my machine". And because corgi never blocks on a prompt and speaks plain JSON, an AI agent or a CI job can drive it exactly the way you do.

Install it and take a ready-made example for a spin:

```bash
brew install andriiklymiuk/homebrew-tools/corgi
corgi run -l      # browse runnable examples, pick one to try
```

Here's what `corgi run` does, end to end:

```mermaid
flowchart LR
    F["corgi-compose.yml<br/>(one committed file)"] --> R(["corgi run"])
    R --> C["clone missing repos"]
    R --> DB["start and seed<br/>databases in Docker"]
    R --> ENV["write and wire .env<br/>between services"]
    R --> SVC["run services as<br/>host processes"]
    C --> OUT["whole stack running 🐶"]
    DB --> OUT
    ENV --> OUT
    SVC --> OUT
```

## Why corgi?

Wiring up a multi-service project by hand is the same slog every time — joining a team, setting up a new laptop, or starting a fresh repo. You end up having to:

- clone four different repos,
- install Postgres, Redis, Kafka… and the right Go / Node / Yarn versions,
- create the databases and fill them with test data,
- copy `.env` files and point each service at the others,
- pick ports that don't clash,
- and finally start everything in the right order, across a row of terminal tabs.

That's most of a day gone — and it breaks again on the next laptop.

`docker-compose` handles the _containers_. Corgi handles everything around them too: the repos, the seeded data, the env wiring, the tools you need. Your databases run in Docker; your services run as normal processes (`go run .`, `yarn dev`). One file, one command — and it keeps paying off long after setup, because it's also how you run the stack from then on.

In practice, a real multi-repo stack — backend, frontend, and a mobile app — can eat **more than a day** of cloning and debugging setup by hand. With corgi that drops to **~an hour** the first time someone uses it, and **~10 minutes** on the next project once they know the tool. And when you come back to a project months later and can't remember how to run it, the answer is just `corgi run` — no per-project setup scripts to write or keep alive.

## What corgi does for you

- **Your repos** — Corgi clones each service from its Git URL the first time you run. It can also pull them all at once, fork them, or run one service on a branch in a throwaway worktree — without disturbing the checkout you're working in.
- **Your databases** — 38 ready-to-go drivers. Corgi starts them in Docker and **seeds** them from a dump or a remote DB, so you get real data instead of an empty schema. Open a shell with `corgi db shell` and the password is already filled in. Need AWS or Supabase locally? LocalStack and Supabase come up from the same file.
- **Your services** — Everything starts together with the env vars already wired between them. They boot in parallel by default; when one genuinely needs another up first, gate it with `condition: ready` on the dependency (or `--gate-deps` for all of them). Press `Ctrl-C` and it all winds down cleanly. Prefer the background? `corgi run -d`, then check on it with `corgi ps`.
- **The fiddly bits** — A preflight that catches missing tools and busy ports _before_ they bite (`corgi doctor`), live health (`corgi status -w`), public HTTPS URLs for webhook testing (`corgi tunnel`), saved logs, and a desktop ping when something crashes.
- **Made for AI agents** — it speaks clean JSON, returns exit codes an agent can branch on, runs an MCP server, and ships a Claude Code plugin that turns a pile of tickets into draft PRs (and reviews them for you).

## In your day-to-day

corgi isn't a one-time setup tool you forget about — it's how you run the project, in whatever shape the day calls for:

- **The whole stack, one command.** `corgi run` brings up every database and service together — and starts Docker for you if it isn't running, so there's no "wait, is Docker up?" dance. Need AWS or a backend locally? LocalStack and Supabase stand in, so there's no VPN or shared environment to wrestle with.
- **Just the databases.** Running a service straight from your IDE or debugger? `corgi db -u` brings the databases up on their own and leaves the rest to you.
- **Local, staging, or a mix.** Define an env tier once — a folder of per-service env files, plus whether to skip the local databases:
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
  (A tier can also set `confirm: true` to prompt before you run against a sensitive one.)
- **New project? Start with corgi.** The first thing to do in a fresh repo is write a `corgi-compose.yml` — `corgi create` or `/corgi-new` gets you one in a minute — so "how do I run this?" has a permanent answer.
- **Let Claude plan the work.** Drop a few tickets into `/corgi:stories` and Claude plans the feature across your services, back to front, and opens a draft PR for each.

The point isn't any single command — it's that you stop babysitting infrastructure. No "is Docker running?", no VPN to a shared environment, no stale `.env`, no five terminal tabs in the right order. You run one thing and get back to building.

## Quick start

```bash
brew install andriiklymiuk/homebrew-tools/corgi   # or see Install below

corgi run -l        # browse runnable examples, pick one to try

# in your own project, next to a corgi-compose.yml:
corgi doctor        # check required tools, ports, docker
corgi run           # start every database + service, together
corgi status -w     # watch each service turn healthy
```

Don't have a `corgi-compose.yml` yet? `corgi create` scaffolds one — or let Claude write it with `/corgi-new` (see [AI agents](#ai-agents-mcp--claude-code)).

## What the file looks like

Here's the whole setup for a seeded Postgres, an auto-cloned Go API, and a web app — wired together:

```yml
db_services:
  db:
    driver: postgres
    databaseName: app
    port: 5432
    seedFromFilePath: ./seed.sql            # loaded on first run

services:
  api:
    cloneFrom: https://github.com/acme/api.git   # cloned if ./api isn't there yet
    path: ./api
    port: 7012
    depends_on_db:
      - name: db                            # puts DB_HOST/DB_PORT/DB_NAME/... in api/.env
    start:
      - go run .
  web:
    cloneFrom: https://github.com/acme/web.git
    path: ./web
    depends_on_services:
      - name: api                           # puts api's URL in web/.env
    beforeStart:
      - yarn install
    start:
      - yarn dev

required:                                   # corgi doctor checks these; --fix installs them
  docker:
    checkCmd: docker -v
  go:
    why:
      - Build and run the api
    checkCmd: go version
    install:
      - brew install go
```

Run `corgi run` and it clones anything missing, starts Postgres in Docker and seeds it, writes the `.env` files (and sources them for you — no boilerplate), then runs `api` and `web` together. `Ctrl-C` shuts it all back down and runs any cleanup steps.

Want to see every field? Run `corgi docs`, or browse the [examples repo](https://github.com/Andriiklymiuk/corgi_examples).

## Getting it running on a real project

The examples use public repos. Real projects have private repos, prerequisites, and first-run hiccups — here's the honest version.

**What you need:** `git`, and Docker (only if you declare `db_services`). Everything else lives in your project's `required:` block. Homebrew is just one way to install corgi itself, not a requirement.

That `required:` block is more than a checklist — it's a committed, runnable record of everything the project needs. Each entry has `why:` (so teammates know what it's for), `checkCmd:` (how to verify it — check a specific version here if you want), and `install:` (the commands to get it). `install:` runs whatever it takes: a `brew install`, a `pyenv`/`rbenv` install to pin Python 3.12 or Ruby 3.4, a native lib, a cert via `mkcert -install`. `corgi doctor` runs every `checkCmd`; `corgi doctor --fix` runs the `install:` steps for you — so "what do I need installed?" is answered in the file, not a wiki.

**Private repos just work.** corgi clones with plain `git`, so your existing SSH keys or credential helper are used as-is — private GitHub/GitLab services clone fine if your `git` is already set up. There's no corgi-specific auth to configure.

**Joining a team that already uses corgi?** `git pull`, then `corgi run`. No `corgi-compose.yml` yet? You don't have to hand-write it — `corgi create` (or `/corgi-new` with Claude) inspects the repos and scaffolds one. Adding corgi is a single committed file, and teammates who don't use it aren't affected, since everything corgi generates is gitignored (see below).

**When the first run trips up:**
- _Port already in use_ — `corgi doctor` names the process holding it; `corgi run --kill-port` frees it.
- _Missing tool, or Docker not running_ — `corgi doctor --fix`.
- _A clone failed_ — you don't have git access to that repo yet; fix your SSH/token and re-run.
- _Seeding failed_ — check the `seedFromFilePath` path and that the dump matches the driver.
- _Want a clean slate?_ `corgi stop` tears down a detached run; `corgi clean -i db,corgi_services` drops the databases and generated files (add `services` to also remove the cloned repos).

### Secrets & env files

corgi writes each service's `.env` for you — DB host/port/credentials, sibling-service URLs — and sources it before your commands run. On first init it also adds `.env*` and `corgi_services/*` to your project's `.gitignore`, so **generated env files and any secrets in them never get committed**. Your own secrets (API keys, tokens) go in a service's env or a tier file like `env/staging/web.env` — also gitignored, also staying on your machine. The `corgi-compose.yml` itself holds config, not secrets, so it's safe to commit and share.

**What gets wired.** A `depends_on_db` edge writes the database's connection vars — for Postgres that's `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`; other drivers use their own prefix (`REDIS_`, `MYSQL_`, `MONGO_`, …), and `envAlias:` renames it (`envAlias: DATABASE` → `DATABASE_HOST`, …). A `depends_on_services` edge writes `<SERVICE>_URL` (e.g. `API_URL=http://localhost:7012`). Run `corgi env <service>` to see the exact, fully-resolved set a service will get, and where each value came from.

**Low lock-in:** your services stay ordinary git repos, your databases are standard Docker images (corgi even writes a plain `docker-compose.yml` per database under `corgi_services/db_services/`), and the wiring is just `.env` files. Stop using corgi and you keep all of it.

## Supported databases & services

In `services` you can run anything you like. In `db_services`, corgi ships managed drivers that handle the container, seeding, a native shell, and env vars for you. A couple are whole stacks rather than single containers — `localstack` stands up a fleet of AWS services, and `supabase` brings up auth, storage, and studio — all from the same file.

<details>
<summary><strong>38 database & infra drivers</strong> (click to expand)</summary>

- [postgres](https://www.postgresql.org), [example](https://github.com/Andriiklymiuk/corgi_examples/tree/main/postgres)
- [mongodb](https://www.mongodb.com), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/mongodb/mongodb-go.corgi-compose.yml)
- [rabbitmq](https://www.rabbitmq.com), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/rabbitmq/rabbitmq-go-nestjs.corgi-compose.yml)
- [sqs](https://docs.localstack.cloud/user-guide/aws/sqs/) — local AWS SQS, [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/aws_sqs/aws_sqs_postgres_go_deno.corgi-compose.yml)
- [s3](https://docs.localstack.cloud/user-guide/aws/s3/) — local AWS S3 buckets
- [redis](https://redis.io), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/redis/redis-bun-expo.corgi-compose.yml)
- [redis-server](https://redis.io)
- [mysql](https://www.mysql.com)
- [mariadb](https://mariadb.org)
- [dynamodb](https://aws.amazon.com/dynamodb/)
- [kafka](https://kafka.apache.org)
- [mssql](https://www.microsoft.com/en-us/sql-server/sql-server-downloads)
- [cassandra](https://cassandra.apache.org/_/index.html)
- [cockroach](https://www.cockroachlabs.com)
- [clickhouse](https://clickhouse.com)
- [scylla](https://www.scylladb.com)
- [keydb](https://docs.keydb.dev)
- [influxdb](https://www.influxdata.com)
- [surrealdb](https://surrealdb.com)
- [neo4j](https://neo4j.com)
- [arangodb](https://arangodb.com)
- [elasticsearch](https://www.elastic.co/elasticsearch#)
- [timescaledb](https://www.timescale.com)
- [couchdb](https://couchdb.apache.org)
- [dgraph](https://dgraph.io)
- [meilisearch](https://www.meilisearch.com)
- [mailpit](https://mailpit.axllent.org) — mail-mock SMTP + web UI (the local mail server Supabase uses); web UI port via `port2`
- [faunadb](https://fauna.com)
- [yugabytedb](https://www.yugabyte.com)
- [skytable](https://skytable.io)
- [dragonfly](https://www.dragonflydb.io)
- [redict](https://redict.io)
- [valkey](https://github.com/valkey-io/valkey)
- [postgis](https://postgis.net)
- [pgvector](https://github.com/pgvector/pgvector) — postgres + `pgvector` extension. Uses prefix `DB_`, same as plain `postgres`
- [localstack](https://docs.localstack.cloud/) — single container for multiple AWS services (sqs, s3, sns, secretsmanager, ssm, kinesis), with `queues` / `buckets` / `topics` / `secrets` auto-created from config. Full docs: [docs/drivers/localstack.md](docs/drivers/localstack.md)
- [supabase](https://supabase.com/docs/guides/local-development) — wraps `supabase init`/`start`. Emits `SUPABASE_*` + S3 vars, ports from config.toml. Seeds `buckets:` and `authUsers:` on `up`; `jwtSecret:` re-signs keys; `configTomlPath:` makes corgi own config.toml under `corgi_services/`; `port:`/`dbPort:`/`studioPort:`/`inbucketPort:` patch the matching `[section].port`. Full docs: [docs/drivers/supabase.md](docs/drivers/supabase.md)
- `image` — generic docker-image driver for any public image (gotenberg, mailhog, jaeger, meilisearch, …). Set `image:` + `port:` + optional `containerPort:`/`environment:`/`volumes:`/`command:`. Full docs: [docs/drivers/image.md](docs/drivers/image.md)

</details>

**Once a database is up**, corgi keeps helping:

- **Seed it** with real data from a file (`seedFromFilePath:`) or another database (`seedFromDb:`). `corgi run --seed` loads it; the dump format is chosen per driver automatically.
- **Open a shell** with `corgi db shell [name]` — the right tool (`psql`, `mongosh`, `redis-cli`, …) with credentials already filled in. Add `-e '<query>'` to run one query and exit.
- **Manage them** from the `corgi db` menu — bring containers up or down, seed, or dump.

## Working across many repos

This is the part `docker-compose` leaves to you. Corgi treats your repos as part of the stack:

- **Auto-clone** — a service with `cloneFrom:` is cloned the first time its folder is missing.
- **`corgi pull`** — `git pull` everything at once, including nested corgi projects.
- **`corgi fork`** — fork the cloned repos to your own GitHub/GitLab and update the file to match.
- **Run a service on a branch** — point one service at a branch or another folder for a single run, no file edit needed:

```bash
# run api's feature branch in its own worktree — your checkout stays exactly as it is
corgi run --service-branch api=feature/login

# mix and match: api on a branch, web from a folder, everything else as usual
corgi run --service-branch api=feature/login --service-dir web=/tmp/wt/web

# or actually switch the checkout in place (refuses if you have uncommitted changes)
corgi run --service-checkout api=hotfix/x
```

The worktrees live under `corgi_services/.worktrees/` and are reused between runs, so dependencies and uncommitted work stick around. List or clean them with `corgi worktree list` / `corgi worktree prune`. Great for trying a PR branch, comparing two branches side by side on different ports, or letting an agent work on a branch while you keep running `main`.

## The rest of the toolbox

**Check before you run.** `corgi doctor` confirms your required tools are installed, Docker is running, and the ports are free — and tells you which process is hogging a port if one isn't. Add `--fix` and it'll start Docker, install what's missing, and free the ports for you.

**Watch it stay healthy.** `corgi status` pings every service. Use `-w` to watch live, or `-r` to block until everything's ready (handy in scripts). Set a `healthCheck:` path on a service and corgi will hit it over HTTP instead of just checking the port.

**Keep it running in the background.** `corgi run -d` starts everything detached and returns right away — no daemon, corgi just remembers what it started. Check in with `corgi ps`, restart one piece with `corgi restart --service api`, or stop it all with `corgi stop`.

**Read the logs later.** `corgi run --logs` saves each service's output; `corgi logs` lets you browse and follow past runs, with crashes clearly marked.

**Get pinged on a crash.** `corgi notifications on` sends a desktop notification when a service falls over mid-run.

**Share a local service over HTTPS.** `corgi tunnel` gives your services public URLs — perfect for testing webhooks (Stripe, GitHub apps, e-sign callbacks) without any signup:

```bash
corgi tunnel                       # tunnel every service that has a port
corgi tunnel api                   # just one
corgi tunnel --port 3030           # a raw port, skip the compose lookup
corgi tunnel --provider ngrok      # default is cloudflared (free, no signup)
```

By default it uses [Cloudflare Quick Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/) — free and anonymous, but the URL changes every restart. For a stable URL, use a Cloudflare named tunnel (login required) or another provider. If a provider needs auth, corgi tells you the exact login command up front instead of failing halfway.

## AI agents, MCP & Claude Code

corgi is meant to be driven by AI agents and CI, not just typed by hand. It notices when it's running in an agent or pipeline and never blocks on a prompt, prints clean JSON with `--json`, and returns predictable exit codes (`0` ok, `1` failed, `2` bad usage) plus a documented [error-code list](docs/agents.md) — so a tool can read what happened and react. There are quiet, scriptable entry points too: `corgi env` (the exact env a service will see, and where each value came from), `corgi exec`, `corgi test`, and `corgi run --dry-run --json` to preview a run without touching anything. Full guide: [docs/agents.md](docs/agents.md).

### MCP server

Run `corgi mcp` and any MCP-speaking agent can control your stack through proper tools instead of guessing at shell commands. It runs over stdio by default (no network at all), or over HTTP with `--http` / `--tunnel` when you want it remote. It exposes around a dozen tools — bring up, tear down, status, env, exec, test, logs, db queries — plus read-only resources like the compose schema and live status. One caution: plain `--http` has no auth; corgi only adds a bearer token when you expose it through a public tunnel. Full docs: [docs/mcp.md](docs/mcp.md).

### Claude Code plugin

If you use [Claude Code](https://claude.com/claude-code), install the plugin:

```
/plugin marketplace add Andriiklymiuk/corgi
/plugin install corgi@corgi
```

Now Claude recognizes any project with a `corgi-compose.yml` and reaches for real `corgi run` / `corgi doctor` / `corgi status` commands instead of inventing its own. Two workflows do the heavy lifting day to day:

- **Ship a batch of work — `/corgi:stories`.** Hand Claude some tracker issues (Linear or Jira) or just describe a feature. It investigates the codebase, writes a short spec for each item and waits for your sign-off, then branches per service, runs the tests, reviews its own changes, and opens **draft** PRs/MRs. Each service works in its own git worktree, so parallel stories never step on each other or on your checkout.
- **Review the result — `/corgi:review`.** Point it at the PRs/MRs (yours or the ones it just opened). It reviews them against your repo's standards and any linked ticket, checks that changes line up across services, and — after you preview — posts a summary plus inline suggestions.

```
/corgi:stories ABC-123 ABC-124       # spec → branch per service → draft PRs
/corgi:review  https://github.com/acme/api/pull/42
```

Both wait for your go-ahead and only ever open **draft** PRs — they never merge or ship on their own. Two more helpers round things out: **`/corgi-new`** scaffolds a fresh `corgi-compose.yml` from a quick chat, and **`/corgi-describe`** writes a service map with a Mermaid diagram.

## How it compares

The honest version of "why not just use X":

- **vs `docker-compose`** — Compose runs containers; that's where it stops. corgi runs your whole inner loop: it clones the repos, runs and seeds the databases (it even generates a real `docker-compose.yml` per database under the hood), wires the env between services, checks your tools, and runs your services as ordinary host processes — so you keep your usual debugger and hot-reload. The two coexist fine.
- **vs Tilt / Skaffold** — Great when your inner loop is Kubernetes and you want live container rebuilds. corgi deliberately keeps your services out of containers — no image rebuild between edits — so it's lighter for a "repos + databases + processes" stack, and not the tool if you genuinely need k8s.
- **vs Procfile runners (foreman / overmind)** — They start a list of processes. corgi does that _and_ the repos, databases, seeding, env wiring, and tool checks around them.
- **vs devcontainers / Nix** — They pin a stricter, fully reproducible environment at the OS level. corgi's `required:` block installs and verifies the exact tools each service needs — a pinned `pyenv`/`rbenv` version, native libs, certs — without the container or Nix buy-in. Lighter, though not full OS isolation.

**What corgi isn't:** a deploy tool. It runs and tests your stack locally — shipping to staging/prod stays with your CI/CD (you test with corgi, then deploy as usual). It's also not the fit if your dev loop must run _inside_ Kubernetes, or if you want a fully sandboxed, OS-level environment (devcontainers/Nix territory).

## Security & scope

corgi runs your stack on your own machine — the local inner loop. It doesn't deploy or build production artifacts (that's your CI/CD pipeline's job), though it _can_ point local services at a staging or prod environment via env tiers.

- A `corgi-compose.yml` runs its `beforeStart` / `start` commands on your machine, so only run files you trust — especially `corgi run -t <url>`, which downloads and runs a remote one.
- `corgi doctor --fix` starts Docker for you automatically, but **installing a tool or killing a port-holding process always asks first** (or needs `--yes` in CI).
- `corgi mcp` runs over stdio (local, no network) by default. `--http` is **unauthenticated** — only expose it with `--tunnel`, which adds a bearer token. Its tools can start, stop, and run commands in your stack, so treat that URL + token like a credential.
- `corgi tunnel` gives a local service a public HTTPS URL — exactly what you want for testing signing/webhook callbacks from an outside tool. The default Cloudflare quick-tunnel URL is public and ephemeral, so shut it down when you're done.

## Documentation

- Full docs: https://andriiklymiuk.github.io/corgi/
- 2-min video showcase: https://youtu.be/rlMCjs4EoFs?si=o3SQaymM55zxBCUY
- Driving corgi from a script or agent? See [docs/agents.md](docs/agents.md) and [docs/mcp.md](docs/mcp.md).

### VSCode users

Install the [corgi extension](https://marketplace.visualstudio.com/items?itemName=corgi.corgi) for syntax highlighting, autocompletion, and one-click commands.

## Install

Once installed, `corgi` works from any folder.

### macOS / Linux — [Homebrew](https://brew.sh)

```bash
brew install andriiklymiuk/homebrew-tools/corgi
```

### macOS / Linux — install script

No Homebrew? This one-liner grabs the right binary for your OS/arch from GitHub releases:

```bash
curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | sh
```

It verifies the release's sha256 checksum before installing, to `/usr/local/bin` if it can, otherwise `~/.local/bin` (and adds it to your PATH for zsh/bash/fish).

A few optional overrides:

- `CORGI_VERSION=1.10.0` — pin a version
- `CORGI_INSTALL_DIR=$HOME/bin` — force a directory
- `CORGI_NO_MODIFY_PATH=1` — don't touch shell rc files

### Windows — PowerShell

```powershell
irm https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\corgi\bin` and adds it to your user PATH.

### Windows — [Scoop](https://scoop.sh)

```powershell
scoop bucket add corgi https://github.com/Andriiklymiuk/scoop-bucket
scoop install corgi
```

### [mise](https://mise.jdx.dev) (tool/version manager)

```bash
mise use -g github:Andriiklymiuk/corgi
```

Reads corgi's GitHub releases directly — no registry config needed.

### [pkgx](https://pkgx.sh)

```bash
pkgx corgi run        # one-off, no install
pkgx install corgi    # to PATH
```

### Verify

```bash
corgi -h
```

`corgi update` (alias `corgi upgrade`) notices how you installed corgi and upgrades the same way.

Want to try it cold? Run the expo + hono example straight from a URL:

```bash
corgi run -t https://github.com/Andriiklymiuk/corgi_examples/blob/main/honoExpoTodo/hono-bun-expo.corgi-compose.yml
```

### Shell tab-completion

Brew installs `_corgi` (zsh), `corgi.bash`, `corgi.fish` automatically. After that:

- `corgi run --services <TAB>` → service names from `corgi-compose.yml`
- `corgi run --dbServices <TAB>` → db_services
- `corgi script -n <TAB>` → script names per service (filters by `--services` if set)
- `corgi tunnel <TAB>` → tunnelable services
- `corgi clean -i <TAB>` → clean targets — and completions are wired for `corgi tunnel --provider`, `corgi run --omit`, and the global `--dockerContext` / `--fromTemplateName` too

<details>
<summary><strong>Completion showing filenames instead of names? (zsh fpath / Linux setup)</strong></summary>

**zsh users — if `<TAB>` shows files instead of names**, your shell isn't loading brew's site-functions dir. One-time fix in `~/.zshrc` (works for every brew CLI, not just corgi):

```sh
# macOS Apple Silicon
FPATH="/opt/homebrew/share/zsh/site-functions:$FPATH"
# macOS Intel
FPATH="/usr/local/share/zsh/site-functions:$FPATH"
# Linux (linuxbrew)
FPATH="/home/linuxbrew/.linuxbrew/share/zsh/site-functions:$FPATH"

autoload -Uz compinit && compinit
```

Add it BEFORE any existing `compinit` call. Then `rm -f ~/.zcompdump* && exec zsh`.

Why: brew drops completions in `<brew-prefix>/share/zsh/site-functions/`, but plain zsh doesn't include that path in `$fpath` by default — so the file is installed but never loaded. Same gap affects `gh`, `kubectl`, `helm`, etc.

**Linux native package managers** (apt/dnf/pacman) — corgi isn't packaged there yet. Use the install script (`curl ... install.sh | sh`), then generate the completion script manually:

```sh
# zsh
mkdir -p ~/.zsh/completions
corgi completion zsh > ~/.zsh/completions/_corgi
# add once to ~/.zshrc:
fpath=(~/.zsh/completions $fpath); autoload -Uz compinit && compinit

# bash (needs bash-completion package)
corgi completion bash | sudo tee /etc/bash_completion.d/corgi >/dev/null

# fish
corgi completion fish > ~/.config/fish/completions/corgi.fish
```

</details>

## Credits & thanks

- `corgi tunnel` defaults to [cloudflared](https://github.com/cloudflare/cloudflared) ([Apache 2.0](https://github.com/cloudflare/cloudflared/blob/master/LICENSE)) and its free, no-signup [Quick Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/). Big thanks to Cloudflare for shipping this open and free — makes local webhook testing painless.
- Optional providers: [ngrok](https://ngrok.com) (closed source, free tier with authtoken) and [localtunnel](https://github.com/localtunnel/localtunnel) ([MIT](https://github.com/localtunnel/localtunnel/blob/master/LICENSE)) — thanks to both projects for the alternatives.
- <a href="https://www.freepik.com/free-vector/cute-corgi-dog-astronaut-floating-space-cartoon-vector-icon-illustration-animal-science-icon-concept-isolated-premium-vector-flat-cartoon-style_22271104.htm#query=corgi%20icon&position=7&from_view=keyword">Corgi image by catalyststuff</a> on Freepik
</content>
</invoke>
