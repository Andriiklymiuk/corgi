---
name: align
description: Use when the user says "align", "align on this", "let's align first", "make sure we agree", or hands over a request that can be read two ways before any code is written. Reaches a shared understanding at the lowest token cost, then codes. NOT a plan gate (stories Phase 2) and NOT a long interview.
---

# Align: agree once, then ship

Read `../_shared/conventions.md` first.

A wrong reading of the request is the most expensive token spend there is: everything
built on it is thrown away. A question that grep could have answered is the second most
expensive. Align spends the minimum to rule out the first without paying the second.

## Step 0 - look before asking

Read the request, then the code it points at (`grep`, `rg`, `git log -5`, the compose
file). Facts are your job, never the user's: file names, current behaviour, which service
owns a route, what a flag does today. Main session only; no subagents.

Then answer one question: **does any other reading of the request change what you
would build?** Not the wording, not the style - the work.

- **No** → one line, then code:

  ```
  Aligned: <what you will do, one sentence>. Going.
  ```

  No questions, no bullets, no recap. Most requests end here.

- **Yes** → one round, format below. Then code.

## The round - forks only, picks always

List only the forks whose answer changes the work. At most 5. Number them, give the
body in one or two lines, and give your pick. Grill style, not grill length:

```
Reading: <≤5 bullets - what you understood, what you found in the code>

❓ **Q1** - **<fork>**: <two or three options, one line each>
➡️ <your pick, half a sentence why>

---

❓ **Q2** - ...
```

Whole frontier in one round; a fork that depends on an open fork waits. The user answers
in letters or words, or says "go": unanswered forks take your pick. Apply the answers and
code - do not echo them back, do not ask a second round unless an answer opened a fork
you could not have seen.

Never ask about: naming, style, formatting, a detail one grep settles, or a choice where
your pick is the obvious default. Fold those into the pick.

## Mid-flight forks

A real fork that appears while coding gets one line, two options, your pick - then keep
going on the pick unless told otherwise. No status recap around it.

```
Fork: <what> - a) <x> b) <y>. Taking a.
```

## Diagrams

Off. Draw one only when the user asks ("diagram", "show me", "draw it") or the command
carries `--diagram`. Never as a courtesy.

## What survives

Picks nobody overruled are assumptions. The `prep-pr` skill puts them in the PR body under
`## Assumptions` (≤5 bullets, skipped when empty) so the reviewer sees what the agent
decided alone. Nothing else is written down.

## Unattended

A `corgi watch ·` run (see conventions) never asks: every fork takes the pick, named in
one line, and the run goes on.

## Red flags

- A question the codebase could answer → grep, don't ask.
- More than 5 forks → you have not read enough yet, or the request is a story
  (`stories` skill), not an align.
- Explaining your reasoning before the forks → cut it; the pick carries it.
- "Aligned" line longer than one sentence → not aligned yet.
- Second round without a new fork → stop, code.
