---
description: Where was I. Reads corgi agent brief, corgi context, workspace memory, the session board and per-repo git state, then prints a six-line orientation and the next command. Pass a workspace name to orient in one that is not the cwd; no args = the current stack.
---

Run the corgi **resume** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a workspace name or path (resolved with `corgi agent resolve
  <name>`). Empty → the stack in cwd; no compose here → the newest entry from
  `corgi agent brief`.
- Read-only: `corgi agent brief --json`, `corgi context --json`,
  `.corgi/memory/index.md`, `corgi agent sessions`, then `git status` and the
  unpushed log only for repos that are dirty or ahead.
- Print the six lines (workspace, last session, branches, stack, open threads,
  next) and one suggested command with a half-sentence reason.
- Never start, stop, or check anything out while orienting.

Follow `skills/resume/SKILL.md`.
