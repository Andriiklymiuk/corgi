# Agent mode

Agent mode makes your machine an always-on, multi-repo host for Claude Code's
[Remote Control](https://code.claude.com/docs/en/remote-control).

It is opt-in. If you never run `corgi agent init`, nothing changes.

## What it is for

Remote Control already gives you a Claude Code session on your own machine,
driven from the Claude app or claude.ai/code. Two things stop it being the thing
you actually want:

1. **It is not always on.** Its docs are explicit: *"If you close the terminal,
   quit VS Code, or otherwise stop the `claude` process, the session goes
   offline"*. An extended network outage ends it too — in server mode, the one
   corgi supervises, *"Claude Code gives up after roughly 10 minutes and the
   `claude remote-control` process exits"* (an interactive session instead
   retries for as long as the outage lasts). Either way you have to remember to
   arm it again — and forgetting is the failure this exists to remove. Check the
   [Remote Control docs](https://code.claude.com/docs/en/remote-control) for the
   current wording; the exact deadlines move.
2. **It sees one directory.** A corgi stack is several repositories, databases,
   and the env wiring between them. Remote Control sees none of that.

corgi fixes exactly those two. It does not reimplement sessions, streaming,
approvals, or cost tracking — Remote Control does all of that well, and doing it
again would be worse.

```
  phone / claude.ai/code
        │
        ▼
  claude rc --spawn=worktree      ◄── supervised by corgi, not replaced
        │                                   │
        │ calls MCP tools                   │ restarts after the 10-minute exit,
        ▼                                   │ a crash, or a reboot; holds a wake lock
  corgi mcp                            corgi agent serve
        ├─► which stack is "the recipe app"?
        ├─► a worktree per repo, one branch
        └─► one diff across every repo
```

## Quick start

One command does the whole phone-startable setup — register this workspace (a
corgi stack **or any git repository** — no compose file needed), start the
daemon + MCP + tunnel + pairing, and print a QR (and the pairing code):

> A registered workspace is phone-startable, so register directories you mean to
> work in. In particular, if your `$HOME` is itself a git repo (dotfiles), running
> `agent up` there registers your entire home directory — probably not what you
> want.

```bash
cd ~/dev/your-stack
corgi agent up                   # register + daemon + tunnel + pairing, prints a QR
corgi agent up --at-login        # the same, and it all comes back after a reboot
```

Everything `agent up` starts is **detached**, so it survives crashes and the
Remote Control network timeout — but not a reboot, unless you say so once.

### Surviving a reboot

`--at-login` installs the platform service (launchd / systemd) **and** records
this up, so the daemon repeats it when it next starts itself: the MCP endpoint,
the tunnel and the pairing server come back with it. In a terminal, a bare
`agent up` offers the same thing once and remembers your answer.

`corgi agent install` on its own is the daemon half only — supervised sessions
come back, the endpoint your phone talks to does not. Run it after an `up` and
it adopts that up; run it on a machine that has never done one and it says so.

```bash
corgi agent up --at-login        # daemon + endpoint + tunnel at login
corgi agent up --at-login=false  # stop doing that (the service is left alone)
corgi agent uninstall            # remove the service entirely
corgi agent status               # "start at login: launchd — daemon, MCP endpoint and tunnel"
```

A quick tunnel gets a **new URL** each time it comes back, so the phone has to
re-pair. Pair a named tunnel with it (`--tunnel-hostname`) and the URL after a
reboot is the one already saved on the phone.

Or do it step by step:

```bash
cd ~/dev/your-stack
corgi agent init                 # register AND enable this stack
corgi agent install              # start at login (launchd / systemd)
corgi agent status               # what is running, and under which account
```

### Which path the phone should use

Pick by where the phone will be, not by which tunnel sounds best:

| where the phone is | run this | URL to save on the phone |
|---|---|---|
| on the same Wi-Fi | `corgi agent up --http 0.0.0.0:8765` | `http://<lan-ip>:8765/app` |
| anywhere, domain on Cloudflare | `corgi agent tunnel setup corgi.yourdomain.com` | `https://corgi.yourdomain.com/app` |
| anywhere, no domain | `corgi agent tunnel setup <yours>.ngrok-free.dev --provider ngrok` | that host + `/app` |
| one-off, re-pairing is fine | `corgi agent up` | changes on every restart |

The Wi-Fi row uses no tunnel, no DNS and no provider, so it is the fastest and
the least breakable — reach for it first when you are at home. The launcher is
token-protected either way, so serving it on the local network is not serving it
to the internet. The middle two rows keep a fixed origin, which is what keeps
the phone paired across restarts; the quick tunnel does not.

### A launcher URL that never changes

The default is a Cloudflare **quick tunnel**: free, no signup — and a **new random
URL every restart**, so the phone bookmark goes stale whenever `agent up` reruns.
For a permanent URL, use a **named tunnel**:

```bash
# one-time (free Cloudflare account + a domain on it):
cloudflared tunnel login
cloudflared tunnel create corgi-agent
cloudflared tunnel route dns corgi-agent corgi.yourdomain.com

# then always:
corgi agent up --tunnel-name corgi-agent --tunnel-hostname corgi.yourdomain.com
```

Both flags: the name picks the tunnel, the hostname is the URL corgi prints
(cloudflared does not report it). The launcher then lives at
`https://corgi.yourdomain.com/app`, and because the origin never changes the
phone stays paired across restarts.

`agent up` remembers the settings it last ran with, so from then on a bare
`corgi agent restart` keeps the named tunnel. Pass the flags again to change
them; `--tunnel-hostname ""` goes back to a quick tunnel.

No domain on Cloudflare? ngrok's free tier already gave your account one static
`*.ngrok-free.dev` **dev domain** and needs no DNS work. You cannot choose its
name — picking one is a paid feature — but the assigned one never changes:

```bash
brew install ngrok
ngrok config add-authtoken <token>   # dashboard.ngrok.com → Your Authtoken
# dashboard.ngrok.com → Domains → copy the row tagged `dev domain`, then:
corgi agent up --provider ngrok --tunnel-hostname <yours>.ngrok-free.dev
```

The first time the phone opens the page, ngrok's free tier shows its own
"you are about to visit" interstitial once; tap through. The launcher's own
requests carry the header that skips it, so pairing and the buttons work.

Either way the launcher lives at the same hostname forever — save it to the
phone's home screen once. One command does the whole setup and remembers it:

```bash
corgi agent tunnel setup corgi.yourdomain.com                  # cloudflared
corgi agent tunnel setup <yours>.ngrok-free.dev --provider ngrok
```

Per-service stable tunnels: [docs/tunnel.md](tunnel.md#stable-urls-named-mode).

One more knob worth knowing: a workspace's Claude **default model** is not corgi's
to pick — `claude remote-control` takes no model flag; you choose it per message in
the Claude app. But the `model` setting in that workspace's config dir
(`<configDir>/settings.json`) sets the default a new session starts with, and it
rides along with `--config-dir` / profiles automatically.

`corgi agent scan <dir>` registers stacks it finds but **does not enable them**.
Supervision is opt-in per workspace: scanning a projects folder should not
quietly spawn a Claude session for every stack in it. Run `corgi agent init` in
the ones you actually want running.

Then open the Claude app. Nothing to arm.

To check it can work before committing to it:

```bash
corgi agent doctor
corgi agent serve --foreground   # run it in this terminal and watch
```

## Commands

| command | what it does |
|---|---|
| `corgi agent up` | one shot: register + daemon + tunnel + pairing, prints a QR |
| `corgi agent init` | register this stack, write `.corgi/agent.yml` |
| `corgi agent scan <dir>` | find stacks under a directory and register them (does not enable) |
| `corgi agent serve` | supervise Remote Control for every enabled workspace |
| `corgi agent install` / `uninstall` | start (or stop starting) at login — daemon only |
| `corgi agent up --at-login` | the same, plus the endpoint and tunnel that up used |
| `corgi agent awake [on\|off]` | keep the machine awake for the daemon's whole life |
| `corgi agent status [--json]` | what is running, restarts, which account |
| `corgi agent doctor [--json]` | can this work here, and what to fix |
| `corgi agent workspaces` | list, `forget`, `relocate` |
| `corgi agent resolve <name>` | what "the recipe app" resolves to |
| `corgi agent brief [id]` | what the last session was working on before it restarted |
| `corgi agent logs <workspace>` | the session timeline: starts, exits and why, links |
| `corgi agent restart` | `down` + `up --fresh` in one — run it after `corgi upgrade` |
| `corgi agent tunnel setup <host>` | one-time permanent-URL setup, remembered for later runs |
| `corgi agent hooks enable` / `disable` | notify when a session in this workspace needs you |
| `corgi agent stop` | stop the daemon |

## Restarts, and being told about them

Remote Control exits on the ten-minute network timeout, on a crash, and on
reboot. corgi restarts it with capped backoff and **says so**:

> `rc restarted 14:32 · previous session ended (network timeout) · worktrees kept`

The notification matters. A relaunched Remote Control starts a **new** session,
so the previous conversation's context is gone. Restarting silently would look
like continuity and cost you an hour of confusion.

### Notifications on your phone

By default notifications are desktop only. Add a `notifyUrl` to the trusted
agent config and every one is also POSTed there. corgi picks the payload from
the host, so point it at whatever you already have:

```yaml
# <agent data dir>/config.yml
notifyUrl: https://api.telegram.org/bot<TOKEN>/sendMessage?chat_id=<ID>
```

**Telegram** is the free option on iOS, and corgi sets it up for you:

```bash
corgi agent notify telegram --token <TOKEN>   # from @BotFather → /newbot
```

It checks the token, waits while you message the bot (a bot cannot open the
conversation), reads the chat id out of that message, writes `notifyUrl` and
sends a test. `--chat-id` skips the wait if you already know it.

**Slack** is a channel's Incoming Webhook URL, **Discord** a channel →
Integrations → New Webhook URL — `corgi agent notify set <url>` for either.
Anything else gets the ntfy shape (body as plain text, title in the `Title`
header), which suits a **self-hosted** ntfy — the ntfy.sh iOS app itself is
paid, Android is free.

```bash
corgi agent notify show   # what is configured, secret masked
corgi agent notify test   # send one to that destination now
corgi agent restart       # a running daemon reads notifyUrl at startup
```

**The restart is the step people miss.** The webhook is attached when the daemon
starts, so a `notifyUrl` written under a running daemon reaches nothing until it
is restarted. Note also that `corgi notifications test` only exercises the
**desktop** path — `corgi agent notify test` is the one that posts to the URL.

Trusted config only: the URL receives restart reasons, so a committed repo file
can never set it. Treat the value as a secret — a Telegram token lets anyone
post as that bot, so never paste it into a chat, a commit or an issue. If one
leaks, `/revoke` in @BotFather issues a new one; the chat id does not change.

### The timeline

Every start, exit (with its classified cause), disable, and captured session
link is appended to a small per-workspace timeline — never session output,
which can hold secrets and is not persisted:

```bash
corgi agent logs api            # newest first: started, exited · why, links
corgi agent logs api --json     # same, for scripts and agents
```

The launcher's per-workspace session list is fed from the same timeline, so
past sessions survive daemon restarts, and the `corgi_session_events` MCP tool
exposes it to a connected Claude.

The same list shows every Claude Code process running in the workspace —
terminal, VS Code, supervised — read from the per-process records under
`<configDir>/sessions/`. A process that registered a web id links straight to
its conversation on claude.ai; one that has not is marked *local only*, and
typing `/remote-control` inside it is what gives it a link.

### What it has been costing

Claude Code records token counts in its own transcripts, so corgi can add them
up per workspace — nothing is sent anywhere, and only the numbers are read:

```
$ corgi agent status
  corgi                running   restarts=0 wakeLock=true
                       tokens 147.3M today · 1.0B this week
```

The launcher shows the same two numbers on each card. Cache reads are included,
which is why the totals are large — that is the real traffic against the window.

### Hiding a workspace on the phone

Each card has a **hide** chip. Hidden cards collapse into one `N hidden — show`
button. It is stored in that browser only and changes nothing on the machine —
it exists for the moment someone else is looking at your screen. To actually
stop supervising a workspace, set `autostart: false` for it instead.

### If the phone cannot reach it

The tunnel is the fragile part, and three things break it in ways that look
like corgi being down:

- **The Mac sleeps between sessions.** The wake lock is held per session by
  default, so with nothing running the laptop dozes off and a phone tap reaches
  nothing at all. `corgi agent awake on` holds it for as long as the daemon
  runs. On battery `caffeinate -i` still works — only `-s` is AC-only — so what
  actually ends a session is a closed lid or a flat battery.
- **A blocked tunnel domain.** Most networks are fine here — plenty of people
  run a `*.trycloudflare.com` link over cellular for years and never hit this.
  But that domain and `*.loca.lt` are on enough blocklists that *some* carriers
  and filtering resolvers refuse them, and then the same link works on Wi-Fi and
  does nothing on cellular. It is per-network, so test rather than assume in
  either direction: one carrier that dropped both of those resolved ngrok's
  `*.ngrok-free.dev` fine, and someone else's phone may well have been fine on
  all three. If yours is refused, the ngrok dev domain is the free thing to try
  next:

  ```bash
  corgi agent tunnel setup <yours>.ngrok-free.dev --provider ngrok
  ```

  A hostname you own is on no list at all, and is the answer if the free ones
  are refused too: `corgi agent tunnel setup corgi.yourdomain.com`.
- **Nothing at all, if the phone is on your Wi-Fi.** Serve on the local network
  and skip tunnels entirely:

```bash
corgi agent up --http 0.0.0.0:8765     # prints http://<lan-ip>:8765/app too
```

That path has no DNS, no provider and no public exposure — it is the one to
reach for first when you are at home, and the fallback when a tunnel misbehaves.
Back to loopback with `corgi agent restart --http 127.0.0.1:8765`.

When it is unclear whether corgi or the tunnel is at fault, two requests settle
it. Run both on the machine:

```bash
curl -so /dev/null -w '%{http_code} %{time_total}s\n' http://127.0.0.1:8765/app
curl -so /dev/null -w '%{http_code} %{time_total}s\n' https://<public-host>/app
```

A slow or failing loopback request means corgi itself — `corgi agent status`,
then `corgi agent restart`. A fast loopback and a failing public one means the
tunnel is not delivering; switch provider, or use the Wi-Fi path above. Both
fast means the machine's side is healthy and the phone's network is the
problem. Confirm that on the phone itself, with Wi-Fi turned off — the machine
shares its own DNS and route with that second request, so a `200` there does not
prove a phone on cellular can resolve the name. Do not substitute a public web
proxy for the phone: ngrok and Cloudflare throttle them, and the `522` you get
back says nothing about your tunnel.

### When a session needs you

A session waiting on a permission prompt is invisible from the phone. Opt in
per workspace and corgi tells you:

```bash
cd ~/dev/your-stack
corgi agent hooks enable        # writes the needs-you hook into .claude/settings.local.json
corgi agent hooks enable --all  # or every registered workspace at once, from anywhere
corgi agent hooks enable --all --turns   # also ping on every finished turn (noisy)
```

Only the permission prompt notifies by default: it blocks the session until you
answer, while a finished turn fires constantly once several workspaces are busy.
Notifications go to the machine running corgi — **set `notifyUrl` to reach your
phone**, or the hooks only reach the desk you were trying to leave. On macOS with
`terminal-notifier` installed, clicking the desktop toast opens that workspace's
session (or the launcher when corgi does not know a session URL for it).

The toast is read at the machine corgi runs on, so the launcher it opens is the
one served from **localhost**, not the public tunnel URL: no round trip out to
the internet and back, and it still works when the tunnel is down. The phone
push keeps the public URL, which is the only one that is any use to a phone.

`notifyUrl` picks its payload from the host, so it is not ntfy-only — useful
because ntfy's **iOS** app is paid:

| host | what it sends | free on iOS |
|---|---|---|
| anything else | ntfy shape: body + `Title`/`Click` headers | self-hosted yes, ntfy.sh app no |
| `discord.com` | `{"content": …}` | yes |
| `hooks.slack.com` | `{"text": …}` | yes |
| `api.telegram.org` | `{"text": …, "chat_id": …}` | yes |

For Telegram put the chat in the URL and corgi copies it into the body:
`https://api.telegram.org/bot<TOKEN>/sendMessage?chat_id=<ID>`. Each message
carries the title, the detail, and the link to open.

They call `corgi agent hook`, which reports to the daemon, which sends the same
notification as a restart — including the phone push when `notifyUrl` is set.
It covers every Claude session in that directory, supervised or not.
`corgi agent hooks disable` removes them and leaves your other hooks alone.

### The handover brief

corgi cannot restore the conversation. What it can keep is the half that
survives on disk, captured in the gap between the old process exiting and the
new one starting — the only moment that state is both final and current:

```bash
$ corgi agent brief acme-stack
acme-stack
  ended   2026-08-14 14:32 (network-timeout)
  reason  remote control restarted — the previous session ended (network timeout)
  state   was on feature/referral · 1 repo has uncommitted changes
    api              feature/referral (worktree)
    web              feature/referral · uncommitted changes (worktree)
```

The worktrees are the part worth having. A branch spread across four
repositories is invisible from a fresh session's working directory, and nothing
else would tell the new session it exists. The summary line is appended to the
restart notification, so the lock screen says *where* as well as *that*.

Uncommitted work counts untracked files, unlike the check that guards worktree
removal. Creating files is the most common thing an agent does, and a note
calling that "clean" would be worse than no note. `.gitignore` is respected, so
build output does not inflate the count.

A session that ends because you asked it to gets no brief — you do not need a
handover note for something you just closed. Only the most recent is kept per
workspace, and `corgi agent workspaces forget` drops it, so a reused id cannot
surface another stack's branches.

From a phone, the same thing is `corgi_session_brief`. Call it first when
picking work back up.

Not every exit is worth retrying:

| cause | what corgi does |
|---|---|
| network timeout | restart, notify |
| crash | restart with backoff, notify |
| exits immediately, repeatedly | stop after 5, disable the workspace, notify |
| auth failure | **do not restart** — retrying cannot produce credentials |
| `corgi agent stop` | stay stopped |

## Wake lock

A machine that sleeps mid-session kills the session, the stack, and any tunnel.
Remote Control does not take a lock, so corgi does — scoped to the session, not
to forever, because an always-awake laptop is a flat battery.

- macOS: `caffeinate -i -m -s -w <pid>`. The `-w` ties the lock to the
  supervised process, so a crash cannot leave your machine awake overnight.
- Linux: `systemd-inhibit --what=idle:sleep`
- Configurable per workspace: `wakeLock: session | always | off`. On a desktop
  you want `off`.

### Staying awake between sessions

Per-session scope leaves the worst gap open: with no session running, nothing
holds the lock, the laptop sleeps, and the phone tap that would have started one
reaches nothing. That is the gap people paper over with a `caffeinate` left
running in a terminal all day.

```bash
corgi agent awake on       # hold the lock for the daemon's whole life
corgi agent awake          # what is set now
corgi agent awake off      # back to per session (the default)
corgi agent restart        # so the running daemon picks it up
```

It writes `stayAwake: true` into the user config, and the daemon takes
`caffeinate -i -m -s -w <its own pid>` at startup. Off by default, because a
machine that never sleeps is a flat battery and that is the owner's call.

**Honest limits:** `caffeinate -s` (prevent system sleep) is AC-only, but `-i`
(prevent idle sleep) is honoured on battery too, so an idle laptop on battery
does *not* doze off with the lock held. What still ends a session is closing the
lid, or the battery running out. "Lid closed on the train" only works plugged
in; if that is your workflow, supervise from a machine that stays on.

## Supervising an agent other than Claude Code

Everything the supervisor actually does — restart after the ways a session dies,
hold a wake lock, scope credentials and config directory per workspace — is the
same whichever agent CLI is running. Only the launch details differ, so those
are a `kind`:

```yaml
workspaces:
  acme-stack:
    autostart: true
    kind: custom
    bin: some-agent            # a command name on PATH, never a path
    args: [serve, --headless]  # the argv, in full
    configDirEnv: SOME_AGENT_HOME
    credentialEnv: [SOME_AGENT_API_KEY, SOME_AGENT_OAUTH_TOKEN]
```

| kind | what it launches |
|---|---|
| `claude` | `claude remote-control …`, built from `spawn`, `capacity`, `permissionMode`. The default, so an existing config is unchanged. |
| `custom` | exactly the `args` you wrote, after `bin`. |

**Why `custom` takes the whole argv rather than corgi guessing flags.** A
supervised process runs unattended, and a flag corgi invented for a CLI whose
interface it cannot verify would fail at 3am with a message nobody sees. Writing
the command out means what runs is what you tested in a terminal. Built-in kinds
exist for CLIs whose flags corgi can be sure of; adding one is a map entry in
`utils/agent/supervisor/kind.go`.

Three rules carry over unchanged, because they are what makes supervision safe
rather than merely convenient:

- **`args` is trusted config only.** An argv is a choice of what code runs, so
  there is deliberately no field in the committed `.corgi/agent.yml` that
  reaches it. A cloned repository cannot choose the command.
- **Untrusted config may never disarm the permission prompts.** `--dangerously-*`
  and `--yolo` smuggled through `args`, and a `permissionMode: bypassPermissions`
  string, are rejected. The one way to skip prompts is the explicit
  `dangerouslySkipPermissions` boolean in your trusted user config (see [Running
  more than one Claude account](#running-more-than-one-claude-account)) — never
  a committed repo file.
- **A setting that cannot take effect is an error.** `spawn` and
  `permissionMode` on a `custom` kind are rejected rather than dropped, and so
  is a `configDir` with no `configDirEnv` to put it in — silently ignoring the
  last one would leave the workspace on the default account, which looks exactly
  like being on the right one.

## Running more than one Claude account

If you keep work and personal logins separate, this is the section that matters.

Multi-account setups are almost always shell aliases:

```bash
alias claude-work='CLAUDE_CONFIG_DIR=~/.claude-work claude'
```

**launchd and systemd never source your shell rc files.** A supervised session
would therefore run under your *default* account — no error, no warning,
correct-looking output, wrong account.

So corgi sets `CLAUDE_CONFIG_DIR` per workspace explicitly:

```bash
corgi agent init --config-dir ~/.claude-work
```

Each supervised process gets an environment corgi builds itself, rather than
inheriting the daemon's. Ambient `ANTHROPIC_API_KEY` and
`CLAUDE_CODE_OAUTH_TOKEN` are **stripped** unless a workspace opts in — Remote
Control refuses to start with an API key set, and an inherited one bills the API
instead of your subscription.

`corgi agent status` prints which account each workspace will actually use.
That one line prevents the most likely surprise in agent mode.

This is environment and config-path scoping, not a sandbox. It prevents
accidents. It does not contain a compromised session. For a real boundary,
give each account its own config directory and keep sensitive stacks separate.

## Configuration, and why it is split in two

Settings live in two files with different trust levels.

**`.corgi/agent.yml` — committed, untrusted.** It arrives with a `git clone`
and was written by whoever wrote the repository, who may not be you. So it holds
identity only:

```yaml
version: 1
workspace:
  id: acme-stack
  aliases: [acme, recipe app]
  sensitive: false      # true ⇒ never open a public tunnel for this workspace
```

**The user-level file — never committed, `chmod 600`, trusted.** It holds
everything that grants capability, and corgi refuses to read it if it is
readable by other users:

```yaml
version: 1
defaults:
  spawn: worktree
  capacity: 4
workspaces:
  acme-stack:
    autostart: true          # supervise this one; `corgi agent init` sets it
    kind: claude             # which agent CLI; default, see below
    configDir: ~/.claude-work
    wakeLock: session
    permissionMode: default
```

**`autostart` is opt-in.** A registered workspace is not supervised until it
says so, because `corgi agent scan` can register a dozen stacks and starting a
Claude session for each of them is a surprise measured in gigabytes.
`corgi agent init` sets it; `serve` says which workspaces it skipped and why.

The rule: **untrusted config may restrict, never relax.** A cloned repository
can mark itself `sensitive`, which only removes capability. It cannot choose
which binary runs, which account is used, or which permission mode applies —
otherwise cloning a repository would be a way to run code on your machine.

Those prompts are what you answer from your phone, and they are the main defence
against a session acting on injected instructions from a file it read. So corgi
does not skip them **unless you deliberately ask it to, per workspace, in your
trusted config**:

```bash
corgi agent init --dangerously-skip-permissions          # this workspace
corgi agent profile add work --config-dir ~/.claude-x \
  --dangerously-skip-permissions                         # or a reusable profile
```

That sets `dangerouslySkipPermissions: true`, and corgi then passes
`--permission-mode bypassPermissions` for that workspace. Two guarantees hold
even so:

- **A committed repo file can never turn it on.** The field lives only in the
  trusted user config; `.corgi/agent.yml` has no such field, so cloning a
  repository cannot make your machine skip prompts. A stray
  `permissionMode: bypassPermissions` string, or a `--dangerously` flag smuggled
  through `args:`, is still rejected — the boolean above is the one sanctioned
  route.
- **It is never silent.** `corgi agent up`/`serve` print
  `⚠ permissions: SKIPPED` for any workspace running this way, and
  `corgi agent profile list` marks the profile.

One caveat on the profile form: profiles are global, so a bypass **profile** can
be applied to any non-sensitive workspace from the phone, not only the one you
built it for. Prefer `agent init --dangerously-skip-permissions` to pin the bypass
to a single workspace; reach for a bypass profile only when you deliberately want
it reusable across several.

Leave it off (the default) for anything you would not let run unattended.

## The MCP tools

`corgi mcp` gains eleven tools. A Remote Control session calls them from your
phone; they also work from any other MCP client.

| tool | what it does |
|---|---|
| `corgi_session_brief` | what the previous session was working on before it restarted |
| `corgi_session_events` | the workspace timeline: starts, exits and why, session links |
| `corgi_workspaces` | every stack registered on this machine |
| `corgi_workspace_resolve` | "the recipe app" → one stack, or candidates |
| `corgi_worktrees_materialize` | a worktree per repo, all on one branch |
| `corgi_worktrees_release` | remove those worktrees, keep the branches |
| `corgi_diff` | every repo's change against its base, in one response |
| `corgi_preview_start` | open a public tunnel to a running service |
| `corgi_preview_state` | starting / ready / broken / stopped, with the reason |
| `corgi_preview_freeze` | pin it so idle reaping leaves it alone |
| `corgi_preview_stop` | tear it down |

`corgi_worktrees_*` mutate, so they join the same tunnel gate that already
covers `corgi_exec` and `corgi_db_query` — see [exposure
tiers](#exposure-local-private-public) for when that gate is closed.

### Resolution never guesses

```
$ corgi agent resolve "the recipe app"
acme-stack (/Users/you/dev/acme), api + web + db, matched on alias recipe app
```

An ambiguous name returns candidates and starts nothing, because a wrong
resolution means an agent editing the wrong repository. One extra tap is cheap;
that is not. Echo the resolved path back before doing any work.

### One branch across every repository

This is the thing Remote Control structurally cannot do. `--spawn=worktree`
gives it one worktree of *one* repository; a stack is several.

```jsonc
// corgi_worktrees_materialize { "branch": "feature/referral" }
{
  "branch": "feature/referral",
  "worktrees": [
    {"service": "api", "dir": ".../corgi_services/.worktrees/api@feature-referral", "created": true},
    {"service": "web", "dir": ".../corgi_services/.worktrees/web@feature-referral", "created": true}
  ]
}
```

Two services sharing a repository share one worktree, because git allows a
branch in exactly one. Re-running is idempotent and keeps uncommitted work.

### The diff is the artifact that works on a train

```jsonc
// corgi_diff { "branch": "feature/referral", "base": "main" }
{
  "base": "main", "additions": 4, "deletions": 0,
  "repos": [
    {"service": "api", "additions": 1, "files": [{"path": "README.md", "additions": 1}]},
    {"service": "web", "additions": 3, "files": [{"path": "Signup.tsx", "additions": 3, "new": true}]}
  ]
}
```

No tunnel, no running stack, survives bad signal. Newly created files are
included — `git diff` alone says nothing about an untracked file, and creating
files is the most common thing an agent does. `.gitignore` is respected, so an
ignored secrets file never reaches a transcript. Very large patches are
truncated rather than dropped.

## Live preview

A tunnel onto a service the agent is editing, so the change can be watched from
a phone. corgi needs no refresh mechanism — the dev server already hot reloads.
It needs to keep one tunnel open and be honest about the build state.

```
corgi_up                       # the stack must be running first
corgi_preview_start { "service": "web", "branch": "feature/referral" }
  → { "state": "starting" }    # returns immediately; no MCP handler may block
corgi_preview_state
  → { "state": "ready", "url": "https://kind-zebra-42.trycloudflare.com" }
```

The tunnel runs **detached**, writing to `corgi_services/.previews/<id>.log`,
which is the same shape corgi already uses for detached services. So a preview
outlives the session that started it, and a later corgi run can still find it.

**States, because a banner beats a white screen.** Mid-task a worktree is often
in a broken intermediate state — a half-written file, an import that does not
resolve. `broken` means the tunnel is up but nothing answers on the port, which
usually means a build in progress. Show that, rather than handing over a URL
that renders a stack trace.

**Freeze** pins a preview so idle reaping leaves it alone while someone is
reading it. **Idle reaping** tears down anything unwatched for 20 minutes by
default and actually kills the tunnel — a forgotten preview is a public URL onto
seeded data.

Tunnels are off unless asked for, and a workspace marked `sensitive` refuses one
outright, pointing at `corgi_diff` instead.

### What is not verified

Being straight about this, because it decides whether the feature is worth
using for your stack:

- **Hot reload over a tunnel is unproven here.** Whether a dev server's HMR
  websocket survives the round trip depends on the provider. If it does not,
  you get a page that loads but does not update, which is worse than knowing.
- **Vite and Next need the tunnel host allowed** (`allowedHosts` /
  `allowedDevOrigins`) or every preview is a blocked-host error. corgi does not
  inject that yet — add it to your dev server config.
- **A quick tunnel changes URL when it restarts**, which breaks the link already
  open on a phone. Declare a named tunnel in the service's `tunnel:` block in
  `corgi-compose.yml` for anything you want to keep open — there is no command
  line flag for it — and corgi reports `quickTunnel: true` so you know which
  kind you have.

Try it by hand before relying on it. `corgi_diff` needs none of this and is the
better answer to "what changed".

## Exposure: local, private, public

"Is there a tunnel" is the wrong question to gate on. A tunnel behind an
identity proxy is not open to the internet; a quick tunnel is open to anyone who
has the URL. Those deserve different answers, so `corgi mcp --http --tunnel`
sorts the endpoint into a tier:

| tier | what it means | `corgi_exec`, `corgi_db_query`, `corgi_worktrees_*` |
|---|---|---|
| `local` | loopback or LAN, no tunnel | allowed |
| `private` | a tunnel an identity proxy stands in front of | allowed |
| `public` | anyone holding the URL can reach it | blocked unless `CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1` |

`private` is **only ever reached by observing it**. When the tunnel URL is
published, corgi makes one unauthenticated request to **`/mcp`** — the route the
tools are actually served on — and looks at what comes back: a redirect to an
Access login, a `cf-access-*` header, a challenge naming a realm. Nothing in any
config file can assert protection, because a gate that relaxes on a claim is a
gate that fails open on a typo.

The route matters. Making a non-browser MCP client work behind Access usually
means giving `/mcp` a service-token or bypass policy while `/` keeps redirecting
to the login page. Probing the root would see that redirect, call the tunnel
private, and re-enable `corgi_exec` on a route anyone with the URL can reach.

```
🌐 ✓ public MCP endpoint: https://corgi.example/mcp
🌐 exposure: private — cloudflare-access (unauthenticated request redirected to the Access login).
   corgi_exec/corgi_db_query stay enabled; no CORGI_MCP_ALLOW_DANGEROUS_TUNNEL needed.
```

corgi's own bearer check answers 401 too, and is deliberately **not** counted:
treating it as protection would let the endpoint declare itself private on the
strength of the very token the gate exists to protect. Anything unrecognised —
including a probe that could not connect — stays `public`. The gate starts
closed and opens only on evidence.

The practical result is that `CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1`, which is set
once and then forgotten about forever, stops being the only way to use these
tools from a phone. Put a named tunnel behind an access policy and the gate
opens for the right reason.

Two things this does **not** do. It does not check previews: `corgi_preview_*`
opens a tunnel onto a dev server, and probing it would mean a network call
inside an MCP handler, which must never block. A preview is public, `sensitive`
workspaces refuse one, and idle reaping still tears it down. And it is a
reachability check, not an authorization model — it tells you an unauthenticated
request does not reach corgi, nothing about who is on the other side once it
does.

## Pairing a phone

A phone reaches corgi over the MCP HTTP endpoint, and should never be handed the
server's own bearer token — that token reaches `corgi_exec` and `corgi_db_query`.

```bash
corgi mcp --http 127.0.0.1:8765 --pair
```

prints a single-use code, valid ten minutes, which a client exchanges once for
its own revocable token. `corgi mcp devices revoke <name>` kills exactly one
device without disturbing the others — which is the whole reason not to share
one token. Full detail: [docs/mcp.md](mcp.md).

## When corgi cannot find its data directory

corgi keeps its registry beside its other state. On macOS that is the Homebrew
`var/corgi` directory when one already exists, otherwise
`~/Library/Application Support/corgi`. The location is decided by looking at the
filesystem, never by running `brew` — launchd's PATH does not include it, and
shelling out would give the daemon and your shell two different directories.

If you use a custom Homebrew prefix and `HOMEBREW_PREFIX` is not exported, point
corgi at the right place explicitly:

```bash
export CORGI_DATA_DIR="$(brew --prefix)/var/corgi"
```

## Platform support

| | supported |
|---|---|
| macOS | yes — launchd, `caffeinate` |
| Linux | yes — systemd user unit, `systemd-inhibit` |
| Windows | **not yet.** `corgi agent install` exits 2 and says so rather than half-installing. Run `corgi agent serve` under your own supervisor. |

## macOS keeps asking to let corgi read Documents

macOS puts `~/Desktop`, `~/Documents`, `~/Downloads` and iCloud Drive behind
TCC. The first time corgi reads one it has to ask — and the answer is filed
against that exact binary. corgi ships **ad-hoc signed**, with no Developer ID,
so every upgrade is a new identity as far as TCC is concerned and the dialog
comes back. Homebrew makes it worse: the real path is versioned
(`/opt/homebrew/Cellar/corgi/<version>/bin/corgi`), so it changes too.

```bash
codesign -dv "$(readlink -f "$(which corgi)")" 2>&1 | grep -E 'Signature|TeamIdentifier'
# Signature=adhoc / TeamIdentifier=not set  → the answer lasts until the next upgrade
```

The fix that needs nobody's certificate is to keep workspaces **outside** those
four folders — `~/dev`, `~/code`, `~/src` are not gated, and macOS never asks
about them at all:

```bash
mv ~/Documents/your-stack ~/dev/your-stack
corgi agent workspaces relocate your-stack ~/dev/your-stack
```

`corgi agent status` lists the registered workspaces macOS gates, so the dialog
has an explanation somewhere other than your memory. Until corgi is
Developer-ID signed and notarized, an upgrade will re-ask for any workspace left
in one of them.

## Security summary

- Config split by trust; a cloned repo cannot grant itself capability.
- `bin` must be a command name on PATH, never a path.
- Permission prompts are only skipped when your trusted config explicitly sets
  `dangerouslySkipPermissions` — never from a committed repo file, a
  `permissionMode` string, or a smuggled `--dangerously` arg; and never silently.
- Ambient credentials stripped from supervised processes and reported.
- The user config must be `0600` or corgi refuses to read it; briefs are written
  `0600` for the same reason — they name repository paths and branches.
- A custom kind's `args` cannot carry `--dangerously-*` or `--yolo`.
- Exposure is downgraded to `private` only on an observed interception, never on
  a config claim, and corgi's own 401 does not count as one.
- No secret material in the launchd plist or systemd unit — those are
  world-readable and land in backups.
- Supervised output is not mirrored to the daemon's log unless you pass
  `--foreground`; a session's output can contain env values and tokens.
- Mutating MCP tools are blocked over a public tunnel by default.
- **Prompt injection is a real exposure, not a solved problem.** A session reads
  repository files, issue text, and dependency READMEs, and corgi's tools can
  materialize branches. The permission prompts you answer from your phone are
  the defence, which is why weakening them is refused.

## A phone app

The Claude app already covers the conversation. What it has no concept of —
which stacks exist, whether the daemon is up, a cross-repo diff — is sketched in
[corgi-remote](remote-app.md). Nothing is built; the document mostly records
what *not* to build.

## Licensing

Supervising `claude remote-control` on your own machine, for your own work,
under your own login is ordinary individual use — Remote Control is a
first-party feature built for exactly that.

Hosting it for other people, or routing anyone else's requests through your
seat, is not. If agent mode ever grows a multi-user mode, that mode must require
API-key authentication and refuse to start on subscription credentials.
