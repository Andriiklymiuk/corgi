---
name: agent
description: Use when working on a corgi stack from a phone or another device through Claude Code Remote Control, or when setting that up — resolving a stack by name, putting one branch across every repository in it, reading a cross-repo diff, previewing a running service over a tunnel, picking up where a restarted session left off, keeping `claude remote-control` alive across reboots and the ten-minute network timeout, phone notifications (Telegram / Slack / Discord / ntfy), Claude account profiles, and starting or stopping a session in any registered workspace on demand. NOT for authoring corgi-compose.yml (corgi skill), starting a stack (run skill), or diagnosing a broken stack (debug skill).
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
`run` and `debug` skills. The orientation and repair tools work the same way:

- `corgi_context` — one call for topology, ports, health and every repo's branch;
  make it first in a workspace you have not looked at this session.
- `corgi_why { service }` — one verdict for a service that is down (dependency,
  port owner, exit code, env, log tail) instead of reading three snapshots.
- `corgi_wait_for_log { service, pattern }` — block until a log line matches;
  never poll `corgi_logs` on a timer.
- `corgi_checkpoint { name }` / `corgi_restore { name }` — mark every repo's branch,
  HEAD and uncommitted work before a cross-repo change, and put it all back.
- `corgi_checkout { branch }` — every repo onto one branch (or its own default),
  fast-forwarded; dirty repos are skipped, never clobbered.

## Setting agent mode up

Only when the user asks for it, or when they say a remote session keeps dying.

```bash
cd <the stack>
corgi agent init                 # register AND enable this stack
corgi agent up --at-login        # daemon + endpoint + tunnel, back after a reboot
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

### Getting notifications on a phone

Raise this whenever the user enables hooks, or says a notification only reached
the laptop. **Without `notifyUrl` every notification stops at the machine** —
which is the desk they were trying to leave.

**Do not suggest ntfy.sh to an iPhone user.** Its iOS app is paid; Android is
free. corgi picks the payload from the host, so free destinations exist:

| `notifyUrl` host | payload | free on iOS |
|---|---|---|
| `api.telegram.org` | `{"text", "chat_id"}` | yes |
| `discord.com` | `{"content"}` | yes |
| `hooks.slack.com` | `{"text"}` | yes |
| anything else | ntfy shape (body + `Title`/`Click` headers) | only self-hosted |

**Telegram is set up by one command — do not walk them through getUpdates.**

```bash
corgi agent notify telegram --token <TOKEN>
corgi agent restart
```

It validates the token, waits while they message the bot, resolves the chat id,
writes `notifyUrl` and sends a test. Their part is only: message **@BotFather**,
`/newbot`, copy the token, then message the new bot when the command asks.

**Never ask for the token in chat and never echo one back.** It is a credential;
it belongs in that command's flag and nowhere else. If one has been pasted
somewhere, tell them to `/revoke` in @BotFather — the chat id survives.

**Slack or Discord**: `corgi agent notify set <webhook-url>`.

`corgi agent notify show` prints the destination with the secret masked, and
`corgi agent notify test` posts to it.

**Treat the URL as a secret.** A Telegram bot token lets anyone post as that bot;
an ntfy topic anyone knows is readable by them. Never echo the configured value
back into a transcript, a commit, or a PR.

**Two traps worth naming unprompted**, because both look like a broken webhook:

- The daemon attaches the webhook **at startup**, so a `notifyUrl` written while
  it is running reaches nothing until `corgi agent restart`.
- `corgi notifications test` only fires the **desktop** notification. The one
  that posts to the URL is `corgi agent notify test`.

**On the laptop itself**, a desktop toast is clickable when `terminal-notifier`
is installed (`brew install terminal-notifier`) — it opens that workspace's
session, or the launcher when corgi has no session URL for it. Without it macOS
falls back to `osascript`, which cannot carry a click target.

### The launcher page

Everything the phone can do without the Claude app: start and stop a session,
pick a profile and name it, read the timeline, revoke a paired device, run
doctor. Two things worth telling a user unprompted:

- Cards carry a **hide** chip. Hidden cards collapse into one button, on that
  browser only — nothing on the machine changes. It is for showing the screen
  to someone, not for disabling a workspace (`autostart: false` does that).
- The header names the machine, the corgi version and whether the daemon is
  up. If it says an update is available, `corgi upd && corgi agent restart`.
- Cards show tokens today / this week, summed from Claude Code's own
  transcripts (`corgi agent status` prints the same). Cache reads are included,
  so the numbers are large by design — do not report them as an anomaly.

### Choosing how the phone reaches the machine

Ask where the phone will be, then pick the row — do not default to a tunnel:

| where the phone is | what to run | URL they save |
|---|---|---|
| same Wi-Fi as the machine | `corgi agent up --http 0.0.0.0:8765` | `http://<lan-ip>:8765/app` |
| anywhere, domain on Cloudflare | `corgi agent tunnel setup corgi.<their-domain>` | `https://corgi.<their-domain>/app` |
| anywhere, no domain | `corgi agent tunnel setup <yours>.ngrok-free.dev --provider ngrok` | that host + `/app` |
| one-off, re-pairing is fine | `corgi agent up` | changes on every restart |

The Wi-Fi row has no tunnel, no DNS and no provider, so it is the one to
suggest first when the phone is in the same building. The launcher is
token-protected either way — serving it on the local network is not serving it
to the internet. The middle two rows hold a fixed origin, which is what keeps
the phone paired across restarts. ngrok's free plan cannot choose a name;
every account already has one static `*.ngrok-free.dev` dev domain, and that is
the one to use.

### If the phone cannot open the launcher

Check these before suspecting corgi:

| symptom | cause | say |
|---|---|---|
| requests stall ~15s, Safari says "server stopped responding" | the Mac sleeps on battery; the wake lock is AC-only | plug in, or `sudo pmset -b sleep 0` |
| works on Wi-Fi, dead on cellular | that network's DNS refuses the tunnel domain — seen with `*.trycloudflare.com` and `*.loca.lt`, but it is per-carrier and most pass them fine, so do not present it as a known defect | try `--provider ngrok` with their `*.ngrok-free.dev` dev domain (free, and has resolved where those two did not); a host they own if that is refused too |
| any tunnel flakiness, phone at home | no need for a tunnel | `corgi agent up --http 0.0.0.0:8765`, then `http://<lan-ip>:8765/app` |

`corgi agent status` and `doctor` report the sleep risk directly; relay it
rather than re-deriving.

When it is unclear whether corgi or the tunnel is at fault, run both of these on
the machine before changing anything:

```bash
curl -so /dev/null -w '%{http_code} %{time_total}s\n' http://127.0.0.1:8765/app
curl -so /dev/null -w '%{http_code} %{time_total}s\n' https://<public-host>/app
```

Loopback slow or failing is corgi — `corgi agent status`, then restart. Loopback
fast and public failing is the tunnel — switch provider or move to the Wi-Fi
path. Both fast means the machine is healthy and the phone's network is the
problem; say so instead of restarting things. That second request shares this
machine's DNS and route, so a `200` does not prove a phone on cellular can
resolve the name — have them confirm on the phone with Wi-Fi off. Never stand a
public web proxy in for the phone: ngrok and Cloudflare throttle them, so its
`522` is about the proxy and reading it as a corgi fault sends the whole
diagnosis the wrong way.

### If a session died

`corgi agent status` shows `lastReason`, and `corgi agent logs <workspace>`
(or the `corgi_session_events` tool) shows the whole timeline — every start,
exit with its classified cause, and captured session link, newest first. Use
the timeline when the current reason is not enough.

A restarted session starts with none of the earlier conversation. Call
`corgi_session_brief { workspace }` first when the user picks up where they left
off: it reports the branch each repository was on, which held uncommitted
changes, and which cross-repo worktrees exist. `null` means nothing restarted,
which is the ordinary case. The common reasons:

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
  config — see **Getting notifications on a phone** above. The push carries a
  tap target back to the session.
- `corgi agent hooks enable [--all]` in a workspace also notifies when a Claude
  session there is waiting on a permission prompt. It writes into
  `.claude/settings.local.json`, never the committed file. `--turns` adds a ping
  on every finished turn and is off by default because it fires constantly.
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
`--provider ngrok --tunnel-hostname <yours>.ngrok-free.dev` (the static dev
domain every free account already has, no DNS; its name cannot be chosen on the
free tier) — ngrok shows an interstitial once on first open.

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

`corgi agent install` covers the **daemon** only. To have the MCP endpoint and
the tunnel come back after a reboot too, run `corgi agent up --at-login` once —
the daemon then repeats that up when it starts itself. Pair it with a named
tunnel, or the URL after a reboot is new and the phone has to re-pair.

A laptop that sleeps between sessions answers no phone tap: the wake lock is
per session by default. `corgi agent awake on` holds it for the daemon's whole
life — that is the replacement for a `caffeinate` left running in a terminal.

On macOS, a workspace under `~/Documents`, `~/Desktop`, `~/Downloads` or iCloud
Drive makes macOS ask to let corgi read it — and ask again after every corgi
upgrade, because corgi is ad-hoc signed. Suggest moving the stack to `~/dev`
(then `corgi agent workspaces relocate <id> <new path>`).

A "Claude is waiting for your input" toast when nothing is blocked is Claude's
60-second idle nudge. corgi drops it by default; `corgi agent hooks enable
--idle` puts it back.

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
| tool errors "daemon is not running" | `corgi agent serve` on the laptop (or `corgi agent up --at-login` so it comes back by itself). |
| everything gone after a reboot | `corgi agent install` restores the daemon only. `corgi agent up --at-login` restores the endpoint and tunnel too. |
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
