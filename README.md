<div align="center">
  <img width="300" height="300" src="./resources/corgi.png">

  # 🐶 CORGI 🐶

  **Your whole project lives in one file — and everything works from it.** Repos, databases, services, env wiring. `corgi run` boots the lot; the same file is what your AI agents build on, what CI boots, and what your phone connects to when you're not at the desk.

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
  [![Homebrew](https://img.shields.io/badge/install-brew-orange.svg)](#install)
  [![Platforms](https://img.shields.io/badge/platform-macOS%20·%20Linux%20·%20Windows-blue.svg)](docs/install.md)
  [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

</div>

Four repos, a Postgres, a Redis, a pile of `.env` files and a tracker full of tickets — that's not four problems, it's one workspace. corgi is the file that describes it and the CLI that runs it:

```text
                          ┌─ you       corgi run            whole stack up, one command
                          │
  corgi-compose.yml ──►   ├─ an agent  /corgi:stories        ticket ─► code ─► draft PR per repo
   (committed, shared)    │
                          ├─ your CI   corgi test --e2e      every repo's branch, one suite
                          │
                          └─ your phone  scan a QR           Claude Code session on this laptop
```

Prefer watching? Here's the [2-minute showcase](https://youtu.be/rlMCjs4EoFs?si=o3SQaymM55zxBCUY).

**Install:** `brew install andriiklymiuk/homebrew-tools/corgi` — or [other ways](docs/install.md).

## Why corgi

**Day one takes a day.** Clone four repos, install Postgres and Redis, seed them, copy `.env` files and point every service at the others, pick ports that don't clash, start it all in the right order across a row of terminal tabs — then do it again on the next laptop. corgi puts all of it in **one committed file**, so day one is `corgi run`, and so is every day after.

**And then it stays useful.** Not an onboarding script you run once: it's how you work. `corgi db -u` for just the databases, `corgi run --service-branch api=feature/login` to try a teammate's branch, `corgi tunnel` when a webhook needs a public URL, `corgi status -w` when something feels off. `docker-compose` runs your containers; corgi runs everything around them — the repos, the seeded data, the env wiring, the tools — and the two coexist.

**Your agents get the same workspace you have.** An agent with one folder can only guess at the other services. An agent with a corgi workspace has every repo, databases with real data, the env wiring, and `corgi` itself to boot the stack and check its own work. So the unit of work stops being "a file" and becomes "a ticket, across three repos" — [see below](#ship-a-whole-ticket-not-a-file).

**And it's your machine, from anywhere.** Your laptop has what a cloud sandbox doesn't: the repos, the working credentials, the seeded databases, the simulator. Scan a QR once and your phone can start a session on it — handy when the work needs a real machine (mobile builds, a heavy local stack) and you're on the couch. [See below](#code-from-your-phone-).

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

No `corgi-compose.yml` yet? `corgi create` scaffolds one, or `/corgi-new` writes it with Claude.

## What the file looks like

A seeded Postgres, an auto-cloned Go API, and a web app — wired together:

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

`corgi run` clones what's missing, seeds Postgres, writes the `.env` files, then runs `api` and `web` together. `Ctrl-C` shuts it all down. Want every field? Run `corgi docs`, or browse the [examples repo](https://github.com/Andriiklymiuk/corgi_examples).


## Ship a whole ticket, not a file

A corgi workspace is the thing an agent needs and rarely gets: every repo, databases with real data, env wiring between services, and a CLI to boot it. Give it that, and it can take a ticket end to end — read the tracker, change three services, run the stack to check itself, and leave you a draft PR in each repo.

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

It never ships on its own — it only ever opens **draft** PRs, behind your go-ahead. New repo? `corgi run -l` grabs an example to try these against.

**Why agents get on with it:** corgi never blocks on a prompt, speaks clean JSON (`--json`), returns clear exit codes (`0` ok, `1` failed, `2` bad usage), and ships an **MCP server** so an agent drives the stack through real tools instead of guessed shell commands:

```bash
corgi mcp                        # stdio, local, no network — point any MCP client at it
corgi mcp --http :8765 --tunnel  # remote: a bearer-token-protected public URL
```

More: [agents & scripting](docs/agents.md) · [MCP server](docs/mcp.md) · [planning from your tracker](docs/tracker.md).

## Code from your phone 🪄

Some work needs a real machine — a mobile build against the local API, a stack that takes ten minutes to warm up, credentials that only live on your laptop. corgi turns your phone into a remote for that machine: open the launcher, tap a repo, and a **Claude Code [Remote Control](https://code.claude.com/docs/en/remote-control) session starts on your laptop**, under the right account. You watch it work and answer its permission prompts from the phone; the branch is waiting when you're back at the desk.

Setup, in a corgi stack **or any git repo**:

```text
$ corgi agent up
                                        ▄▄▄▄▄▄▄ ▄  ▄▄ ▄▄▄▄▄▄▄
  ✓ workspace registered                █ ▄▄▄ █ ▀█▄▀ █ ▄▄▄ █
  ✓ agent daemon running                █ ███ █ ▄ █▀ █ ███ █
  ✓ tunnel  https://…trycloudflare.com  █▄▄▄▄▄█ █ ▀▄ █▄▄▄▄▄█
  ✓ pairing open · scan to pair  ──►    ▄▄▄▄  ▄ ▀▄█ ▄▄▄ ▄▄▄▄
```

```text
📱 scan QR ──► launcher (one URL, all workspaces)
                 ├─ dev-stack   [open in: app]     ──► Claude app
                 └─ client-app  [open in: chrome]  ──► Chrome (its own account)
```

Scan once and your phone is paired — its own token, revocable without touching your other devices. Each workspace remembers where it opens: your own projects deep-link into the Claude app; a work repo on a different Claude account opens in Chrome, signed into that account. Save the launcher to your home screen and it's one tap from then on.

And it stays up. corgi restarts the session through network drops and crashes (Remote Control alone gives up after ~10 minutes offline), `corgi agent install` brings it back after reboots, `--tunnel-name` pins the URL, and `corgi agent down` shuts it all off. Nothing runs unless you opt in. macOS & Linux. With the plugin, `/corgi-remote` walks you through it. Full guide: [docs/agent.md](docs/agent.md).

## What corgi does for you

- **Repos** — clones each service on first run; can pull, fork, or run one on a branch in a throwaway worktree. [More](#working-across-many-repos).
- **Databases** — 38 managed drivers, run in Docker and **seeded** with real data. `corgi db shell` opens a native shell, password filled in. Just the databases? `corgi db -u`. [All drivers](docs/databases.md).
- **Services** — start together with env vars already wired between them. `Ctrl-C` stops all; `corgi run -d` runs in the background.
- **The fiddly bits** — catches missing tools and busy ports before they bite (`corgi doctor`), live health (`corgi status -w`), public HTTPS for webhooks ([`corgi tunnel`](docs/tunnel.md)), saved logs, crash pings.
- **Agents** — the plugin + MCP server let an agent take a ticket across every repo and open a draft PR in each. [More](#ship-a-whole-ticket-not-a-file).
- **Your phone** — any repo on your machine, one tap from a Claude Code session. [More](#code-from-your-phone-).
- **CI** — the same file boots a CI runner and runs cross-repo e2e. [More](#run-the-whole-stack-in-ci).

Real project — private repos, prerequisites, secrets, staging tiers? See [Getting it running on a real project](docs/getting-started.md).

## Working across many repos

Your repos are part of the stack, not something you manage on the side.

- **Auto-clone** — `cloneFrom:` clones a service when its folder is missing; just a `path:` (a monorepo subfolder, or a repo you keep yourself) runs in place. Mix both.
- **`corgi pull`** pulls every repo at once. **`corgi fork`** forks them to your account.
- **Run one service on a branch**, no file edit:

```bash
corgi run --service-branch api=feature/login   # api's branch in its own worktree
corgi run --feature ABC-123                     # every repo that has the branch joins in
```

`--feature` is the cross-repo hinge: pass one branch name, and each repo that has it runs from a worktree while the rest stay put. Great for a PR branch, or letting an agent work on a branch while you keep running `main`.

## Run the whole stack in CI

Each repo's pipeline only proves that repo; the "green in every repo, broken together" bug ships anyway. So corgi's CI job boots the whole stack from the branches under review and runs one e2e suite against the combination.

corgi detects CI on its own and goes non-interactive. With the official action the job is a few lines:

```yaml
- uses: Andriiklymiuk/corgi@v1                     # install corgi + a cache plan
- run: corgi init --depth 1 --feature "$BRANCH"    # shallow-clone every repo with the change
- run: corgi run --feature "$BRANCH" --detach --wait   # boot the stack, block until healthy
- run: corgi test --e2e                            # one e2e suite across the live stack
```

`--feature` tests each PR against the exact combination it will ship into; `--wait` blocks until every service is healthy (no `sleep 60`). Full guide: [Run the stack in CI](https://andriiklymiuk.github.io/corgi/docs/ci).

## Security & scope

- **What corgi isn't:** a deploy tool. It runs and tests your stack — on your laptop and in CI — but shipping to staging/prod stays with your CI/CD. Already on `docker-compose`? Keep it; corgi runs the loop around your containers and the two coexist.
- A `corgi-compose.yml` runs its `start` commands on your machine, so only run files you trust — especially `corgi run -t <url>`, which runs a remote one.
- `corgi doctor --fix` starts Docker for you, but **installing a tool or killing a port-holder always asks first** (or `--yes` in CI).
- `corgi mcp` is local stdio by default. `--http` is **unauthenticated** — only expose it with `--tunnel`, which adds a bearer token. Treat that URL + token like a credential.
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

If corgi saved you a setup day, [a star](https://github.com/Andriiklymiuk/corgi) helps other teams find it. 🐶

## Credits

- `corgi tunnel` defaults to [cloudflared](https://github.com/cloudflare/cloudflared) and its free [Quick Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/) — thanks to Cloudflare. Optional: [ngrok](https://ngrok.com), [localtunnel](https://github.com/localtunnel/localtunnel).
- <a href="https://www.freepik.com/free-vector/cute-corgi-dog-astronaut-floating-space-cartoon-vector-icon-illustration-animal-science-icon-concept-isolated-premium-vector-flat-cartoon-style_22271104.htm#query=corgi%20icon&position=7&from_view=keyword">Corgi image by catalyststuff</a> on Freepik
