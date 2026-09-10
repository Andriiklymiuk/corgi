---
name: resume
description: Use when the user asks "where was I", "what was I doing", "pick up where I left off", or a session restarted. Prints a six-line orientation and the next command. NOT for "what did I do today" (standup).
---

# Resume: where was I

Read `../_shared/conventions.md` first.

A restarted session has no memory of the last one. Everything that survives is on disk;
read it, don't ask the user to retell it.

## Read, in this order (all read-only)

1. `corgi agent brief --json`: what the last supervised session in this workspace was
   doing when it ended (`cause`, `reason`, per-repo `branch`, uncommitted work, leftover
   worktrees). No entry → say so and continue.
2. `corgi context --json`: every service and db with port and status, each repo's
   branch, dirty files, ahead/behind, the env tier, validation findings. No compose in
   cwd → take `dir` from `brief` and `cd` there.
3. `.corgi/memory/index.md` when it exists (or `corgi memory list --json`): open only
   the facts whose name matches a branch or service you just saw.
4. `corgi agent sessions`: other Claude sessions on this machine, which ones wait on a
   person, which account they run under.
5. `git -C <dir> status --short` and `git -C <dir> log --oneline -5 <base>..HEAD` for
   each repo `context` marks dirty or ahead. Skip clean repos.

Do not start, stop, or check out anything while orienting.

## Print six lines

```
workspace     <name> · <dir> · tier <tier>
last session  <ended when> · <cause> · <reason, one clause>
branches      <svc>=<branch>[*dirty][+n ahead] ...
stack         <n> up / <n> down / <n> unhealthy (<names>)
open threads  <sessions waiting on you> · <memory facts that apply>
next          <one command>
```

Keep each line under 100 characters; drop detail from a line before adding a seventh.

## Suggest the next command

Pick one, first match wins:

- dirty work on a story branch → `corgi test --changed --base <base>`, then the
  `prep-pr` skill.
- branch pushed, PR open → `/corgi-review <link>`, or watch CI.
- stack unhealthy → `/corgi-debug`.
- everything clean, stack down → `/corgi-run`.
- nothing in flight → `/corgi-queue` to pick a ticket.

Say why in half a sentence. If `brief` says the session ended on a usage limit, put
`corgi agent usage` (the `budget` skill) before anything else.
