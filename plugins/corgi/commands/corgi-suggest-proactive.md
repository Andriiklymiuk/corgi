---
description: The Proactive bot — picks ONE thing worth building next in a corgi workspace and puts it on the board as a task with its evidence, deduped against the board, the suggest history and workspace memory, at most one per week. Meant to run as a daemon routine (corgi agent routine add suggest --bot proactive); also runnable by hand. Never writes into the repo, never files on the tracker unless opted in, never builds. Pass an absolute workspace path; no args = the current workspace.
---

Run the corgi **proactive suggest** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = an optional absolute workspace path. Empty → the current workspace.
- Runs unattended from a daemon routine (`corgi agent routine add suggest --bot
  proactive`) or by hand; unattended never asks.

Follow the `suggest-proactive` skill (`plugins/corgi/skills/suggest-proactive/SKILL.md`)
end to end: resolve the workspace, the mode and the state and print the mode line
(Phase 0), rank with `suggest` in one pass, magic first (Phase 1), dedupe the cards top
down against the history, the board, open pull requests and memory (Phase 2), put the
survivor on the board with `corgi agent task add` and record it (Phase 3), and report
with the headline on the first line (Phase 4).

Honour every guardrail: one idea per run under the weekly cap; the output is a task, not
a file in the repo; a tracker ticket only when the workspace opted in, and then
draft-only; never a duplicate of an open or recently proposed idea; reuse `suggest` for
the ranking and `tracker` for the write gate.
