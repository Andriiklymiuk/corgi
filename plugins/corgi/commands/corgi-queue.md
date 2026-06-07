---
description: Pick up build-ready tracker tickets and hand them to the stories skill to build as draft PRs. Two ways in — pass explicit Linear/Jira ticket links/keys to build exactly those, or pass nothing to auto-pick the `agent` queue (tickets labelled `agent` that are not In Progress/Done). Either way it skips anything already merged, confirms the picks, then builds. Loop it to drain the queue unattended.
---

Run the corgi **tracker** pickup flow (Job 4) for `$ARGUMENTS`.

- `$ARGUMENTS` = **explicit** ticket links/keys (e.g. `ABC-1 ABC-2`, pasted
  Linear/Jira URLs) → build exactly those. **Empty** → auto-pick the **agent
  queue**: tickets labelled `agent` that are **not In Progress / not Done** and not
  blocked.
- Skip drift either way — anything whose PR already merged (or is in flight) is
  flagged, not rebuilt.
- Present the ready set, confirm the picks, then **auto-invoke the `stories` skill**
  on the picked keys (it owns the build: spec sign-off gate, branch per service,
  draft PRs).
- Read-only in this command — it dispatches; `stories` does the writing.
- Loop it to drain continuously: `/loop 1h /corgi-queue`.

Follow `skills/tracker/SKILL.md` (Job 4 — Pickup).
