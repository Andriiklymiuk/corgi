---
name: agent
description: "Use for corgi agent mode: Remote Control from a phone, the session board (\"who is waiting on me\"), watching trackers and PRs (\"tell me when someone comments\"), why a watch fired, and handoffs (\"leave a handoff for ABC-7\"). NOT compose authoring (corgi), starting a stack (run), debugging (debug)."
---

# Corgi agent mode

Read `../_shared/conventions.md` first.

**Done when:** the change asked for is live and shows in `corgi agent doctor` or the command's own status, or the question is answered from the board, not from memory.

Remote Control runs a Claude Code session on the user's own machine, driven from
their phone. It sees **one directory**. A corgi stack is **several
repositories** plus databases and env wiring.

This skill is how you work across that gap.

## The one rule that matters

**Never guess which stack the user means.** A wrong resolution means editing the
wrong repository, which is far worse than one extra question.

Always resolve first, and **echo the result back before doing any work**:

> Working on **acme-stack** (`~/dev/acme`) - api, web, db.

## Working on a stack

### 1. Find it

```
corgi_workspace_resolve { "query": "the recipe app" }
```

Returns either one workspace, or candidates with a reason. If it returns
candidates, **ask** - do not pick the first one.

`corgi_workspaces` lists everything registered, with `status`:

- `ok` - usable
- `unreachable` - the path did not resolve. Say *"that drive isn't mounted"* or
  *"that folder has moved"*, **not** "workspace not found". The row is kept on
  purpose.
- `disabled` - someone turned it off deliberately.

### 2. Give every repo the same branch

```
corgi_worktrees_materialize {
  "branch": "feature/referral-code",
  "services": "api,mobile"          // omit for every service
}
```

This is the step Remote Control cannot do for you. Each entry comes back with a
`dir` - **that is where you edit**, not the user's own checkout.

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
tunnel and no running stack, so it works on a bad connection - which is exactly
the situation someone reading it on a phone is in.

- Newly created files appear with `"new": true`. `.gitignore` is respected.
- `truncated: true` means the patch was capped, not that the file is small.
- Pass `includePatch: false` when you only need the shape of the change.

Summarise it in prose first - *"3 files across api and web, +47/-12"* - then
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

- `starting` - no URL yet. Say so; do not invent one.
- `ready` - hand over the URL.
- `broken` - the tunnel is up but nothing answers on the port, usually a build
  in progress. **Tell them that** rather than sending a link to a stack trace.
- `stopped` - gone; offer to start it again.

`corgi_preview_freeze` while they are reading it, so idle reaping does not pull
it away. `corgi_preview_stop` when they are done - a forgotten preview is a
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

- `corgi_context` - one call for topology, ports, health and every repo's branch;
  make it first in a workspace you have not looked at this session.
- `corgi_why { service }` - one verdict for a service that is down (dependency,
  port owner, exit code, env, log tail) instead of reading three snapshots.
- `corgi_wait_for_log { service, pattern }` - block until a log line matches;
  never poll `corgi_logs` on a timer.
- `corgi_checkpoint { name }` / `corgi_restore { name }` - mark every repo's branch,
  HEAD and uncommitted work before a cross-repo change, and put it all back.
- `corgi_checkout { branch }` - every repo onto one branch (or its own default),
  fast-forwarded; dirty repos are skipped, never clobbered.

## The rest, one file per topic

This file covers working on a stack. Everything else is in `references/`. Read the file for the topic before you answer or act: the commands and flags are there, not here. Read only the files the task needs.

| Read | When the task is |
|---|---|
| `references/setup.md` | Set up or repair agent mode: the daemon, supervised servers, pairing a phone, notifications on a phone (Telegram, push, the Claude app), `corgi agent doctor`. |
| `references/remote-start.md` | Start a session from a phone or another device, profiles, the custom connector, the troubleshooting table for remote start. |
| `references/board.md` | The session board: who is waiting, `corgi agent sessions`, `track`, `windows`, `pin`, `rescan`, hooks; the board in a terminal (`top`, `ask`, `transcript --why`, `dismiss`); two sessions in one repo (`claim`, `cap`). |
| `references/screen.md` | The Mac's screen and terminal from a phone or another Mac, wake, and agents that use the Mac (`corgi agent hands`, `corgi_mac_*`). |
| `references/acting.md` | Act on the board from a phone, Telegram, Slack, the VS Code extension or an MCP client: answer, carry, continue, pause a workspace, move, ignore or comment on a watched item, undo or replay a watch. |
| `references/watch.md` | Watch a tracker or PRs: enable, `notify` or `fix`, tokens (`watch auth`), webhooks, test and run, why a watch fired. |
| `references/handoffs.md` | Leave, show or verify a handoff (`corgi agent handoff`), the workpad, a blocked run, the kanban columns. |
| `references/policy.md` | Scope, drift, which model runs what, routines, hardening, own tickets and tasks, "work on it", history of done work, who reviews and as whom. |
| `references/unattended.md` | What a workspace does on its own: the per-workspace switches (`corgi_watch_switches`, `corgi_watch_set`), several tries at once, bots, reviews from anywhere, other agents on the board, a read-only phone, pairing without a QR, the stack from the phone; a session whose terminal is gone (`turn`), `undo`, `stream`, `pr-body`, the push `guard`, `pulse`, the daemon in a container. |
| `references/peers.md` | More than one laptop or a server: `corgi agent peers` (invite, join, lead, bootstrap), sending a task to a peer. |
| `references/codex.md` | Codex instead of Claude: what works, hooks, carry to codex. |

## Things not to do

- **Do not weaken permissions on your own.** A `permissionMode: bypassPermissions`
  string and a smuggled `--dangerously` arg are refused. The one sanctioned route
  is the user explicitly setting `--dangerously-skip-permissions` on `agent init`
  or a profile - their deliberate, trusted-config choice, never yours to make.
  Those prompts are what the user answers from their phone, the defence against a
  session acting on instructions injected through a file it read. If a permission
  prompt is in the way, ask - do not route around it.
- **Do not put capability settings in `.corgi/agent.yml`.** That file is
  committed and travels with a clone. `bin`, `configDir`, `permissionMode`, and
  credential inheritance belong in the user-level config only. corgi ignores
  them in the repo file by design.
- **Do not open a public tunnel for a workspace marked `sensitive`.** It is
  refused, and the refusal is the point.
- **Do not edit the user's own checkout** when a worktree exists. That is the
  one place they may have work in progress.

## Reference

Full documentation: [docs/agent.md](https://github.com/Andriiklymiuk/corgi/blob/main/docs/agent.md).
