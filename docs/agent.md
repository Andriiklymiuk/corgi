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
   offline"*, and *"if your machine is awake but unable to reach the network for
   more than roughly 10 minutes, the session times out and the process exits"*.
   So you have to remember to arm it — and forgetting is the failure this exists
   to remove.
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
        ├─► which stack is "the todo app"?
        ├─► a worktree per repo, one branch
        └─► one diff across every repo
```

## Quick start

```bash
cd ~/dev/your-stack
corgi agent init                 # register AND enable this stack
corgi agent install              # start at login (launchd / systemd)
corgi agent status               # what is running, and under which account
```

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
| `corgi agent init` | register this stack, write `.corgi/agent.yml` |
| `corgi agent scan <dir>` | find stacks under a directory and register them (does not enable) |
| `corgi agent serve` | supervise Remote Control for every enabled workspace |
| `corgi agent install` / `uninstall` | start (or stop starting) at login |
| `corgi agent status [--json]` | what is running, restarts, which account |
| `corgi agent doctor [--json]` | can this work here, and what to fix |
| `corgi agent workspaces` | list, `forget`, `relocate` |
| `corgi agent resolve <name>` | what "the todo app" resolves to |
| `corgi agent stop` | stop the daemon |

## Restarts, and being told about them

Remote Control exits on the ten-minute network timeout, on a crash, and on
reboot. corgi restarts it with capped backoff and **says so**:

> `rc restarted 14:32 · previous session ended (network timeout) · worktrees kept`

The notification matters. A relaunched Remote Control starts a **new** session,
so the previous conversation's context is gone. Restarting silently would look
like continuity and cost you an hour of confusion.

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

**Honest limit:** on macOS, closing the lid on battery sleeps the machine no
matter what `caffeinate` does. "Lid closed on the train" only works plugged in.
If that is your workflow, supervise from a machine that stays on.

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
  aliases: [acme, todo app]
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
    configDir: ~/.claude-work
    wakeLock: session
    permissionMode: default
```

The rule: **untrusted config may restrict, never relax.** A cloned repository
can mark itself `sensitive`, which only removes capability. It cannot choose
which binary runs, which account is used, or which permission mode applies —
otherwise cloning a repository would be a way to run code on your machine.

`permissionMode: bypassPermissions` is rejected outright, and corgi never
passes `--dangerously-skip-permissions` whatever your aliases do. Those prompts
are what you answer from your phone, and they are the main defence against a
session acting on injected instructions from a file it read.

## The MCP tools

`corgi mcp` gains nine tools. A Remote Control session calls them from your
phone; they also work from any other MCP client.

| tool | what it does |
|---|---|
| `corgi_workspaces` | every stack registered on this machine |
| `corgi_workspace_resolve` | "the todo app" → one stack, or candidates |
| `corgi_worktrees_materialize` | a worktree per repo, all on one branch |
| `corgi_worktrees_release` | remove those worktrees, keep the branches |
| `corgi_diff` | every repo's change against its base, in one response |
| `corgi_preview_start` | open a public tunnel to a running service |
| `corgi_preview_state` | starting / ready / broken / stopped, with the reason |
| `corgi_preview_freeze` | pin it so idle reaping leaves it alone |
| `corgi_preview_stop` | tear it down |

`corgi_worktrees_*` mutate, so they join the same public-tunnel gate that
already covers `corgi_exec` and `corgi_db_query`: over a tunnel they need
`CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1`.

### Resolution never guesses

```
$ corgi agent resolve "the todo app"
acme-stack (/Users/you/dev/acme), api + web + db, matched on alias todo app
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

## Platform support

| | supported |
|---|---|
| macOS | yes — launchd, `caffeinate` |
| Linux | yes — systemd user unit, `systemd-inhibit` |
| Windows | **not yet.** `corgi agent install` exits 2 and says so rather than half-installing. Run `corgi agent serve` under your own supervisor. |

## Security summary

- Config split by trust; a cloned repo cannot grant itself capability.
- `bin` must be a command name on PATH, never a path.
- `bypassPermissions` rejected; `--dangerously-skip-permissions` never passed.
- Ambient credentials stripped from supervised processes and reported.
- The user config must be `0600` or corgi refuses to read it.
- No secret material in the launchd plist or systemd unit — those are
  world-readable and land in backups.
- Supervised output is not mirrored to the daemon's log unless you pass
  `--foreground`; a session's output can contain env values and tokens.
- Mutating MCP tools are blocked over a public tunnel by default.
- **Prompt injection is a real exposure, not a solved problem.** A session reads
  repository files, issue text, and dependency READMEs, and corgi's tools can
  materialize branches. The permission prompts you answer from your phone are
  the defence, which is why weakening them is refused.

## Licensing

Supervising `claude remote-control` on your own machine, for your own work,
under your own login is ordinary individual use — Remote Control is a
first-party feature built for exactly that.

Hosting it for other people, or routing anyone else's requests through your
seat, is not. If agent mode ever grows a multi-user mode, that mode must require
API-key authentication and refuse to start on subscription credentials.
