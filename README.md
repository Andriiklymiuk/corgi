<div align="center">
  <img width="300" height="300" src="./resources/corgi.png">

  # 🐶 CORGI 🐶

  **One file runs your whole project.** Every repo, database, service, and the env vars between them. `corgi run` starts all of it. That same file is what your AI agents work in, what CI boots, and what your phone connects to.

  [![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
  [![Homebrew](https://img.shields.io/badge/install-brew-orange.svg)](#install)
  [![Platforms](https://img.shields.io/badge/platform-macOS%20·%20Linux%20·%20Windows-blue.svg)](docs/install.md)
  [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

</div>

A feature is rarely one repo. It's an API change, a web change, a mobile change, and a migration. corgi describes all of that in one `corgi-compose.yml` and runs it, so you can build the whole feature at once:

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

One committed `corgi-compose.yml` describes the project. After that, the things you actually do in
a day are one command each:

| what you want | what you type |
| --- | --- |
| the whole stack up: repos cloned, databases seeded, env wired | `corgi run` |
| a feature that spans `api`, `web` and `mobile` | `corgi run --feature ABC-123` |
| a teammate's branch, in one service only | `corgi run --service-branch api=fix/login` |
| just the databases, to write a migration against | `corgi db -u` |
| the bug reproduced on yesterday's data | `corgi db restore nightly` |
| a public HTTPS URL for a webhook or a device | `corgi tunnel` |
| the same stack in CI, on the branches under review | `corgi run --feature $BRANCH --detach --wait` |
| an agent to take the ticket across every repo | `/corgi:stories ABC-123` |
| your laptop still working while you're out | `corgi agent up`, then scan the QR |
| the next project tomorrow | the same commands, in its folder |

The part that changes how you work is the second row. A feature that touches three repos normally
means three checkouts, three terminals and a lot of hoping. `--feature ABC-123` runs every repo
that has that branch and leaves the rest on `main`, so the mobile app calls the real endpoint,
which reads the real seeded database, on your laptop. You see the whole feature work before you
open a PR.

The rest of the list is why it stays installed: databases you can seed, snapshot and restore, env
files written for you, health you can watch, logs kept after the process dies, and the same five
commands on every project instead of a bespoke `make dev` per repo. Your
[agent](#let-an-agent-take-a-whole-ticket), your [CI](#run-the-whole-stack-in-ci) and your
[phone](#code-from-your-phone) drive that same file.

If you already use `docker-compose`, keep it. corgi runs the repos, seed data, env files and tool
checks around your containers.

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

Run `corgi agent up` on your laptop and scan the QR once. Your phone now has a
launcher: every repo you registered, one tap each. Tap one and a Claude Code
session starts on the laptop — your files, your databases, your credentials — and
you drive it and answer its permission prompts from the phone. The branch is
waiting when you get back to the desk.

That one command is the whole setup, and it works in a corgi stack **or any git
repo**:

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

One scan pairs the phone. It gets its own token, revocable without touching your other devices. Each workspace remembers where it should open — personal projects in the Claude app, a work repo on a different Claude account in Chrome signed into that account. Save the launcher to your home screen and it is one tap after that.

**Adding your other repos.** `agent up` registered the directory you ran it in, and the launcher lists only what is registered. Add the rest:

```bash
cd ~/dev/api && corgi agent init          # register this repo and enable it
corgi agent init --config-dir ~/.claude-work   # …under a different Claude account

corgi agent scan ~/dev                    # find repos under a folder and register them
                                          # (registers only; enable each with agent init)

corgi agent workspaces                    # list them · forget <id> drops one
```

`agent init` writes `.corgi/agent.yml` in the repo. It carries identity only, so it is safe to commit:

```yaml
version: 1
workspace:
  id: acme-stack
  aliases: [acme, recipe app]
```

Which Claude account it runs under, and whether it starts on its own, live in your own machine's config instead — a cloned repo can never decide that. The new repo shows up in the launcher on the next refresh; no re-pairing.

**What corgi is doing for you.** The session itself is Claude's own
[Remote Control](https://code.claude.com/docs/en/remote-control) — corgi
reimplements none of it, and never asks you to start it. What corgi adds is
everything that has to be true before a phone is any use:

- **It is running when you are not there.** `corgi agent install` starts corgi at login, so the laptop answers after a reboot without you having set anything up before you left. Sessions come back after a crash, and a wake lock stops the laptop sleeping through a long task. If one dies anyway, `corgi agent brief` says where it stopped and which repo it left dirty.
- **Every repo, on its own Claude account.** One launcher lists them all. Personal projects open in the Claude app; a work repo opens in Chrome signed into the work account.
- **One branch across the whole stack.** A session is not stuck in one directory: corgi puts the same branch in every repo that has it, and hands back a single diff over all of them — readable on the phone with no tunnel and nothing running.

The free tunnel gets a new URL on every restart; `corgi agent up --tunnel-name <yours>` keeps it stable so the phone stays paired. `corgi agent down` turns everything off, and nothing runs again until you start it. macOS and Linux. With the plugin, `/corgi-remote` walks you through the whole setup. Full guide: [docs/agent.md](docs/agent.md).

## The rest of the commands

Beyond the ones above, these are the ones that come up in a normal week:

| command | what you get |
| --- | --- |
| `corgi run --tier staging` | local services pointed at your staging env tier |
| `corgi db shell` | a native `psql`/`mysql` shell with the password already filled in |
| `corgi db snapshot` | freeze the current database state so you can come back to it |
| `corgi exec api -- go test ./...` | a one-off command in that service's directory and env |
| `corgi logs --service api` | logs kept after the process is gone |
| `corgi status -w` · `corgi ps` | health and run state while you work |
| `corgi mc` | one pane: each service's run state with its branch, PR and CI |
| `corgi open web` | opens the localhost URLs in the browser |
| `corgi doctor --fix` | missing tools, busy ports, Docker not running |
| `corgi memory list` | decisions and incidents the team commits next to the code |

38 database drivers ([list](docs/databases.md)), and all of this behaves the same whether the
project has one service or twelve.

Private repos, prerequisites, secrets or staging tiers? See
[Getting it running on a real project](docs/getting-started.md).

## Working across many repos

corgi treats your repos as part of the stack, not as something you keep in sync on the side.

- **Auto-clone** — `cloneFrom:` clones a service when its folder is missing. A plain `path:` (a monorepo subfolder, or a repo you manage yourself) runs in place. You can mix both.
- **`corgi pull`** pulls every repo at once. **`corgi fork`** forks them to your account.
- **`corgi checkout main`** puts every repo back on `main` and fast-forwards it. A repo that calls its trunk something else falls back to its own default branch, and a repo with uncommitted work is skipped, not clobbered.
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
