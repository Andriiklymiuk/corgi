---
name: agent
description: "Use when working on a corgi stack from a phone or another device through Claude Code Remote Control, or setting that up: cross-repo branches and diffs, tunnels, restarts, notifications, account profiles, session tracking (\"which sessions are waiting on me\", \"answer that permission from my phone\"). Also for the daemon's tracker and PR watch: being told about ticket or review activity (\"watch the jira issues here\", \"tell me when someone comments on my MRs\"), what is or should be tracked, stopping a watch, or why it did or did not fire. NOT for compose authoring (corgi), starting (run), or debugging (debug)."
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
corgi_up { "omit": "useAwsVpn" }                 # compose has useAwsVpn but this run doesn't need it
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

### Supervised servers are devices, not conversations

A server the daemon starts on its own opens **no session**: it registers with
claude.ai as a device (`claude remote-control --no-create-session-in-dir`) and waits.
`corgi agent status` calls that state `online`; the launcher card says *online · no
session* with a solid green dot and a **Start** button. That is the resting state,
not a start that failed — do not restart anything because of it.

A session exists once someone asks for one: the Claude app's device list (new
session on that machine), the launcher's Start, or `corgi_session_start`. The last
two swap the device server for one that opens a session and returns its link —
expect a few seconds' gap and a `starting` card in between. Those sessions are
named `<workspace>-brave-otter` (corgi sets the name prefix), so the list still
says which repo each is in. **Stop** on a session in an autostart workspace ends
the session and puts the device back.

Why: remote control pre-creates a session on every start and leaves it in the
claude.ai list when it stops, so four supervised workspaces restarted at login, on
the network timeout and on `corgi agent restart` used to fill the phone's list with
`<ws> · main · 10:00` rows nobody opened. Two things worth telling a user who asks
about that:

- rows an older corgi left behind are ordinary offline sessions — archive them once
  in the claude.ai list; new ones will not appear;
- `autostartSession: true` in the trusted config (per workspace or `defaults:`) is
  the way back to a session waiting in the list the moment the daemon is up.

A Claude Code older than the flag rejects it; the daemon drops it on the first exit,
restarts at once with a session, and `corgi agent status` shows a *note:* line
saying so. The fix is `claude update`, not a corgi setting.

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
is installed (`brew install terminal-notifier`) — it brings the session's window
forward (VS Code, iTerm, Terminal or tmux, as `corgi agent focus` does); a session
with no window on this Mac opens its session URL, or the launcher when corgi has
none. Without it macOS falls back to `osascript`, which cannot carry a click target.

### The launcher page

Everything the phone can do without the Claude app: start and stop a session,
pick a profile and name it, read the timeline, revoke a paired device, run
doctor. With session tracking on, the top of the page is the session board —
"2 waiting on you", each with what it is waiting for — so the phone answers
the question it was unlocked for before any card is tapped. Worth telling a user
unprompted:

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

With two laptops on one Cloudflare account, each gets its own hostname
(`home.<domain>`, `work.<domain>`) and its own tunnel; `tunnel setup` names
the tunnel after the laptop and refuses a tunnel whose credentials live on
another one. A domain bought elsewhere (Namecheap, GoDaddy) stays there: only
its nameservers move to Cloudflare's free plan, because a tunnel hostname
must be proxied by Cloudflare — a plain CNAME at the registrar does not work.

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
  URL. Ambiguous names return candidates — ask, as always. A workspace that is
  `running` with `deviceOnly: true` and `sessionsThisRun: 0` is being **swapped** for
  a session-opening server — poll on; the URL follows.
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
`corgi agent restart` is `down` + `up --fresh` in one. `corgi upd` runs it
itself when the daemon was set up with `corgi agent install` (it says so, or
says why not); only recommend a restart by hand when `corgi agent status`
still shows the old version. `up` remembers
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

The Claude-app custom connector is the second option: claude.ai, Claude
Desktop and the Claude phone app all reach corgi's tools through it. The
launcher is the setup-free path; the connector is for "ask Claude to run
corgi_status" from a chat. Walk the user through it in this order, and give
them each value ready to paste:

1. **URL** — `corgi agent status` prints it on the `connector` line
   (`https://<host>/mcp`); `--json` has it as `connectorUrl`. It is the
   daemon's public tunnel origin plus `/mcp`; if the status says *no public
   URL yet*, the tunnel is not up — `corgi agent up`.
2. **Authentication → Sign in now** (the default Claude detects). corgi is
   its own OAuth server on that origin, so **Add** opens corgi's consent
   page in a browser tab. Approve it:
   - a browser that has opened `corgi agent dashboard` before shows one
     **Approve** button;
   - any other browser (the phone's, a fresh profile) shows a code — the
     user runs `corgi agent approve ABCD-2345` on the machine the daemon
     runs on, and the page finishes on its own. Ten minutes per code.
   Claude then holds a one-hour access token it refreshes for 30 days. It
   shows in `corgi mcp devices` as `Claude · oauth <id>`; `corgi mcp devices
   revoke "<that name>"` ends the grant and Claude asks to sign in again.
3. **Fallback, header path** — if the daemon runs with `--no-oauth`, or the
   user prefers a token: Authentication **No sign-in** and a request header
   `Authorization` = `Bearer corgi_dev_…`. The token comes from
   `corgi agent dashboard --print --name claude-web` in a terminal the user is
   looking at (it refuses a pipe; the link ends in `#token=corgi_dev_…`, the
   part after `token=` is the bearer) or the phone's Settings → connector
   token. The phone's **own** token is refused on `/mcp` by design. Revoke
   with `corgi mcp devices revoke claude-web`.

If the consent page answers *cannot start this sign-in*, the client's
redirect URI is not one corgi trusts: loopback, `claude.ai`, `claude.com`,
or a host given with `corgi mcp --oauth-client-host` /
`CORGI_MCP_OAUTH_CLIENT_HOSTS`. Nothing was redirected.

After **Add**, the connector lists the server as **Corgi** with every tool
titled; read-only tools (`corgi_status`, `corgi_logs`) run without a prompt,
destructive ones (`corgi_exec`, `corgi_db_query`) ask first. Verify with
"corgi status" in a chat.

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
the **Claude app as a custom connector** (the recipe above: connector URL
from `corgi agent status`, Sign in now, approve the consent page), then say
"start a session in the recipe app" and it calls the tool for you. A
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
| status says `online`, launcher says *online · no session* | Not a failure: a supervised server waiting as a device. Start from the launcher or the Claude app's device list opens a session. |
| my claude.ai list is full of `<ws> · main · HH:MM` rows | Leftovers from an older corgi — see *Supervised servers are devices* above. Archive them once. |
| status shows *note: this Claude Code predates --no-create-session-in-dir* | `claude update`, then `corgi agent restart`. |
| `corgi agent sessions` is empty though Claude is running | Hooks not installed, or installed after the session started: `corgi agent track enable`, then `corgi agent rescan`; new sessions report from their next event. |
| a Stream Deck / `focus` press does nothing | `corgi agent doctor` → a session with `unknown` host. Install the corgi VS Code extension and reopen the terminal; iTerm2 needs the tty (a session started before tracking shows none until its next event). |
| VS Code tabs still say "claude" | Set `terminal.integrated.tabs.title` to `${sequence}` — the default `${process}` ignores the title the hook sets. |
| `up` says the port is in use, pairing "not open" on the old URL | A leftover MCP holds the port. Newer corgi reclaims it on `up` automatically; otherwise `corgi agent down` then `corgi agent up` for a fresh tunnel + pairing window. |

## Tracking every session on the machine

`corgi agent hooks` covers one workspace. `corgi agent track` covers **every**
Claude Code session on the machine, wherever it was started — a VS Code
terminal, the Claude Code panel, iTerm2 — and keeps them on a fixed board of
keys the daemon publishes as `sessions.json`. That board is what a Stream Deck
plugin draws, what the phone launcher shows at the top ("2 waiting on you"),
what `corgi_sessions` returns, and what `corgi agent sessions` prints:

```text
7 session(s) on 6 keys, 1 more than fit
 1   ▲ acme-api           NEEDS YOU  permission: Bash  work · vscode-terminal · 12s
 2 📌 ● web                WORKING    Edit              default · vscode-panel · 3m
 6  +2  (press to page)
```

Set it up once, for the whole account (and every corgi profile's config dir):

```bash
corgi agent track enable            # hooks into ~/.claude/settings.json + each profile's dir
corgi agent board --slots 15        # a bigger deck (default 6, a Stream Deck Mini), applied live
corgi agent track enable --no-tab-title
corgi agent doctor                  # "session tracking" and "session board" lines
```

What the user gets, and the words to use for it:

- **Status per session**: `working` (model or tool running), `needs_input`
  (permission prompt, a question, an API failure — the one that matters),
  `done`, `stale` (30 min of nothing), `gone` (exited, but the key is pinned).
  Claude's "waiting for your input" nudge after a *finished* turn is not
  `needs_input`; the same nudge mid-turn is.
- **Tab titles**: every terminal tab running Claude reads `● repo`,
  `▲ repo NEEDS YOU` or `✓ repo`. VS Code shows them once
  `terminal.integrated.tabs.title` is `${sequence}` (its default, `${process}`,
  shows only "claude"); the corgi VS Code extension offers that setting once.
- **Focus**: `corgi agent focus <label|id|key>` brings that session's window
  to the front. The exact terminal tab or the Claude Code panel needs the
  corgi VS Code extension (it injects `CORGI_VSCODE_WINDOW` and reveals tabs);
  iTerm2 and Terminal.app tabs are found by tty. Without either, the app comes
  forward and nothing else — never a guessed folder, which would open a new
  window.
- **Keys never move**: a new session takes the lowest free key, an ending one
  frees its key. `corgi agent pin <key>` reserves one (its session stays even
  after exit, dimmed, until `--off`). More sessions than keys → the last
  unpinned key is a `+N` pager, `corgi agent page next|prev` turns it.
- **New session from the deck**: `corgi agent new` opens a fresh terminal
  running `claude` in the last-focused editor window (needs the corgi VS
  Code extension); the session takes the lowest free key within a second.
- **Nothing is lost when the daemon is down**: hooks are async and exit 0, and
  the next daemon rescans the process table (`corgi agent rescan` on demand).
  Remote sessions (`CLAUDE_CODE_REMOTE`) and subagents are never registered;
  neither is the `claude remote-control` server corgi itself supervises.

When a key press goes nowhere, `corgi agent doctor` names sessions with no
known window (`unknown` host). The usual cause is a terminal opened before the
extension activated: reopen it. `corgi agent windows` lists the editor windows
the extension has connected. `corgi agent track disable` removes the hooks and
touches nothing else in `settings.json`.

From a phone, `corgi_sessions` is the same board as JSON — call it for "is
anything waiting on me" or "what is running right now"; it says so when
tracking is not enabled yet.

## Acting on the board from elsewhere

Everything the keys do is a command, so the same board reaches other places:

```bash
corgi agent send <session> --enter "run the tests"   # type into a session (VS Code terminal, iTerm, Terminal.app)
corgi agent answer <session> allow|always|deny       # its permission prompt; risky Bash (rm -rf, sudo, --force, drop) is refused
corgi agent note <session> "waiting on PR"           # a line of yours under the session
corgi agent usage [--json|--watch]                   # every account: 5h and week windows, forecast, when a reached limit lifts
corgi agent carry <session> --profile work           # continue a limited session under another listed account
corgi agent continue on                              # daemon types "continue" into a limited session once its window resets (off by default)
corgi agent watch ignore <REF|key>                   # a ticket out of the inbox everywhere, for good; unignore puts it back
corgi agent claude --profile auto                    # start under the listed account with the most budget
corgi agent watch enable --auto-for reviews,comments # work those unattended; a fresh ticket only notifies
corgi agent watch enable --ci                        # red builds too: the one kind with its own test for done
corgi agent watch enable --from max                  # only the reviewer you are waiting on
corgi agent watch undo [REF] [--dry-run]             # close what a run opened, put its ticket back
corgi agent watch replay [--since 168h]              # the week unattended mode would have had
corgi agent while-away                               # what corgi opened, could not do, is still running
corgi agent dashboard                                # open the dashboard on this machine, paired
corgi agent watch enable --pickup "In Progress"      # picking a story up moves it on the board
corgi agent watch board [--refresh]                  # the tracker columns corgi knows
corgi agent watch move ABC-123 "Ready for staging"  # move, assign, comment: writes as you
corgi agent today [--write]                          # today since midnight, the watch's own runs included
corgi agent today --json                             # + waits{count, medianS} and days[14] from the daemon's own ledger — the phone's share card numbers
corgi agent standup [--since 48h] [--write]          # a rolling window of the same
corgi agent digest --send                            # the daily message (digestAt in the user config)
corgi agent workspaces pause|resume <id>             # stop supervising one (autostart: false), or resume
```

Surfaces that draw the board: **corgi-bar** (macOS menu bar: rows by
workspace, Allow/Deny, accounts, Talk, Remote devices with Pause), the
**VS Code extension** (Agent sessions view, status bar, toast with Go, quick
pick), **Corgi Agent Deck** (Stream Deck keys; "+" opens a session in the
window in front, a hold picks another open window), and the **Telegram bot**
(`corgi agent notify telegram --token …`: reply to a "needs you" message to
type into it; `/sessions`, `/usage`, `/send`, `/allow`, `/always`, `/deny`,
`/focus`). "limit lifted on <account> — back to work" arrives the moment a
limited session resumes (after a 20 s grace), and "the turn resumed after
the limit … is done" when that turn ends. A limited key says which kind: **quota** (the
account's window is spent; `resets 2pm`, carry or wait) or **overload**
(the API said try later; minutes). With `corgi agent continue on` the daemon
plans the resume itself — the key shows `continues 14:02` — and gives up after
three tries when the limit comes straight back. The **phone launcher** has the same under each
session row: Allow, Always, Deny when it needs you, Send… for any live one,
and a PR link when the session mentioned one; its **New chat** box opens a
session in a chosen editor window with a first prompt, a model and an
account (the prompt travels by id in a 0600 file, never in the shell line).

The board also feeds back into sessions: with tracking on, every new session
starts with a few lines from `corgi agent hook context` (the other sessions
in the workspace with their branch, the account's 5h and week percent and
pace, the last handover brief, the workspace memory count). Read them before
editing: another session on the same branch means coordinate, a budget near
its limit means keep the turn short or `corgi agent carry`.

## Watching the tracker and your pull requests

`corgi agent watch` makes the daemon notice work that arrives while nobody
is at the desk, and optionally start the fix:

```bash
cd ~/dev/acme-stack
corgi agent watch enable --tracker linear --project ABC --repos acme/api --labels bug,defect --prs   # notify: new issues assigned to me, reviews on my PRs
corgi agent watch enable --labels bug --prs --action fix      # and run the skill: /corgi:stories <id>, /corgi:review <pr>
corgi agent watch auth linear --token lin_api_…               # machine-wide; or LINEAR_API_KEY / JIRA_URL+JIRA_EMAIL+JIRA_API_TOKEN / GITHUB_TOKEN (gh auth) / GITLAB_TOKEN
corgi agent watch auth jira --url https://acme.atlassian.net --email me@acme.com --token … --local   # this workspace only, beats the machine-wide token and the env
corgi agent watch                                             # tokens, watched workspaces, last polls, events today, fix budget
corgi agent watch run                                         # one poll now; hands deferred fixes back to the daemon
corgi agent watch test issue.comment --body "still needed?"   # one made-up event through rules, dedupe, claim, caps; prints the prompt, runs nothing
corgi agent watch hooks [--install]                           # the workspace's webhook plan; --install creates/updates GitHub + GitLab repo hooks (polling stays on as the safety net; same comment = same key, runs once)
corgi agent restart
```

**Slack.** The watch reads Slack with the person's own user token (`xoxp-`),
issued by any Slack app they own in that workspace (Slack has no app-less
personal tokens; a free workspace caps apps at 10, so reuse an existing one
rather than creating a new one). Scopes go under **User Token Scopes**, never
Bot:

| Wanted | User Token Scopes |
|---|---|
| mentions of me → phone | `search:read`, `users:read` (the smallest token) |
| + a review channel read (a teammate's post with PR links = one review) | + `channels:read`, `channels:history` (`groups:*` for a private channel) |
| + DMs count as mentions | + `im:read`, `im:history` |
| + answer in the thread / `chat announce` as me | + `chat:write` |
| + the ✅ on a reviewed post | + `reactions:write` |

Pitfalls: the granular `search:read.*` and any `admin.*` scope are
Enterprise-only on a user token — plain `search:read` is the one; a reinstall
keeps the token and the app's existing webhooks (a CI bot posting through the
same app keeps working; do not remove `incoming-webhook`). A token with fewer
scopes still works for what it can read (public channels alone, mentions
alone); `corgi agent watch` names the scope a failing call needs.

```bash
corgi agent watch auth slack --token xoxp-… --local          # the person runs this; the token never goes through a session
corgi agent watch enable --mentions                           # notify: @me anywhere → phone
corgi agent watch enable --review-channel '#code-review' --reply-as me   # teammates' review posts in; chat announce out
corgi agent watch enable --action fix --auto-for reviews --approve --trust @teammate   # reviews run on their own; only --trust people may start one from a mention
```

**Unattended.** `corgi agent watch enable --auto` is `--action fix --prs
--comments`: it works on what arrives instead of only telling you. Draft PRs
only, never a merge, one run at a time per ticket, at most 3/hour and 10/day,
nothing in `--quiet` hours — no fix starts and nothing buzzes; what arrived is recorded, shown in the inbox, and delivered as one summary when the window opens — none above 95% of a usage window unless `--limit-ceiling` says otherwise. Nothing is done
twice — an event key is handled once across polls and webhooks, and a second
event for a ticket already being worked on is refused. A run the daemon was
killed in the middle of is closed on the next start and its event offered
again, so a reboot loses nothing and repeats nothing. What it opened outlives
the notification: `corgi agent watch` lists the last fixes with their PRs,
`corgi_watch_fixes` returns them, and the phone shows them under "worked on
for you". It needs `corgi agent init --dangerously-skip-permissions` to run
without stalling on a prompt — say that before turning it on.

**Asked what is tracked?** ("what do we watch?", "what is corgi tracking?".)
Read it, say it plainly, then say what is missing — that second half is the
part people are actually asking for.

1. `corgi_watch_status` (MCP), or `corgi agent watch`, for every workspace: the
   sources with a token, what each is watching, notify or fix, which kinds run
   unattended, the caps, quiet hours, and the last poll.
2. Say it per workspace in one line each — tracker and project, what reaches
   them, and whether corgi acts or only reports. Name what is **not** watched
   as plainly as what is: a workspace with no watch at all is the most useful
   thing in the answer.
3. **Then suggest, concretely.** Read `whatIsMissing[]` first — it is the
   ordered list of what that workspace still needs. Beyond it, the suggestions
   worth making, in this order:
   - a registered workspace with **no watch** and a tracker key in its commits
   - `--prs` off, when the person opens pull requests in those repos
   - `--ci` off — a red build is the safest thing to hand over, since it comes
     with its own test for "done"
   - `--reviews` off, when other people request their review
   - `--action fix --auto-for reviews,comments` on a workspace that only
     notifies and has been quiet enough to trust
   - `--from <person>` when they keep saying they are waiting on someone
   - `--pickup "In Progress"` so a picked-up ticket moves itself
   - `--lease` when more than one machine watches the same board
4. **Offer the smaller version first.** Watching a portion is a real answer:
   one project key, one label (`--labels bug`), a few repos, a handful of
   states (`--states "Ready,In Progress"`), or one person. Say so — people
   refuse "watch everything" and accept "watch the bugs assigned to you".
5. Anything unattended: point at `corgi agent watch replay` before turning it
   on, and `corgi agent watch undo` for after. Neither is a detail; they are
   what make it reversible.

Never turn any of this on unasked. Say what you would run, and run it when
they say yes.

**Asked to watch something?** ("watch the jira issues here", "tell me when
someone comments on my MRs".) Never guess the settings — read them, then act:

1. `corgi_watch_status` (MCP) or `corgi agent watch` says what this workspace
   already has and what it is missing. Do that first; half the answer is
   usually already configured.
2. **Derive the key and the repo list, never invent them.** A key one letter
   off routes nothing, silently, and looks exactly like a quiet week:
   ```bash
   for d in */; do git -C "$d" log --oneline -30; git -C "$d" branch -a; done 2>/dev/null \
     | grep -oE '\b[A-Z][A-Z0-9]{1,9}-[0-9]+\b' | sort | uniq -c | sort -rn | head
   for d in */; do git -C "$d" remote get-url origin 2>/dev/null; done \
     | sed -E 's#.*[:/](.+)\.git#\1#' | sort -u | paste -sd, -
   ```
3. Missing a token? Print the command for the person to run — **never put a
   token in a tool call or in chat.** Jira wants the plain API token (Basic
   auth), GitLab a legacy one scoped `read_api`; both are a coin flip in their
   UIs. `--local` keeps it to this workspace.
4. `corgi_watch_enable`, or `corgi agent watch enable`, then
   `corgi agent restart`. Say that the first tracker round looks 24 hours back,
   so a day of tickets arrives once — otherwise it reads as a bug.
5. Backlog noise → `--states` with the tracker's **real** status names.
   `corgi_watch_events` answers "what came in?"; `corgi agent watch test
   <kind> --ref <KEY>` answers "why did that fire?" and runs nothing.

How it stays cheap: a saved cursor per source (Linear/Jira `updated >`,
GitHub notifications with `If-Modified-Since` → 304, GitLab todos by id), a
seen list across polls and webhooks, the first round only sets the bookmark,
a failing token backs off, and a source the rules take nothing from (GitHub
or GitLab with `prs` off) is not polled at all. Nothing spends agent tokens
unless a new event matched the rules; with `fix`, one headless `claude -p`
per issue or PR at a time, thirty minutes at most, the process group killed
at the deadline, draft PRs only. Per kind: a new issue runs
`/corgi:stories <key>`; a comment on your issue is read and either answered
on the ticket through the tracker (no PR) or applied on the ticket's
existing branch (else `/corgi:stories`); a review or comment on your PR
runs `/corgi:review <url>` in address-feedback mode, never a fresh review.
Budget: at most 3 fixes an hour and 10 a day (`--max-per-hour`,
`--max-per-day`), none in `--quiet 23:00-07:00`, none at 95 % of a usage
window; a fix past that is deferred with the reason in the notification,
and starts on its own once the cap, the quiet hours or the budget has
passed — most urgent first (labels `urgent`, `p0`, `p1`, `critical`, then
`high`, `p2`), one per poll round; `--no-retry` leaves them to
`corgi agent watch run`. A fix runs unattended only for a workspace enabled with
`corgi agent init --dangerously-skip-permissions`. Rules live in the user
config under `workspaces.<id>.watch` (`labels`, `states`, `assignee`,
`comments`, `prs`, `repos`, `project`, `interval`, `action`,
`maxFixesPerHour`, `maxFixesPerDay`, `quiet`).

Routing and tokens across companies: `--project` (issue key prefix) and
`--repos` (owner/name) are what assign an event to a workspace; without them
it falls through to the first watched workspace whose rules merely match, so
always set them. Tokens are machine-wide by default and per-workspace with
`--local` / `--workspace <id>`, which beats both the machine-wide token and
the environment — one Jira site per client, a second Linear key, a
self-hosted GitLab. They are stored in the user-level agent directory at
0600 and never in the repository; `corgi agent watch` prints a row per
workspace holding an override. `--clear --local` drops one.

When the user asks "can corgi listen to new Jira/Linear issues or PR
comments and fix them", this is the answer: enable watch with `--action fix`,
add tokens, restart the daemon; webhooks for instant reaction.

### Handoffs, the workpad, blocked, the kanban

A run that stops part-way leaves a **handoff**: a typed packet, never a
transcript, at `.corgi/corgi_services/handoffs/<ref>.json` with a Markdown
twin. Write one yourself when you stop with work remaining, before a carry,
or when you are blocked:

```bash
corgi agent handoff --ref ABC-123 --done "api returns 429" --remaining "web banner" \
  --decision "5h sliding window" --uncertain "retry on the phone?" --next "web banner" \
  --verify "corgi test --changed"        # runs it now, records exit + head
corgi agent handoff show ABC-123         # the packet; says how many commits since
corgi agent handoff verify ABC-123       # re-runs its check (must be a doneWhen line)
corgi agent handoff --ref ABC-123 --blocked "no GITLAB_TOKEN for the web repo"
```

The ref defaults to the ticket key in the branch name. A packet with a
secret or a TODO is refused. The next run — unattended, or a new session on
the branch (the SessionStart context says "handoff for ABC-123: read … first")
— re-runs the packet's check at the current head before trusting its done
list, but only when that check is one of the workspace's `doneWhen` lines
(the packet was written by a run, so its command is not the person's); a
check that fails, is not listed, or a branch that moved, means start from
the ticket and the diff. `corgi agent carry` writes a draft packet before it
moves a session, and past 85 % context (or `--fresh`) starts the new session
clean with the packet as its first prompt.

The **workpad** is the one corgi comment on a ticket, sections rewritten in
place: `Spec`, `Pull requests`, `Handoff`, `Blocked`. Post the spec there
(`corgi agent watch workpad ABC-123 Spec - < docs/stories/ABC-123.md`); corgi
adds the PRs a run opened, the handoff it left, the reason it is blocked.
The first line of the comment is `corgi · workpad`; never write a second.

**Blocked** is a column: two failed runs in a row on one ticket trip the
breaker; a handoff in state `blocked` or `auth-required` blocks it; a person
can too (`corgi agent watch block ABC-123 "needs the sandbox key"`). A
blocked ticket takes no budget and no run until `corgi agent watch unblock
ABC-123` (also on the phone). A quota hit or a missing credential never
counts as a failure.

`corgi agent watch enable --isolate` gives every unattended run its own
worktrees on `corgi/<ref>`, one per repository, so a run never touches the
checkout; `watch undo` releases them (keeping any with uncommitted work).

### Scope, drift, models, routines, hardening

**Scope** is the contract a ticket's change stays inside, written after the
spec is agreed (the stories skill does it): `corgi agent scope set ABC-123
--path "api/limits/**" --lines 400 --tests 2 --done "…"`. Two hooks
`corgi agent track enable` installs hold the session to it: a write outside
the paths is refused with the way to widen (`corgi agent scope add ABC-123
--path …` — do it when the change needs it, and say why in the PR), and a
diff over budget is reported once when the turn ends (trim it, or raise the
budget on the record). Both are silent on a branch with no scope.

**Drift** is what the daemon concludes from the numbers on a live session:
context ≥ 85 %, the same tool failing three times running, a diff twice the
scope's budget (or past 800 lines with none), files outside the scope. The
key says `DRIFT` with the first reason; the phone offers **Fresh** — a clean
restart from a handoff under the same account (`corgi agent carry <session>
--fresh`). The other ways out are `/compact`, `/rewind`, or splitting the
branch by plan task.

**Models** are a policy per phase in the user config (`models:` under a
workspace or `defaults:`): `plan` and `review` on opus, `execute` and
`triage` on sonnet/haiku, `escalate` after a failed run, `kinds:` per event.
`corgi agent claude --model auto` starts on the policy's `auto` (opusplan by
default); the unattended runner picks per kind and steps up after a failure.

**Routines** are runs on a clock through the same runner: `corgi agent
routine add digest` (daily 08:30), `babysit-pr` (every 2h), `deps`,
`release-notes`, `flaky`, `doc-drift`, `suggest`, or `--prompt "…" --schedule
"daily 03:00"`. Each report is one inbox row with the log behind it; `routine
run <name>` runs one now. Caps, quiet hours and budget apply. `--bot <name>`
runs a routine as that bot (soul, model, account, filed under its name):
`bot add proactive --template proactive` + `routine add suggest --bot
proactive` is the Proactive bot — one thing worth building next, weekly, as
a task on the board (the `suggest-proactive` skill).

**Hardening**: `corgi agent harden` writes deny rules for secrets and
destruction plus a hook that refuses to write a credential into a file, into
the workspace's `.claude/settings.local.json`; `corgi agent doctor
--security` says what is still loose. `corgi surface` is the changed public
surface of the stack's diff (first section of every PR body, first thing a
review reads); `corgi docs check` the docs that name what changed and the
CLAUDE.md pointers that no longer land.

**Own tickets**: `corgi agent task add "Meta SDK on iOS" --workspace app
--body "…"` is a ticket the person writes themselves, kept on this machine
and shown on the same board as the tracker's — Todo, Doing, Review, Done —
and never sent to a tracker. `corgi agent watch work TASK-3` (or the phone's
**Work on it**) starts a session on it with the body as the prompt; the
session runs `corgi agent task move TASK-3 Review` when its draft PR is up
and `task done TASK-3` when nothing is left. `corgi agent refresh` reloads
everything now — sessions rescanned, every tracker polled, the board
published — when a person says the board looks stale.

`corgi agent kanban [--workspace X] [--json]` is one card per ticket in a
column corgi works out — Inbox, Ready (deferred, or a handoff waiting),
Running (a run or a session on the branch), Blocked, Review (a PR is open),
Done — with the session, the branch, the PRs, the handoff's next step and
what the ticket has cost (runs keep claude's own receipt; sessions on the
branch add their transcripts). The phone's Board tab draws the same. Moving a
card is `watch move`; nobody drags one into Running.

### What a workspace does on its own

Switches per workspace, each off until flipped, each read live — from the
CLI or the phone's repo sheet, no restart:

- `corgi agent watch enable --auto-allow reads` — the daemon answers a Read,
  Grep, Glob or web-search prompt itself in an iTerm2 session; never Bash,
  never a write. The row counts them (`autoAllowed`).
- `--done-when "go test ./...,pnpm lint"` — when a session stops with
  changes on its branch, the commands run in its directory; a red one is
  typed back as the next message (*Not done yet: … fix it, run it again,
  stop when green*), the row shows `gate` and *tests ✗*; three reds in a row
  ring a person instead. **Done means the checks say so.**
- `--compact-at 85` — `/compact` sent to a full session the next time it
  stops, once per episode.
- `--rebase` — a stopped session's clean branch is rebased onto main where
  it sits when main moved; with `--hand-over`, one that would conflict is
  told which files. The sweep measures `behind` (`main moved 12 · conflicts
  in api.go`) on every live branch.
- `--lessons` — reviews on the user's PRs, checks that stayed red and failed
  bot runs are written one line each to `<agentDir>/lessons/<workspace>.md`;
  `corgi agent lesson add|list`; the SessionStart hook tells you how many
  and the last one. **Read them before changing code.**
- `--hand-over`, `--auto-merge`: feedback typed into the session on
  the branch; a ready pull request merged.
- `--after-merge "<column>"`: once **every** pull request of a run is
  merged (by the daemon or by a person), the ticket moves to that column —
  a name `corgi agent watch board` prints. `--after-merge-subtasks Done`
  sends a subtask to a column of its own. Jira boards mostly; Linear's
  GitHub link usually moves the issue by itself, so leave it off there.
- `--batch 3`: tickets that arrive within 90 s of each other (a planner
  assigning a few at once) share one run — `/corgi:stories A B C` — one
  preflight and one context. Each still counts toward the caps; the notice
  says `fixed A + B + C`; a crash retries them one by one. 1 is off.
- What is a new ticket: one created assigned to the user, **or an old one
  a person hands over** — assigned to the user or moved to a column, by
  someone else (the changelog / issue history says who). The user's own
  moves and an automation's moves are not news. A parent with open subtasks
  of the user's is left to them (`its open subtasks are the work`); a
  subtask's run gets the parent's key and title for context.
- `corgi agent watch sweep --states "<not-started column>"`: the watch
  only sees what changes after it starts, so work already assigned before
  it was enabled never arrives. A sweep reads every open ticket of the
  user's once and hands the ones in that column to the daemon as new
  issues; seen, ran, ignored, blocked and parents-with-subtasks stay put.
  `--dry-run` lists first. Never sweep In Progress / In Review — that is
  the user's own hand work.
- A story run has three hours per ticket (a review 45 min): it builds,
  tests, opens the pull requests and watches CI.
- A run that crashed once (exit code, timeout) gets exactly one more go
  30 min later; a second crash trips the breaker (`watch unblock` clears
  it). `--no-retry` turns the retry off with the rest.
- `--approve`: an unattended review of a pull request someone asked
  the user to review may end in an approval — only when nothing blocks and
  the risk card says `auto-approve: yes`; otherwise findings, no stamp.
  Off by default: an approval carries the user's name. `--auto-for all`
  (or `requests`) is what makes review requests run at all; without it
  they only notify, and with `--silent` not even that.
- `--bots`: comments from bot accounts count. Off, a bot is not a
  person waiting — right for a coverage or pipeline bot, wrong for an AI
  reviewer whose findings are meant to be fixed. Ask which account posts
  those before turning it on.
- Comment fixes settle: a fix for a comment on a pull request
  starts a minute after the **last** comment on it — reviewers and review
  bots post several in a row — and one run reads every open thread, so
  three comments are one fix. A comment that lands while a fix is running
  is one more run after it, through the same settle, never a parallel one.
  The user's own comments never count; a merged or closed pull request is
  skipped before any run.

**Several tries at once**: `corgi agent watch work ABC-123 --attempts 3
--models opus,sonnet` opens three sessions in worktrees of their own on
`corgi/ABC-123-N`; `corgi agent attempts ABC-123` compares them (status,
changes, tests, done-when, cost, PR) and `attempts pick ABC-123 2` keeps
one. The phone's "Try 3 ways" does the same.

**Bots** (`corgi agent bot add reviewer --template reviewer --on
pr.review`) act on their own when their kind arrives; a failed run retries
once a model up (haiku → sonnet → opus); `bot show` sums a ledger — runs,
failed, retried, PRs and how many merged, the bill.

**Reviews from anywhere**: `corgi agent watch pr approve|request|comment
<ref> [words]` posts on the pull request the row is about, the user's name
on it. **Mute**: `corgi agent mute [1h|off]` — nothing rings, the board goes
on; the phone, the bar and a deck key flip it. **Transcript**: `corgi agent
transcript <session> --json` reads a conversation the way the phone does.
**Any agent on the board**: `corgi agent event start|prompt|tool|done|fail|
permission|stop|end --agent codex --session $ID` from another CLI's hooks.
**A read-only phone**: `corgi agent up --viewer` (or `corgi agent pair
--viewer`) pairs a teammate who can look but press nothing.

**Pairing without a QR**: `corgi agent pair` opens a fresh window
on the running server; `--file` writes `~/Desktop/<laptop>.corgipair` to
AirDrop to the phone, which opens it with corgi and is paired. With
corgi-bar the Mac is findable by a phone beside it the AirDrop way
(Bonjour over Bluetooth / peer-to-peer Wi-Fi / LAN, relayed to the daemon),
no tunnel needed nearby; Handoff shows the session in front on the iPhone's
lock screen. A quick tunnel's address dies with a restart: `agent up` warns,
pushes the new address to paired phones (they relink themselves), and
`corgi agent tunnel setup <host>` makes one that never changes.

From an MCP client: `corgi_watch_switches` reads all of the above per
workspace, `corgi_watch_set` flips the live ones (autoAllow, doneWhen,
compactAt, rebase, lessons, handOver, autoMerge — when the user asks for the
behaviour), `corgi_agent_mute`, `corgi_agent_attempts` (with `pick`),
`corgi_agent_lessons` (with `add`).

**The stack from the phone**: with a `corgi-compose.yml` the repo sheet
lists the services at a glance and starts, stops, restarts or tests them
(`/launch/stack`); without one the section stays away. **The whole diff**:
`/launch/diff?all=1`.

## Two laptops on the same trackers

Two laptops that watch the same Linear project or GitHub repo would each start
a fix and each ring the phone. Paired as **peers** they agree: for every
tracker both watch, one leads — it fixes, runs bots and routines, and rings;
the other records the events and stays quiet. A leader that stops pulsing for
three minutes (asleep, shut) is replaced by the next one, and it takes the
lead back when it returns. Sessions and their permission prompts are still
per laptop — those are not shared work.

- The phone pairs with both → it introduces them (`POST /launch/peers/invite`
  on the one it knew, `POST /launch/peers/join` on the new one). Nothing to run.
- By hand: `corgi agent peers invite` on laptop A prints a URL and a
  ten-minute code; `corgi agent peers join <url> <code>` on B pairs both ways.
- `corgi agent peers` — who is paired, awake, leading, and what each watches.
- `corgi agent peers lead` — this laptop leads every tracker it shares (the
  home machine that should do the unattended work); `lead off` returns to
  first-by-name.
- `corgi agent peers rm <name>` — forget one (run it on both).

What the pulse carries (2.29.1): each laptop's board (sessions, tickets,
branches, what waits), its unattended runs of the day (running, failed with
why, blocked), the tickets it ignored, how much of its five-hour window is
free, and whether it can work at all. So:

- `corgi agent sessions` and `/launch/board` (`peers[]`) show the other
  laptop's sessions under "on <name>" and what failed there — never its token.
- **Who leads, in order:** a laptop that can work (login fine, window not
  spent) before one that cannot; then the one marked `peers lead`; then the
  one with clearly more budget (a ten-point gap); then the first by name. A
  laptop left alone for weeks whose Claude login lapses stops leading on its
  next pulse, the other rings once "home cannot run fixes (its agent wants a
  login)", and takes over. When it can again, it leads again.
- `corgi agent watch ignore ENG-5` on one laptop ignores it on both;
  `corgi agent mute 60` on one mutes both for the hour.
- The lead moving (a laptop went silent, or came back) rings once — unless
  it flapped within fifteen minutes, then it is only logged.
- `corgi agent doctor --away` has a `peers` line; Telegram `/peers` prints
  the same summary. `corgi agent peers --json` never prints tokens.
- Same ticket, or same branch of the same repo, open on both laptops →
  "crossing laptops" rings once.

**A laptop alone for weeks** (see also `corgi agent doctor --away`): keep it
plugged in with the lid open or `corgi agent awake`; turn off automatic
macOS restarts (a reboot lands on the FileVault password screen and nothing
runs until someone types it); `corgi agent install` so the daemon returns
after a crash; tunnel credentials (cloudflared, ngrok) and peer tokens do not
expire; a Claude login that does lapse takes only that laptop out of the
lead — the other keeps working, and `corgi agent profile add` there gives it
a second account to fall back on.

How it holds together: a peer is a device in the other laptop's
`devices.json` with role `peer` — a bearer token plus the same end-to-end key
a phone gets — and that token opens only `/launch/peers/pulse` and
`/launch/peers/join` (never the board, a transcript or a send). Peers pulse
each other once a minute with the trackers they watch (`linear/<project>`,
`github/<owner/repo>`), stored in `<agentDir>/peers.json` (0600). Both need a
URL the other can reach: a tunnel each, or the same Wi-Fi.

## Codex as the harness

`kind: codex` on a workspace or a profile in the user config makes corgi drive
Codex there: `corgi agent claude` opens `codex` (permission modes map:
`acceptEdits` → `--full-auto`, `bypassPermissions` → no sandbox), and the
daemon's fixes, bots and routines run `codex exec --json` and read its
receipt (thread id, tokens). `corgi agent codex` opens it once (`corgi agent claude --kind codex` is the same); `corgi agent watch enable
--kind codex` sets the workspace's kind for good.

**An order of agents** (`agents: [claude, codex]` on a workspace or under
`defaults`; `corgi agent workspaces agents <id> claude,codex`, `--default`,
`corgi agent init --agents`, `watch enable --agents`) is what runs when the
first cannot. `utils/agent/daemon/harnesses.go`: every unattended run calls
`pickHarness` — the first agent that is installed, whose window is not spent
(claude's `usage` limits) and whose newest finished run of the day in this
workspace did not fail on a login, a permission or a limit (`FixRecord.Harness`
tags each run, `watch.Sidelining`). Nothing is remembered between runs. A
fallback drops `CLAUDE_CONFIG_DIR` (its own home, its own login). A spent
window is no reason to defer (`fixDeferral`) when a fallback can run, and the
peers hear `unwell` only when every agent is out (`laptopUnwell`; the MCP-side
pulse reads `watch/agents.json`). The daemon logs the pick and rings once a day
per workspace and agent. `corgi agent claude` opens the first installed agent
of the order; `--kind` overrides. `corgi agent doctor` has an `agents` check. `corgi agent track enable`
writes Codex's hooks when the installed codex runs them (`codex features
list` shows `hooks … true`): `~/.codex/hooks.json` (`CODEX_HOME`) gets
SessionStart (emit + context), UserPromptSubmit, PreToolUse,
PermissionRequest, PostToolUse, Stop (emit + budget) and SessionEnd, all
`corgi agent hook emit --agent codex` — the same stdin JSON as Claude Code,
so a codex row has its status, tool, risk word, TTY / tmux pane / window,
context % (from the rollout's `token_count`) and a Stop summary (from
`last_assistant_message`); corgi's old notify line is removed so a turn is not
reported twice. Codex fires PermissionRequest before its own reviewer
answers, so with `approvals_reviewer = "auto_review"` (top level of the
project's or the user's `.codex/config.toml`) or `permission_mode:
bypassPermissions` the emit hook records it as PreToolUse — work, not a
wait, no "needs you" ring. Codex runs a hook only once the person trusts it in `/hooks`
(the trust is a hash in `config.toml` under `hooks.state`) — corgi never
writes that trust. An older codex keeps the `notify` line (someone else's
notify is left alone, since Codex runs one). `configDir` moves `CODEX_HOME`;
`OPENAI_API_KEY` is the credential. `pickHarness` reads codex's window from
the newest rollouts' `rate_limits` (`usage.ReadCodexWindow`): 98 % or
reached, before its reset, is `limit`, wherever codex stands in the order. Adding another agent is one more
entry in `utils/agent/harness`.

**One session, either harness (2.30.17).** `harness.Open` is what a person's
session asks for — permission mode, model, instructions, resume, first
prompt — and each harness spells it (`OpenArgs`): claude `--permission-mode
bypassPermissions --model --append-system-prompt --resume --fork-session`,
codex `resume <id> --dangerously-bypass-approvals-and-sandbox -m` with the
instructions in front of the prompt. `corgi agent claude|codex` builds it in
`resolveLaunch`, so a workspace's `dangerouslySkipPermissions: true` opens
codex without approvals (what `codex --dangerously-bypass-approvals-and-sandbox`
does by hand), and a bot's soul, model and last thread go through whichever
opens. `configDir` reaches only the first agent of the order: codex under a
claude account keeps its own `~/.codex` login. A model of the other harness
(`opus`, `sonnet`, `opusplan` on codex; `gpt-*`, `o3` on claude) is dropped
so the run goes ahead on the default (`Harness.Model`), which is how the
daemon's model policy, written in claude's words, survives a codex fallback.
`corgi agent new --agent codex` and `/launch/new {agent}` open a named harness
from the editor or the phone.

**Handing work across harnesses.** A conversation cannot cross harnesses, so
`corgi agent carry <session> --to codex` (or `--to claude`) is always a fresh
start: the handoff packet is written (`From.Harness` is the session's own),
and `corgi agent codex --workspace <id> --prompt-id …` opens in the same
workspace with the packet as its first prompt. Only an agent the workspace
lists in its `agents` order may take it (`checkHandTarget`). A codex session
carried to another account starts fresh too (its threads live in
`~/.codex`), and cannot `--fork`. `POST /launch/carry {session, to | profile |
fresh}` is the phone's route; the ended row keeps `agent`, so a headless turn
for a gone codex session runs `codex exec resume <thread>`.

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
