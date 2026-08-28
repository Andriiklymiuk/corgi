<div align="center">
  <img width="300" height="300" src="./resources/corgi.png">

  # 🐶 CORGI 🐶

  **One file runs your whole project.** Every repo, database, service, and the env vars between them. `corgi run` starts all of it. That same file is what your AI agents work in, what CI boots, and what your phone connects to.

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
  [![Homebrew](https://img.shields.io/badge/install-brew-orange.svg)](#install)
  [![Platforms](https://img.shields.io/badge/platform-macOS%20·%20Linux%20·%20Windows-blue.svg)](docs/install.md)
  [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

</div>

Most projects are more than one repo. Write them all down once in a `corgi-compose.yml`, and the same file serves everyone who needs the project running:

```text
                          ┌─ you       corgi run            whole stack up, one command
                          │
  corgi-compose.yml ──►   ├─ an agent  /corgi:stories        ticket ─► code ─► draft PR per repo
   (committed, shared)    │
                          ├─ your CI   corgi test --e2e      every repo's branch, one suite
                          │
                          └─ your phone  scan a QR           send work to this laptop from anywhere
```

Video: [2-minute showcase](https://youtu.be/rlMCjs4EoFs?si=o3SQaymM55zxBCUY).

**Install:** `brew install andriiklymiuk/homebrew-tools/corgi` ([other ways](docs/install.md)).

## Why corgi

**Setup takes a day.** Clone four repos, install Postgres and Redis, seed them, copy `.env` files around and point each service at the others, find ports that are free, then start everything in the right order across five terminal tabs. Next laptop, same day again. corgi keeps all of that in one file you commit, so setup is `corgi run`.

**You keep using it after setup.** `corgi db -u` when you only need the databases up. `corgi run --service-branch api=feature/login` to try a teammate's branch. `corgi tunnel` when a webhook needs a public URL. `corgi status -w` when something looks wrong. If you already use `docker-compose`, keep it. corgi handles the repos, seed data, env files and tool checks around your containers.

**Agents get the same workspace you have.** An agent that can see one folder has to guess at the rest of the system. In a corgi workspace it has every repo, databases with real data, the env wiring, and the `corgi` CLI to start the stack and check its own work. That is the difference between editing a file and finishing a ticket. [More below](#let-an-agent-take-a-whole-ticket).

**Your laptop, from your phone.** The repos, the working credentials, the seeded databases and the simulator live on your machine, not in a cloud sandbox. Scan a QR code once and you can start a session on that machine from your phone. It helps most when the work needs the real machine, like a mobile build or a stack that takes ten minutes to warm up. [More below](#code-from-your-phone).

## Quick start

```bash
brew install andriiklymiuk/homebrew-tools/corgi   # or see Install below

corgi run -l        # browse runnable examples, pick one to try

# in your own project, next to a corgi-compose.yml:
corgi doctor        # check required tools, ports, docker
corgi run           # start every database + service, together
corgi status -w     # watch each service turn healthy
```

```text
corgi-compose.yml  ─►  corgi run
                         ├─ clone missing repos
                         ├─ start + seed databases (in Docker)
                         ├─ write + wire .env between services
                         └─ run services as host processes
                                    ↓
                         whole stack running 🐶   (Ctrl-C tears it all down)
```

> **Agents & CI:** use `corgi run --detach` then `corgi status --ready --timeout 2m` instead of the foreground commands above — they return instead of blocking. See [agents & scripting](docs/agents.md).

No `corgi-compose.yml` yet? `corgi create` writes a starter one, or `/corgi-new` writes one with Claude.

## What the file looks like

A seeded Postgres, a Go API that corgi clones for you, and a web app:

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
    start:
      - yarn dev
```

`corgi run` clones what's missing, seeds Postgres, writes the `.env` files, then runs `api` and `web` together. `Ctrl-C` stops all of it. For every available field, run `corgi docs` or browse the [examples repo](https://github.com/Andriiklymiuk/corgi_examples).


## Let an agent take a whole ticket

Give an agent a corgi workspace and it has what it usually lacks: every repo, databases with real data, the env between services, and a way to start the whole thing. So it can read a ticket, change three services, run the stack to check the result, and open a draft PR in each repo.

```text
tracker ticket ABC-123
   │
   ▼
agent reads the whole workspace
   ├─ edits  api/  ·  web/  ·  mobile/
   ├─ corgi run --detach      boots the real stack (seeded DBs, wired env)
   ├─ corgi status --ready    waits until healthy, then tries the feature
   └─ draft PR per repo  ──►  you review, you merge
```

[Install corgi](#install), then add the [Claude Code](https://claude.com/claude-code) plugin (other agents: `npx skills add Andriiklymiuk/corgi`):

```
/plugin marketplace add Andriiklymiuk/corgi
/plugin install corgi@corgi
```

Slash-commands and plain English both work:

```
/corgi:stories ABC-123          "build a referral program across the services"
/corgi:review <pr-url>          "fix the comments on this MR and answer them"
/corgi-run                      "run the todo stack, then show me the logs"
/corgi-tracker                  "how's the team doing — anything stuck?"
/corgi-queue                    "I just joined — what should I pick up first?"
/corgi-debug                    "the api is 500ing, find out why"
```

Nothing ships without you. It opens **draft** PRs and waits. If you have no project to try this on, `corgi run -l` fetches an example.

corgi is built to be driven by a program: it never stops to ask a question, prints JSON with `--json`, and returns exit codes you can branch on (`0` ok, `1` failed, `2` bad usage). It also ships an **MCP server**, so an agent calls real tools instead of guessing shell commands:

```bash
corgi mcp                        # stdio, local, no network — point any MCP client at it
corgi mcp --http :8765 --tunnel  # remote: a bearer-token-protected public URL
```

More: [agents & scripting](docs/agents.md) · [MCP server](docs/mcp.md) · [planning from your tracker](docs/tracker.md).

## Code from your phone

Some work needs your actual machine: a mobile build against the local API, a stack with a long warm-up, credentials that only exist on that laptop. corgi makes your phone a remote for it. Open the launcher, tap a repo, and a **Claude Code [Remote Control](https://code.claude.com/docs/en/remote-control) session starts on your laptop**, under the right account. You watch it work and answer its permission prompts from the phone. The branch is there when you get back to the desk.

Setup, in a corgi stack **or any git repo**:

```text
$ corgi agent up

  ✓ workspace registered
  ✓ agent daemon running
  ✓ tunnel  https://…trycloudflare.com
  ✓ pairing open · scan with your phone to pair

  █▀▀▀▀▀█  ▀ ▀▄▄ ▀  █▀▀▀▀▀█
  █ ███ █  ▀ ▄▄ ▀▄  █ ███ █
  █ ▀▀▀ █  ▄▄█▄▄▄ ▀ █ ▀▀▀ █
  ▀▀▀▀▀▀▀ ▀ ▀▄█▄█▄▀ ▀▀▀▀▀▀▀
   ▄▄▄▄▄▀  ▄█ ▀▄██▄▄ ▄▄  ▄ 
   ▀▄ ██▀▀ ▄██▀ █████ ▄▄   
  ▄█▄ ▀ ▀▄▄▄ ▀ ▄ ▀▄▀██▀▄ █▀
   ▄▀▄▄▀▀▀▄ █▀█▄▀███    ▀ █
   ▀▀   ▀  ▄▀ ▄▄█ █▀▀▀█▀ █▀
  █▀▀▀▀▀█    ▄ ▀▄ █ ▀ █▀█▄ 
  █ ███ █  ██▀ █ ▀█▀▀█▀▄▀▄▀
  █ ▀▀▀ █  ▄██ ▄▄█▀▀▀ ▀█▄▀ 
  ▀▀▀▀▀▀▀  ▀ ▀▀▀ ▀▀ ▀  ▀ ▀ 
```

```text
📱 scan QR ──► launcher (one URL, all workspaces)
                 ├─ dev-stack   [open in: app]     ──► Claude app
                 └─ client-app  [open in: chrome]  ──► Chrome (its own account)
```

One scan pairs the phone. It gets its own token, which you can revoke without touching your other devices. Each workspace remembers where it should open: personal projects go straight to the Claude app, and a work repo on a different Claude account opens in Chrome signed into that account. Save the launcher to your home screen and it is one tap after that.

The session survives network drops and crashes, which Remote Control on its own does not (it gives up after about 10 minutes offline). `corgi agent install` brings the session back after a reboot, `--tunnel-name` keeps the URL stable, and `corgi agent down` turns it all off. None of it runs unless you start it. macOS and Linux. With the plugin, `/corgi-remote` walks you through setup. Full guide: [docs/agent.md](docs/agent.md).

## What corgi does for you

- **Repos** — cloned on first run. You can also pull them all, fork them all, or run one service from a branch in a separate worktree. [More](#working-across-many-repos).
- **Databases** — 38 drivers, running in Docker and **seeded** with real data. `corgi db shell` opens a native shell with the password filled in. `corgi db -u` starts the databases and nothing else. [All drivers](docs/databases.md).
- **Services** — started together, with env vars already wired between them. `Ctrl-C` stops all of them, `corgi run -d` runs them in the background.
- **Checks** — missing tools and busy ports found before the run breaks (`corgi doctor`), live health (`corgi status -w`), public HTTPS for webhooks ([`corgi tunnel`](docs/tunnel.md)), saved logs, crash notifications.
- **Agents** — a Claude Code plugin and an MCP server, so an agent can work across every repo and open a draft PR in each. [More](#let-an-agent-take-a-whole-ticket).
- **Your phone** — start a session on your laptop from wherever you are. [More](#code-from-your-phone).
- **CI** — the same file boots the stack in CI and runs cross-repo e2e. [More](#run-the-whole-stack-in-ci).

Working on a real project with private repos, prerequisites, secrets or staging tiers? See [Getting it running on a real project](docs/getting-started.md).

## Working across many repos

corgi treats your repos as part of the stack, not as something you keep in sync on the side.

- **Auto-clone** — `cloneFrom:` clones a service when its folder is missing. A plain `path:` (a monorepo subfolder, or a repo you manage yourself) runs in place. You can mix both.
- **`corgi pull`** pulls every repo at once. **`corgi fork`** forks them to your account.
- **Run one service on a branch**, without editing the file:

```bash
corgi run --service-branch api=feature/login   # api's branch in its own worktree
corgi run --feature ABC-123                     # every repo that has the branch joins in
```

`--feature` takes one branch name. Every repo that has that branch runs from a worktree, and the rest stay where they are. Good for testing a PR branch, or for letting an agent work on a branch while you keep running `main`.

## Run the whole stack in CI

Each repo's pipeline only proves that repo works alone, so the bug that only shows up in the combination still ships. corgi's CI job starts the whole stack from the branches under review and runs one e2e suite against it.

corgi detects CI and runs non-interactive. With the official action the job is a few lines:

```yaml
- uses: Andriiklymiuk/corgi@v1                     # install corgi + a cache plan
- run: corgi init --depth 1 --feature "$BRANCH"    # shallow-clone every repo with the change
- run: corgi run --feature "$BRANCH" --detach --wait   # boot the stack, block until healthy
- run: corgi test --e2e                            # one e2e suite across the live stack
```

`--feature` tests each PR against the exact combination it will ship into. `--wait` blocks until every service is healthy, so there is no `sleep 60` in your pipeline. Full guide: [Run the stack in CI](https://andriiklymiuk.github.io/corgi/docs/ci).

## Security & scope

- **corgi is not a deploy tool.** It runs and tests your stack on your laptop and in CI. Shipping to staging and prod stays with your CI/CD. If you already use `docker-compose`, keep it: corgi runs around your containers.
- A `corgi-compose.yml` runs its `start` commands on your machine, so only run files you trust. That goes double for `corgi run -t <url>`, which runs a file from somewhere else.
- `corgi doctor --fix` will start Docker for you, but **installing a tool or killing whatever holds a port always asks first** (or `--yes` in CI).
- `corgi mcp` is local stdio by default. `--http` is **unauthenticated**, so only expose it with `--tunnel`, which adds a bearer token. Treat that URL and token like a credential.
- **No telemetry.** The only call corgi makes on its own is `corgi update` checking GitHub for a newer release.

## Install

```bash
brew install andriiklymiuk/homebrew-tools/corgi
```

No Homebrew? One line, checksum-verified:

```bash
curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | sh
```

Windows, Scoop, mise, pkgx, shell completion, VSCode extension: **[full install guide](docs/install.md)**. Then `corgi -h`.

## Documentation

- Full docs site: https://andriiklymiuk.github.io/corgi/
- [Agents & scripting](docs/agents.md) (JSON, exit codes, errors) · [MCP server](docs/mcp.md) · [Tracker planning](docs/tracker.md)
- [Work from your phone](docs/agent.md) · [Databases & drivers](docs/databases.md) · [Real-project setup](docs/getting-started.md)
- [Run in CI](https://andriiklymiuk.github.io/corgi/docs/ci) · [Tunnels](docs/tunnel.md) · [Install](docs/install.md)

[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=coverage)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

If corgi saved you a day, [a star](https://github.com/Andriiklymiuk/corgi) helps other people find it. 🐶

## Credits

- `corgi tunnel` defaults to [cloudflared](https://github.com/cloudflare/cloudflared) and its free [Quick Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/). Thanks to Cloudflare. [ngrok](https://ngrok.com) and [localtunnel](https://github.com/localtunnel/localtunnel) also work.
- <a href="https://www.freepik.com/free-vector/cute-corgi-dog-astronaut-floating-space-cartoon-vector-icon-illustration-animal-science-icon-concept-isolated-premium-vector-flat-cartoon-style_22271104.htm#query=corgi%20icon&position=7&from_view=keyword">Corgi image by catalyststuff</a> on Freepik
