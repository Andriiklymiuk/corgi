# Setting agent mode up

Only when the user asks for it, or when they say a remote session keeps dying.

```bash
cd <the stack>
corgi agent init                 # register AND enable this stack
corgi agent up --at-login        # daemon + endpoint + tunnel, back after a reboot
corgi agent doctor               # what is missing, and how to fix it
corgi agent status               # what is running, under which account
```

`corgi agent doctor` output is already actionable - relay it rather than
re-diagnosing.

`corgi agent scan` registers what it finds but deliberately enables nothing -
`autostart` is opt-in, and `corgi agent init` is what sets it. Tell the user to
run `init` in the stacks they actually want supervised, rather than assuming a
scan armed them. `corgi agent serve` names every workspace it skipped and why.

### Supervised servers are devices, not conversations

A server the daemon starts on its own opens **no session**: it registers with
claude.ai as a device (`claude remote-control --no-create-session-in-dir`) and waits.
`corgi agent status` calls that state `online`; the launcher card says *online · no
session* with a solid green dot and a **Start** button. That is the resting state,
not a start that failed - do not restart anything because of it.

A session exists once someone asks for one: the Claude app's device list (new
session on that machine), the launcher's Start, or `corgi_session_start`. The last
two swap the device server for one that opens a session and returns its link -
expect a few seconds' gap and a `starting` card in between. Those sessions are
named `<workspace>-brave-otter` (corgi sets the name prefix), so the list still
says which repo each is in. **Stop** on a session in an autostart workspace ends
the session and puts the device back.

Why: remote control pre-creates a session on every start and leaves it in the
claude.ai list when it stops, so four supervised workspaces restarted at login, on
the network timeout and on `corgi agent restart` used to fill the phone's list with
`<ws> · main · 10:00` rows nobody opened. Two things worth telling a user who asks
about that:

- rows an older corgi left behind are ordinary offline sessions - archive them once
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
setting, every supervised session runs under the *default* account - correct
looking output, wrong account, wrong bill.

```bash
corgi agent init --config-dir ~/.claude-work
```

`corgi agent status` prints the account each workspace will actually use. If the
user has work and personal logins, check it.

### Getting notifications on a phone

Raise this whenever the user enables hooks, or says a notification only reached
the laptop. **Without `notifyUrl` every notification stops at the machine** -
which is the desk they were trying to leave.

**Do not suggest ntfy.sh to an iPhone user.** Its iOS app is paid; Android is
free. corgi picks the payload from the host, so free destinations exist:

| `notifyUrl` host | payload | free on iOS |
|---|---|---|
| `api.telegram.org` | `{"text", "chat_id"}` | yes |
| `discord.com` | `{"content"}` | yes |
| `hooks.slack.com` | `{"text"}` | yes |
| anything else | ntfy shape (body + `Title`/`Click` headers) | only self-hosted |

**Telegram is set up by one command - do not walk them through getUpdates.**

```bash
corgi agent notify telegram --token <TOKEN>
corgi agent restart
```

It validates the token, waits while they message the bot, resolves the chat id,
writes `notifyUrl` and sends a test. Their part is only: message **@BotFather**,
`/newbot`, copy the token, then message the new bot when the command asks.

**Never ask for the token in chat and never echo one back.** It is a credential;
it belongs in that command's flag and nowhere else. If one has been pasted
somewhere, tell them to `/revoke` in @BotFather - the chat id survives.

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
is installed ([how](https://github.com/julienXX/terminal-notifier#download)) - it brings the session's window
forward (VS Code, iTerm, Terminal or tmux, as `corgi agent focus` does); a session
with no window on this Mac opens its session URL, or the launcher when corgi has
none. Without it macOS falls back to `osascript`, which cannot carry a click target.

### The launcher page

Everything the phone can do without the Claude app: start and stop a session,
pick a profile and name it, read the timeline, revoke a paired device, run
doctor. With session tracking on, the top of the page is the session board -
"2 waiting on you", each with what it is waiting for - so the phone answers
the question it was unlocked for before any card is tapped. Worth telling a user
unprompted:

- Cards carry a **hide** chip. Hidden cards collapse into one button, on that
  browser only - nothing on the machine changes. It is for showing the screen
  to someone, not for disabling a workspace (`autostart: false` does that).
- The header names the machine, the corgi version and whether the daemon is
  up. If it says an update is available, `corgi upd && corgi agent restart`.
- Cards show tokens today / this week, summed from Claude Code's own
  transcripts (`corgi agent status` prints the same). Cache reads are included,
  so the numbers are large by design - do not report them as an anomaly.

### Choosing how the phone reaches the machine

Ask where the phone will be, then pick the row - do not default to a tunnel:

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
must be proxied by Cloudflare - a plain CNAME at the registrar does not work.

The Wi-Fi row has no tunnel, no DNS and no provider, so it is the one to
suggest first when the phone is in the same building. The launcher is
token-protected either way - serving it on the local network is not serving it
to the internet. The middle two rows hold a fixed origin, which is what keeps
the phone paired across restarts. ngrok's free plan cannot choose a name;
every account already has one static `*.ngrok-free.dev` dev domain, and that is
the one to use.

### If the phone cannot open the launcher

Check these before suspecting corgi:

| symptom | cause | say |
|---|---|---|
| requests stall ~15s, Safari says "server stopped responding" | the Mac sleeps on battery; the wake lock is AC-only | plug in, or `sudo pmset -b sleep 0` |
| works on Wi-Fi, dead on cellular | that network's DNS refuses the tunnel domain - seen with `*.trycloudflare.com` and `*.loca.lt`, but it is per-carrier and most pass them fine, so do not present it as a known defect | try `--provider ngrok` with their `*.ngrok-free.dev` dev domain (free, and has resolved where those two did not); a host they own if that is refused too |
| any tunnel flakiness, phone at home | no need for a tunnel | `corgi agent up --http 0.0.0.0:8765`, then `http://<lan-ip>:8765/app` |

`corgi agent status` and `doctor` report the sleep risk directly; relay it
rather than re-deriving.

When it is unclear whether corgi or the tunnel is at fault, run both of these on
the machine before changing anything:

```bash
curl -so /dev/null -w '%{http_code} %{time_total}s\n' http://127.0.0.1:8765/app
curl -so /dev/null -w '%{http_code} %{time_total}s\n' https://<public-host>/app
```

Loopback slow or failing is corgi - `corgi agent status`, then restart. Loopback
fast and public failing is the tunnel - switch provider or move to the Wi-Fi
path. Both fast means the machine is healthy and the phone's network is the
problem; say so instead of restarting things. That second request shares this
machine's DNS and route, so a `200` does not prove a phone on cellular can
resolve the name - have them confirm on the phone with Wi-Fi off. Never stand a
public web proxy in for the phone: ngrok and Cloudflare throttle them, so its
`522` is about the proxy and reading it as a corgi fault sends the whole
diagnosis the wrong way.

### If a session died

`corgi agent status` shows `lastReason`, and `corgi agent logs <workspace>`
(or the `corgi_session_events` tool) shows the whole timeline - every start,
exit with its classified cause, and captured session link, newest first. Use
the timeline when the current reason is not enough.

A restarted session starts with none of the earlier conversation. Call
`corgi_session_brief { workspace }` first when the user picks up where they left
off: it reports the branch each repository was on, which held uncommitted
changes, and which cross-repo worktrees exist. `null` means nothing restarted,
which is the ordinary case. `exitOutput`, when present, is the last lines the
process printed before it died - read it before guessing at a crash. The common
reasons:

| reason | what to say |
|---|---|
| network timeout | Remote Control exits after ~10 min awake with no network. corgi restarted it. **The previous conversation's context is gone** - the new session starts clean. |
| unexpected exit (code N after Xh) | the process died; the reason carries its exit code, uptime and last output line, `exitOutput` the tail. A restart is already running - say what it printed, do not restart it again. |
| auth failure | corgi deliberately did not retry; retrying cannot produce credentials. Run `corgi agent doctor`. |
| exited immediately, repeatedly | corgi stopped after 5 attempts and disabled the workspace. Something is wrong with the setup, not the network. |
