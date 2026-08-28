---
name: agent
description: Use when working on a corgi stack from a phone or another device through Claude Code Remote Control, or when setting that up — "work on the recipe app", "which stacks do I have", "start a branch across the api and mobile repos", "show me the diff", "keep corgi running when I'm away", "why did my remote session die", "set up agent mode", "run it under my work account", "start a session in that repo from my phone", "set up remote session start". Covers resolving a stack by name, materializing one branch across every repository in it, reading a cross-repo diff, supervising `claude remote-control` so it survives reboots and the ten-minute network timeout, and starting/stopping a session in any registered workspace on demand (corgi_session_start, profiles, session URLs). NOT for authoring corgi-compose.yml (corgi skill), starting a stack (run skill), or diagnosing a broken stack (debug skill).
---

# Corgi agent mode

Remote Control runs a Claude Code session on the user's own machine, driven from
their phone. It sees **one directory**. A corgi stack is **several
repositories** plus databases and env wiring.

This skill is how you work across that gap.

## The one rule that matters

**Never guess which stack the user means.** A wrong resolution means editing the
wrong repository, which is far worse than one extra question.

Always resolve first, and **echo the result back before doing any work**:

> Working on **acme-stack** (`~/dev/acme`) — api, web, db.

## Working on a stack

### 1. Find it

```
corgi_workspace_resolve { "query": "the recipe app" }
```

Returns either one workspace, or candidates with a reason. If it returns
candidates, **ask** — do not pick the first one.

`corgi_workspaces` lists everything registered, with `status`:

- `ok` — usable
- `unreachable` — the path did not resolve. Say *"that drive isn't mounted"* or
  *"that folder has moved"*, **not** "workspace not found". The row is kept on
  purpose.
- `disabled` — someone turned it off deliberately.

### 2. Give every repo the same branch

```
corgi_worktrees_materialize {
  "branch": "feature/referral-code",
  "services": "api,mobile"          // omit for every service
}
```

This is the step Remote Control cannot do for you. Each entry comes back with a
`dir` — **that is where you edit**, not the user's own checkout.

- Branch is created off each repo's HEAD when nothing carries it yet.
- Two services in one repository share one worktree (git allows a branch in
  exactly one). Expect duplicate `dir` values; that is correct.
- Re-running is safe and keeps uncommitted work.
- `skipped` on an entry means that service was not touched, with the reason.
  Report it rather than pretending the change spans it.

Use a descriptive branch name derived from the task. Slashes are fine.

### 3. Show the change

```
corgi_diff { "branch": "feature/referral-code", "base": "main" }
```

**This is usually the best way to show someone what you did.** It needs no
tunnel and no running stack, so it works on a bad connection — which is exactly
the situation someone reading it on a phone is in.

- Newly created files appear with `"new": true`. `.gitignore` is respected.
- `truncated: true` means the patch was capped, not that the file is small.
- Pass `includePatch: false` when you only need the shape of the change.

Summarise it in prose first — *"3 files across api and web, +47/-12"* — then
show the parts that matter. Do not paste an enormous patch into a phone.

### 4. When you are done

`corgi_worktrees_release { "branch": "..." }` removes the worktrees. The
branches and commits survive; only the checkout is disposable. Do not release
until the user has the work somewhere they want it.

## Showing it running

When the user wants to *see* it, not just read the diff:

```
corgi_up { }                                     # the stack must be up first
corgi_preview_start { "service": "web", "branch": "feature/x" }
corgi_preview_state { }                          # poll until state is ready
```

`corgi_preview_start` returns immediately with `starting`. Poll
`corgi_preview_state` for the URL. States:

- `starting` — no URL yet. Say so; do not invent one.
- `ready` — hand over the URL.
- `broken` — the tunnel is up but nothing answers on the port, usually a build
  in progress. **Tell them that** rather than sending a link to a stack trace.
- `stopped` — gone; offer to start it again.

`corgi_preview_freeze` while they are reading it, so idle reaping does not pull
it away. `corgi_preview_stop` when they are done — a forgotten preview is a
public URL onto seeded data.

Refused for a workspace marked `sensitive`. That is deliberate; offer
`corgi_diff` instead.

Three things are unverified and worth saying once, not repeatedly: hot reload
over a tunnel depends on the provider passing websockets through; Vite and Next
need the tunnel host in `allowedHosts` / `allowedDevOrigins`; and a quick tunnel
changes URL if it restarts, so a named tunnel in the service's `tunnel:` block
is what keeps a link stable. If the page loads but never updates, that is the first of those, not your
code.

## Running the stack

The existing tools still apply, pointed at the worktree directories:
`corgi_up`, `corgi_status`, `corgi_logs`, `corgi_test`, `corgi_down`. See the
`run` and `debug` skills.

## Setting agent mode up

Only when the user asks for it, or when they say a remote session keeps dying.

```bash
cd <the stack>
corgi agent init                 # register AND enable this stack
corgi agent install              # start at login
corgi agent doctor               # what is missing, and how to fix it
corgi agent status               # what is running, under which account
```

`corgi agent doctor` output is already actionable — relay it rather than
re-diagnosing.

`corgi agent scan` registers what it finds but deliberately enables nothing —
`autostart` is opt-in, and `corgi agent init` is what sets it. Tell the user to
run `init` in the stacks they actually want supervised, rather than assuming a
scan armed them. `corgi agent serve` names every workspace it skipped and why.

### If they run more than one Claude account

This is the trap worth raising unprompted, because it fails silently.

Multi-account setups are shell aliases (`CLAUDE_CONFIG_DIR=... claude`), and
**launchd and systemd never source shell rc files**. Without an explicit
setting, every supervised session runs under the *default* account — correct
looking output, wrong account, wrong bill.

```bash
corgi agent init --config-dir ~/.claude-work
```

`corgi agent status` prints the account each workspace will actually use. If the
user has work and personal logins, check it.

### The launcher page

Everything the phone can do without the Claude app: start and stop a session,
pick a profile and name it, read the timeline, revoke a paired device, run
doctor. Two things worth telling a user unprompted:

- Cards carry a **hide** chip. Hidden cards collapse into one button, on that
  browser only — nothing on the machine changes. It is for showing the screen
  to someone, not for disabling a workspace (`autostart: false` does that).
- The header names the machine, the corgi version and whether the daemon is
  up. If it says an update is available, `corgi upd && corgi agent restart`.

### If a session died

`corgi agent status` shows `lastReason`, and `corgi agent logs <workspace>`
(or the `corgi_session_events` tool) shows the whole timeline — every start,
exit with its classified cause, and captured session link, newest first. Use
the timeline when the current reason is not enough. The common reasons:

| reason | what to say |
|---|---|
| network timeout | Remote Control exits after ~10 min awake with no network. corgi restarted it. **The previous conversation's context is gone** — the new session starts clean. |
| auth failure | corgi deliberately did not retry; retrying cannot produce credentials. Run `corgi agent doctor`. |
| exited immediately, repeatedly | corgi stopped after 5 attempts and disabled the workspace. Something is wrong with the setup, not the network. |

## Starting a session on demand (remote session start)

The daemon normally supervises only `autostart` workspaces. `corgi_session_start`
starts a session in **any** registered workspace, from a phone or any paired MCP
client — the fix for "I forgot to enable that repo before leaving the laptop".

```
corgi_session_start { "workspace": "the recipe app", "profile": "work" }
```

- Returns immediately with `state: "starting"`. **Poll `corgi_agent_status`**
  until the workspace reports `running` — its `sessionUrl` is the magic moment:
  hand it to the user, one tap opens the conversation in that repo.
- Idempotent: an already-running workspace answers `state: "running"` with its
  URL. Ambiguous names return candidates — ask, as always.
- `sessionUrl` is best-effort. If it never appears, the session still runs;
  tell the user to find it in claude.ai/code by the workspace's name.
- `corgi_session_stop { "workspace": "..." }` ends it. Stopping a non-running
  workspace is a clean no-op.
- CLI parity for local testing: `corgi agent session start <name> --profile work`,
  `corgi agent session stop <name>`.
- Every remote start and stop raises a desktop notification on the laptop, by
  design — the machine's owner always sees what began running.
- Phone push for those notifications: set `notifyUrl` in the trusted agent
  config (an ntfy.sh topic works out of the box). See docs/agent.md. The push
  carries a tap target back to the launcher.
- `corgi agent hooks enable` in a workspace also notifies when any Claude
  session there is waiting on a permission prompt or has finished its turn.
  It writes into `.claude/settings.local.json`, never the committed file.
- `corgi_pr_open { branch, title }` opens one pull request per repository that
  has commits on the branch and cross-links them — the step after
  `corgi_worktrees_materialize` and `corgi_diff`.

### Setting it up from a session on the laptop

**One command, from inside the stack directory:**

```bash
corgi agent up
```

It registers the current workspace — a corgi stack **or any git repository**
(no compose file needed) — starts the daemon (if down), opens the MCP endpoint
with a public tunnel, and prints a **scannable QR** for pairing — all detached,
so you can run it and keep working. No port to remember. `--json` emits the URL
and pairing code for a caller that wants them structured. A busy MCP port
self-heals: when the holder is identifiably corgi's own server, `up` stops it
and opens a fresh tunnel + pairing window instead of refusing. For a launcher
URL that survives restarts — and a phone that stays paired, since the origin
never changes — pass `--tunnel-name <name> --tunnel-hostname <host>` (cloudflared
named tunnel; both flags, see docs/agent.md). The mirror is `corgi agent down`: stops the
daemon AND the detached MCP + tunnel (`agent stop` stops only the daemon).
`corgi agent restart` is `down` + `up --fresh` in one — recommend it after a
corgi upgrade so the daemon and launcher run the new binary. `up` remembers
the tunnel flags it last ran with, so a bare `restart` keeps the named tunnel;
`--tunnel-hostname ""` is the way back to a quick tunnel. ngrok works too:
`--provider ngrok --tunnel-hostname <yours>.ngrok-free.app` (free static
domain, no DNS) — its free tier shows an interstitial once on first open.

The user scans the QR (or opens the printed URL) on their phone, names the
device, and gets a per-device token. After pairing, the same page offers **Open
launcher** (`/app`): corgi's own phone UI that lists the machine's workspaces
and starts a session in one tap, then hands back the claude.ai link — no
claude.ai connector needed. Each workspace row has an **open in: app | browser
| chrome** switch (set on the phone, remembered per device) — point a workspace
on a different Claude account at the browser/Chrome, where its own claude.ai
login handles the session, while the default account keeps deep-linking into
the Claude app. The launcher remembers the device token on that
browser, so a saved home-screen shortcut is a one-tap daily entry (use a **named
tunnel** so the URL is stable). Verify end-to-end with `corgi agent session
start <workspace>` or by tapping a repo in the launcher, and watch the session
URL appear.

The Claude-app custom connector still works as a second option (add corgi's
`/mcp` URL + Bearer token on claude.ai — note the request-header path is beta
and rolling out); the launcher is the setup-free path.

The old longhand still works when you want the pieces separately:
`corgi agent scan ~/dev` → `corgi agent serve &` → `corgi mcp --http :8765
--tunnel --pair`. Note `corgi agent serve` **blocks** the terminal (background
it with `&`, or use `corgi agent install` to run it at login); `corgi agent up`
backgrounds it for you.

### What travels through the corgi tunnel — and what does not

The corgi tunnel is a **control plane only**: it carries `corgi_session_start`,
status, diff, logs. It does **not** carry the conversation. The back-and-forth
with Claude runs in the **Claude app / Remote Control**, which talks to
claude.ai directly — the `sessionUrl` you open joins that, not anything corgi
proxies. So: corgi starts the session and hands you the URL; the Claude app is
where you actually talk. Two apps, by design.

### Which client calls corgi_session_start

`corgi_session_start` is an MCP tool, so any MCP client works. Today that is
the **Claude app as a custom connector**: add the tunnel URL + device token,
then say "start a session in the recipe app" and it calls the tool for you. A
dedicated companion app is a separate project (it must **not** live in the
corgi repo).

### Profiles — which Claude account runs

`profiles:` in the **user-level** agent config (`<data>/agent/config.yml`) are
named setting bundles picked at start time:

```yaml
profiles:
  work:
    configDir: ~/claude-configs/work
  personal:
    configDir: ~/claude-configs/personal
```

- A remote caller sends only a profile **name**; what it selects is defined in
  the trusted local file. Unknown names error with the list of defined ones.
- A shell alias like `claude-work` is **not** a binary — the supervisor cannot
  exec it. Prefer `configDir:` with the default `claude`; if a different
  command is truly needed, make it a real script on PATH and set `bin:`.

### Letting the laptop sleep between turns

By default the daemon holds a wake lock for the whole session, so the Mac stays
awake even while the session is just waiting for the user. To let it sleep while
idle and wake back up when work resumes, set `wakeLock: idle` in the user
config (per workspace or under `defaults:`):

```yaml
defaults:
  wakeLock: idle    # session|always|off|idle — idle sleeps after ~5 min quiet
```

`off` never blocks sleep (a long build can be cut); `idle` keeps working
sessions awake but sleeps between turns.

### If a remote start does not appear

| symptom | what to say / do |
|---|---|
| tool errors "daemon is not running" | `corgi agent serve` on the laptop (or `corgi agent install`). |
| tool errors "predates remote session start" | The daemon is an older corgi. Restart it: `corgi agent stop` then `corgi agent serve` (or `corgi agent up`). |
| workspace `unreachable` | Drive not mounted or folder moved — `corgi agent workspaces relocate`. |
| workspace marked sensitive | Remote start is refused by design. Start it on the laptop, or unset `sensitive` in `.corgi/agent.yml`. |
| queued but nothing started | Commands expire after 60s. Check `corgi agent status` diagnostics — a rejected start says why there. |
| running but no `sessionUrl` | The session is fine; the URL was not spotted in output. Find it in claude.ai/code. |
| `up` says the port is in use, pairing "not open" on the old URL | A leftover MCP holds the port. Newer corgi reclaims it on `up` automatically; otherwise `corgi agent down` then `corgi agent up` for a fresh tunnel + pairing window. |

## Things not to do

- **Do not weaken permissions on your own.** A `permissionMode: bypassPermissions`
  string and a smuggled `--dangerously` arg are refused. The one sanctioned route
  is the user explicitly setting `--dangerously-skip-permissions` on `agent init`
  or a profile — their deliberate, trusted-config choice, never yours to make.
  Those prompts are what the user answers from their phone, the defence against a
  session acting on instructions injected through a file it read. If a permission
  prompt is in the way, ask — do not route around it.
- **Do not put capability settings in `.corgi/agent.yml`.** That file is
  committed and travels with a clone. `bin`, `configDir`, `permissionMode`, and
  credential inheritance belong in the user-level config only. corgi ignores
  them in the repo file by design.
- **Do not open a public tunnel for a workspace marked `sensitive`.** It is
  refused, and the refusal is the point.
- **Do not edit the user's own checkout** when a worktree exists. That is the
  one place they may have work in progress.

## Reference

Full documentation: `docs/agent.md` in the corgi repository.
