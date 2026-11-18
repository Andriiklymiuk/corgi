# Starting a session on demand (remote session start)

The daemon normally supervises only `autostart` workspaces. `corgi_session_start`
starts a session in **any** registered workspace, from a phone or any paired MCP
client - the fix for "I forgot to enable that repo before leaving the laptop".

```
corgi_session_start { "workspace": "the recipe app", "profile": "work" }
```

- Returns immediately with `state: "starting"`. **Poll `corgi_agent_status`**
  until the workspace reports `running` - its `sessionUrl` is the magic moment:
  hand it to the user, one tap opens the conversation in that repo.
- Idempotent: an already-running workspace answers `state: "running"` with its
  URL. Ambiguous names return candidates - ask, as always. A workspace that is
  `running` with `deviceOnly: true` and `sessionsThisRun: 0` is being **swapped** for
  a session-opening server - poll on; the URL follows.
- `sessionUrl` is best-effort. If it never appears, the session still runs;
  tell the user to find it in claude.ai/code by the workspace's name.
- `corgi_session_stop { "workspace": "..." }` ends it. Stopping a non-running
  workspace is a clean no-op.
- CLI parity for local testing: `corgi agent session start <name> --profile work`,
  `corgi agent session stop <name>`.
- Every remote start and stop raises a desktop notification on the laptop, by
  design - the machine's owner always sees what began running.
- Phone push for those notifications: set `notifyUrl` in the trusted agent
  config - see **Getting notifications on a phone** in `setup.md`. The push carries a
  tap target back to the session.
- `corgi agent hooks enable [--all]` in a workspace also notifies when a Claude
  session there is waiting on a permission prompt. It writes into
  `.claude/settings.local.json`, never the committed file. `--turns` adds a ping
  on every finished turn and is off by default because it fires constantly.
- `corgi_pr_open { branch, title }` opens one pull request per repository that
  has commits on the branch and cross-links them - the step after
  `corgi_worktrees_materialize` and `corgi_diff`.

### Setting it up from a session on the laptop

**One command, from inside the stack directory:**

```bash
corgi agent up
```

It registers the current workspace - a corgi stack **or any git repository**
(no compose file needed) - starts the daemon (if down), opens the MCP endpoint
with a public tunnel, and prints a **scannable QR** for pairing - all detached,
so you can run it and keep working. No port to remember. `--json` emits the URL
and pairing code for a caller that wants them structured. A busy MCP port
self-heals: when the holder is identifiably corgi's own server, `up` stops it
and opens a fresh tunnel + pairing window instead of refusing. For a launcher
URL that survives restarts - and a phone that stays paired, since the origin
never changes - pass `--tunnel-name <name> --tunnel-hostname <host>` (cloudflared
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
free tier) - ngrok shows an interstitial once on first open.

The user scans the QR (or opens the printed URL) on their phone, names the
device, and gets a per-device token. After pairing, the same page offers **Open
launcher** (`/app`): corgi's own phone UI that lists the machine's workspaces
and starts a session in one tap, then hands back the claude.ai link - no
claude.ai connector needed. Each workspace row has an **open in: app | browser
| chrome** switch (set on the phone, remembered per device) - point a workspace
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

1. **URL** - `corgi agent status` prints it on the `connector` line
   (`https://<host>/mcp`); `--json` has it as `connectorUrl`. It is the
   daemon's public tunnel origin plus `/mcp`; if the status says *no public
   URL yet*, the tunnel is not up - `corgi agent up`.
2. **Authentication → Sign in now** (the default Claude detects). corgi is
   its own OAuth server on that origin, so **Add** opens corgi's consent
   page in a browser tab. Approve it:
   - a browser that has opened `corgi agent dashboard` before shows one
     **Approve** button;
   - any other browser (the phone's, a fresh profile) shows a code - the
     user runs `corgi agent approve ABCD-2345` on the machine the daemon
     runs on, and the page finishes on its own. Ten minutes per code.
   Claude then holds a one-hour access token it refreshes for 30 days. It
   shows in `corgi mcp devices` as `Claude · oauth <id>`; `corgi mcp devices
   revoke "<that name>"` ends the grant and Claude asks to sign in again.
3. **Fallback, header path** - if the daemon runs with `--no-oauth`, or the
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
the tunnel come back after a reboot too, run `corgi agent up --at-login` once -
the daemon then repeats that up when it starts itself. Pair it with a named
tunnel, or the URL after a reboot is new and the phone has to re-pair.

A laptop that sleeps between sessions answers no phone tap: the wake lock is
per session by default. `corgi agent awake on` holds it for the daemon's whole
life - that is the replacement for a `caffeinate` left running in a terminal.

On macOS, a workspace under `~/Documents`, `~/Desktop`, `~/Downloads` or iCloud
Drive makes macOS ask to let corgi read it - and ask again after every corgi
upgrade, because corgi is ad-hoc signed. Suggest moving the stack to `~/dev`
(then `corgi agent workspaces relocate <id> <new path>`).

A "Claude is waiting for your input" toast when nothing is blocked is Claude's
60-second idle nudge. corgi drops it by default; `corgi agent hooks enable
--idle` puts it back.

### What travels through the corgi tunnel - and what does not

The corgi tunnel is a **control plane only**: it carries `corgi_session_start`,
status, diff, logs. It does **not** carry the conversation. The back-and-forth
with Claude runs in the **Claude app / Remote Control**, which talks to
claude.ai directly - the `sessionUrl` you open joins that, not anything corgi
proxies. So: corgi starts the session and hands you the URL; the Claude app is
where you actually talk. Two apps, by design.

### Which client calls corgi_session_start

`corgi_session_start` is an MCP tool, so any MCP client works. Today that is
the **Claude app as a custom connector** (the recipe above: connector URL
from `corgi agent status`, Sign in now, approve the consent page), then say
"start a session in the recipe app" and it calls the tool for you. A
dedicated companion app is a separate project (it must **not** live in the
corgi repo).

### Profiles - which Claude account runs

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
- A shell alias like `claude-work` is **not** a binary - the supervisor cannot
  exec it. Prefer `configDir:` with the default `claude`; if a different
  command is truly needed, make it a real script on PATH and set `bin:`.

### Letting the laptop sleep between turns

By default the daemon holds a wake lock for the whole session, so the Mac stays
awake even while the session is just waiting for the user. To let it sleep while
idle and wake back up when work resumes, set `wakeLock: idle` in the user
config (per workspace or under `defaults:`):

```yaml
defaults:
  wakeLock: idle    # session|always|off|idle - idle sleeps after ~5 min quiet
```

`off` never blocks sleep (a long build can be cut); `idle` keeps working
sessions awake but sleeps between turns.

For a laptop that leaves with its lid closed: `corgi agent awake on` (or
`--for 6h`) is read live and, with the lid closed, holds the display awake
too (macOS pauses apps once the display counts as asleep); `--display on`
keeps the screen lit while the lid is open; `corgi agent heat` holds new runs when warm and pauses sessions
when hot; `corgi agent hotspot "<name>"` hops onto the phone when Wi-Fi is
gone; `corgi agent away` lists the rest, sudoers rule included. Never ask the
person to run `caffeinate` in a terminal.

### If a remote start does not appear

| symptom | what to say / do |
|---|---|
| tool errors "daemon is not running" | `corgi agent serve` on the laptop (or `corgi agent up --at-login` so it comes back by itself). |
| everything gone after a reboot | `corgi agent install` restores the daemon only. `corgi agent up --at-login` restores the endpoint and tunnel too. |
| tool errors "predates remote session start" | The daemon is an older corgi. Restart it: `corgi agent stop` then `corgi agent serve` (or `corgi agent up`). |
| workspace `unreachable` | Drive not mounted or folder moved - `corgi agent workspaces relocate`. |
| workspace marked sensitive | Remote start is refused by design. Start it on the laptop, or unset `sensitive` in `.corgi/agent.yml`. |
| queued but nothing started | Commands expire after 60s. Check `corgi agent status` diagnostics - a rejected start says why there. |
| running but no `sessionUrl` | The session is fine; the URL was not spotted in output. Find it in claude.ai/code. |
| status says `online`, launcher says *online · no session* | Not a failure: a supervised server waiting as a device. Start from the launcher or the Claude app's device list opens a session. |
| my claude.ai list is full of `<ws> · main · HH:MM` rows | Leftovers from an older corgi - see *Supervised servers are devices* in `setup.md`. Archive them once. |
| status shows *note: this Claude Code predates --no-create-session-in-dir* | `claude update`, then `corgi agent restart`. |
| `corgi agent sessions` is empty though Claude is running | Hooks not installed, or installed after the session started: `corgi agent track enable`, then `corgi agent rescan`; new sessions report from their next event. |
| a Stream Deck / `focus` press does nothing | `corgi agent doctor` → a session with `unknown` host. Install the corgi VS Code extension and reopen the terminal; iTerm2 needs the tty (a session started before tracking shows none until its next event). |
| VS Code tabs still say "claude" | Set `terminal.integrated.tabs.title` to `${sequence}` - the default `${process}` ignores the title the hook sets. |
| `up` says the port is in use, pairing "not open" on the old URL | A leftover MCP holds the port. Newer corgi reclaims it on `up` automatically; otherwise `corgi agent down` then `corgi agent up` for a fresh tunnel + pairing window. |
