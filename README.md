<div align="center">
  <img width="300" height="300" src="./resources/corgi.png">

  # 🐶 CORGI 🐶

  **Give your AI agents a whole workspace to build on.** corgi runs your whole stack from one file — cloning repos, seeding databases, wiring env between services (the parts docker-compose skips) — so your agents, you, and your CI all work from it.

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
  [![Homebrew](https://img.shields.io/badge/install-brew-orange.svg)](#install)
  [![Platforms](https://img.shields.io/badge/platform-macOS%20·%20Linux%20·%20Windows-blue.svg)](docs/install.md)
  [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

</div>

Describe your stack once in a `corgi-compose.yml`. Then `corgi run` brings it all up:

```text
corgi-compose.yml  ─►  corgi run
                         ├─ clone missing repos
                         ├─ start + seed databases (in Docker)
                         ├─ write + wire .env between services
                         └─ run services as host processes
                                    ↓
                         whole stack running 🐶   (Ctrl-C tears it all down)
```

Prefer watching? Here's the [2-minute showcase](https://youtu.be/rlMCjs4EoFs?si=o3SQaymM55zxBCUY).

**Install:** `brew install andriiklymiuk/homebrew-tools/corgi` — or [other ways](docs/install.md).

## Why corgi

Standing up a multi-repo project by hand is the same slog every time: clone four repos, install Postgres and Redis, seed the databases, copy `.env` files and point every service at the others, pick ports that don't clash, then start it all in the right order across a row of terminal tabs. A day gone — and it breaks again on the next laptop.

corgi puts all of that in **one committed file**. `corgi run` is the whole thing — not just on day one, but every day after. `docker-compose` runs your containers; corgi runs everything around them: the repos, the seeded data, the env wiring, the tools. And because that one file never blocks on a prompt, your agents and your CI drive it the exact same way you do.

## Hand the whole workspace to an agent

corgi is built to be driven by agents, not just typed by hand. An agent gets the **whole workspace** — every repo, the databases, the env wiring — plus real `corgi` commands to drive it. So it can read your tracker, build a feature across several services, run the stack to check its own work, and open a draft PR per repo.

[Install corgi](#install), then add the [Claude Code](https://claude.com/claude-code) plugin (other agents: `npx skills add Andriiklymiuk/corgi`):

```
/plugin marketplace add Andriiklymiuk/corgi
/plugin install corgi@corgi
```

Then just talk to it — slash-command or plain English, it routes on intent:

```
/corgi-run                      "run the todo stack, then show me the logs"
/corgi-tracker                  "how's the team doing — anything stuck?"
/corgi-queue                    "I just joined — what should I pick up first?"
/corgi:stories ABC-123          "build a referral program across the services"
/corgi:review <pr-url>          "fix the comments on this MR and answer them"
```

It never ships on its own — it only ever opens **draft** PRs, behind your go-ahead. New repo? `corgi run -l` grabs an example to try these against.

### Start a coding session from your phone 🪄

Away from the laptop? Open corgi's launcher on your phone, tap a workspace, and a **Claude Code [Remote Control](https://code.claude.com/docs/en/remote-control) session starts on your machine** — in that workspace, under the right account. One command sets it up:

```bash
cd ~/dev/your-stack
corgi agent up      # register this workspace, start the daemon + tunnel, print a QR to pair
```

Scan the QR to pair (per-device token, revocable), then tap any workspace to start. corgi keeps the session alive across network drops and crashes; run `corgi agent install` to start it at login so it survives reboots too. Opt-in, macOS & Linux. Full guide: [docs/agent.md](docs/agent.md).

**Why agents like it:** it never blocks on a prompt, speaks clean JSON (`--json`), returns clear exit codes (`0` ok, `1` failed, `2` bad usage), and ships an **MCP server** so an agent drives the stack through real tools, not guessed shell commands:

```bash
corgi mcp                        # stdio, local, no network — point any MCP client at it
corgi mcp --http :8765 --tunnel  # remote: a bearer-token-protected public URL
```

More: [agents & scripting](docs/agents.md) · [MCP server](docs/mcp.md) · [planning from your tracker](docs/tracker.md).

## Quick start

```bash
brew install andriiklymiuk/homebrew-tools/corgi   # or see Install below

corgi run -l        # browse runnable examples, pick one to try

# in your own project, next to a corgi-compose.yml:
corgi doctor        # check required tools, ports, docker
corgi run           # start every database + service, together
corgi status -w     # watch each service turn healthy
```

> **Agents & CI:** use `corgi run --detach` then `corgi status --ready --timeout 2m` instead of the foreground commands above — they return instead of blocking. See [agents & scripting](docs/agents.md).

No `corgi-compose.yml` yet? `corgi create` scaffolds one, or `/corgi-new` writes it with Claude. Hand a teammate that file and they get a running stack in minutes — no setup call, no "works on my machine."

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

## What corgi does for you

- **Repos** — clones each service on first run; can pull, fork, or run one on a branch in a throwaway worktree. [More](#working-across-many-repos).
- **Databases** — 38 managed drivers, run in Docker and **seeded** with real data. `corgi db shell` opens a native shell, password filled in. Just the databases? `corgi db -u`. [All drivers](docs/databases.md).
- **Services** — start together with env vars already wired between them. `Ctrl-C` stops all; `corgi run -d` runs in the background.
- **The fiddly bits** — catches missing tools and busy ports before they bite (`corgi doctor`), live health (`corgi status -w`), public HTTPS for webhooks ([`corgi tunnel`](docs/tunnel.md)), saved logs, crash pings.
- **CI** — the same file boots a CI runner and runs cross-repo e2e. [More](#run-the-whole-stack-in-ci).

Real project (private repos, prerequisites, secrets, staging tiers)? The honest setup guide: [Getting it running on a real project](docs/getting-started.md).

## Working across many repos

This is the part `docker-compose` leaves to you.

- **Auto-clone** — `cloneFrom:` clones a service when its folder is missing; just a `path:` (a monorepo subfolder, or a repo you keep yourself) runs in place. Mix both.
- **`corgi pull`** pulls every repo at once. **`corgi fork`** forks them to your account.
- **Run one service on a branch**, no file edit:

```bash
corgi run --service-branch api=feature/login   # api's branch in its own worktree
corgi run --feature ABC-123                     # every repo that has the branch joins in
```

`--feature` is the cross-repo hinge: pass one branch name, and each repo that has it runs from a worktree while the rest stay put. Great for a PR branch, or letting an agent work on a branch while you keep running `main`.

## Run the whole stack in CI

Each repo's own pipeline only proves that repo. A change that spans services can leave every pipeline green while the combination is broken. corgi closes that gap: CI boots the whole stack from the branches under review, then runs one e2e suite against it.

It detects CI on its own and goes non-interactive. With the official action the job is a few lines:

```yaml
- uses: Andriiklymiuk/corgi@v1                     # install corgi + a cache plan
- run: corgi init --depth 1 --feature "$BRANCH"    # shallow-clone every repo with the change
- run: corgi run --feature "$BRANCH" --detach --wait   # boot the stack, block until healthy
- run: corgi test --e2e                            # one e2e suite across the live stack
```

`--feature` tests each PR against the exact combination it will ship into; `--wait` blocks until every service is healthy (no `sleep 60`). Full guide: [Run the stack in CI](https://andriiklymiuk.github.io/corgi/docs/ci).

## How it compares

| | docker-compose | Tilt / Skaffold | Turborepo / Nx | process-compose | corgi |
|---|:---:|:---:|:---:|:---:|:---:|
| Databases in containers | ✓ | ✓ (k8s) | — | — | ✓ |
| Services as host processes (debugger, hot-reload) | — | — | ✓ | ✓ | ✓ |
| Works across many repos | — | — | — | — | ✓ |
| Clones & pulls the repos for you | — | — | — | — | ✓ |
| Seeds databases with real data | — | — | — | — | ✓ |
| Wires env between services | — | — | — | — | ✓ |
| Checks & installs required tools | — | — | — | — | ✓ |
| Cross-repo e2e in CI (`--feature`) | — | — | — | — | ✓ |
| Built for AI agents (JSON, MCP, skills) | — | — | — | — | ✓ |

- **vs `docker-compose`** — Compose runs containers, and stops there. corgi runs the loop around them: repos, seeded databases, env wiring, tool checks — and runs your services as ordinary host processes. Already have a Compose file? Keep it; the two coexist.
- **vs your own `dev.sh` / Makefile** — that script, until its author leaves and a new laptop breaks it. corgi is the declarative version — same on macOS/Linux/Windows, every service booting concurrently — plus `doctor`, logs, tunnels, worktrees, and CI for free.

**What corgi isn't:** a deploy tool. It runs and tests your stack — on your laptop and in CI — but shipping to staging/prod stays with your CI/CD.

## Security & scope

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
