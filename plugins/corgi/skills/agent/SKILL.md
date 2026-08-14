---
name: agent
description: Use when working on a corgi stack from a phone or another device through Claude Code Remote Control, or when setting that up — "work on the todo app", "which stacks do I have", "start a branch across the api and mobile repos", "show me the diff", "keep corgi running when I'm away", "why did my remote session die", "set up agent mode", "run it under my work account". Covers resolving a stack by name, materializing one branch across every repository in it, reading a cross-repo diff, and supervising `claude remote-control` so it survives reboots and the ten-minute network timeout. NOT for authoring corgi-compose.yml (corgi skill), starting a stack (run skill), or diagnosing a broken stack (debug skill).
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
corgi_workspace_resolve { "query": "the todo app" }
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

## Running the stack

The existing tools still apply, pointed at the worktree directories:
`corgi_up`, `corgi_status`, `corgi_logs`, `corgi_test`, `corgi_down`. See the
`run` and `debug` skills.

## Setting agent mode up

Only when the user asks for it, or when they say a remote session keeps dying.

```bash
cd <the stack>
corgi agent init                 # register; writes .corgi/agent.yml
corgi agent install              # start at login
corgi agent doctor               # what is missing, and how to fix it
corgi agent status               # what is running, under which account
```

`corgi agent doctor` output is already actionable — relay it rather than
re-diagnosing.

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

### If a session died

`corgi agent status` shows `lastReason`. The common ones:

| reason | what to say |
|---|---|
| network timeout | Remote Control exits after ~10 min awake with no network. corgi restarted it. **The previous conversation's context is gone** — the new session starts clean. |
| auth failure | corgi deliberately did not retry; retrying cannot produce credentials. Run `corgi agent doctor`. |
| exited immediately, repeatedly | corgi stopped after 5 attempts and disabled the workspace. Something is wrong with the setup, not the network. |

## Things not to do

- **Do not weaken permissions.** `permissionMode: bypassPermissions` is refused,
  and `--dangerously-skip-permissions` is never passed. Those prompts are what
  the user answers from their phone, and they are the defence against a session
  acting on instructions injected through a file it read. If a permission prompt
  is in the way, ask — do not route around it.
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
