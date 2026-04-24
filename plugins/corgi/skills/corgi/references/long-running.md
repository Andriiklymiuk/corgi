---
name: long-running
description: How to invoke `corgi run` from inside a Claude Code agent loop without hanging the shell. Read before ever running `corgi run` in bash.
---

# Running `corgi run` safely from an agent

`corgi run` blocks indefinitely, streams logs, watches the compose file, and responds to SIGINT/SIGTERM/SIGHUP. There is no `--detach` flag. Running it synchronously in a Bash tool call will make the agent hang until the 10-minute timeout hits.

## The two correct patterns

### Pattern A — background the process, probe with `status`

```
Bash(command: "corgi run", run_in_background: true)
# returns a shell ID; let it boot
Bash(command: "corgi status")   # synchronous; tells you which ports are up
```

Follow-up:
- If `corgi status` exits 1, either wait another few seconds (services still booting) or read the background shell's output to see the error.
- When done, kill the background shell with `KillShell` on that shell ID. Do **not** leave it running across sessions.

Gotcha: the background shell survives only while the Claude Code session is alive. Don't use this pattern if the user needs corgi running after you're gone.

### Pattern B — hand off to the user's terminal

If the user wants corgi running long-term, tell them:

> Run `corgi run` in a separate terminal. When it's up I'll run `corgi status` to check health.

Then you use the synchronous commands (`doctor`, `status`, `clean`, `db`, `pull`, `script`) from inside the agent.

This is the right pattern when:
- The user is doing active development and needs corgi to outlive this chat.
- Services use `interactiveInput: true` (need real TTY for prompts).
- You'd otherwise tie up the background shell for the whole session.

## `--runOnce` / `-o`

There's a `-o, --runOnce` flag that runs once and exits rather than looping. Useful for CI or scripted checks, but **the services themselves still run their `start:` commands** — so if those are themselves long-running (e.g. `npm run dev`), `--runOnce` doesn't help you. Only use it when every service has a terminating `start:` (e.g. a script that processes a batch and exits).

## What to avoid

- **Never** `corgi run` synchronously in a Bash call. Even with a timeout, you'll lose the ability to see boot output because it gets truncated.
- **Never** pipe `corgi run` into something like `head -n 50`. Corgi will exit prematurely when the pipe closes and your services will die.
- **Never** leave a background `corgi run` shell orphaned across sessions without telling the user — they'll have a silent Go process binding ports.

## Clean shutdown

- SIGINT / Ctrl-C is handled gracefully: corgi kills child processes, runs `afterStart:` commands, exits 0.
- From the agent, use `KillShell` on the background shell ID. It sends SIGINT → SIGTERM.
- If a service hangs on shutdown, user may need to `docker ps` + `docker kill` the db container manually. Warn them.

## TL;DR decision tree

- Need to verify "does this project boot?" → background `corgi run`, `corgi status`, KillShell.
- User wants to keep working in the project → hand off to a separate terminal.
- CI-style "run tests then exit" → `corgi run -o` only if all `start:` commands terminate on their own.
