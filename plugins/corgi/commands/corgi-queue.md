---
description: Pick up build-ready tracker tickets and hand them to the stories skill to build as draft PRs. Several ways in — pass explicit Linear/Jira ticket links/keys to build exactly those, or pass a scope in plain words: nothing = the `agent` queue (tickets labelled `agent`), "in ready" = that status column, "from backlog" = the backlog, "most impactful" = highest-priority first. Any scope skips In Progress/Done/blocked and anything already merged, confirms the picks, then builds. Loop it to keep draining on a schedule (each batch still passes the one spec gate — not zero-touch).
---

Run the corgi **tracker** pickup flow (Job 4) for `$ARGUMENTS`.

- `$ARGUMENTS` = **explicit** ticket links/keys (e.g. `ABC-1 ABC-2`, pasted
  Linear/Jira URLs) → build exactly those. Otherwise a **scope** in plain words,
  resolved by Job 4: **empty** → the **`agent` queue**; **"in ready" / a column
  name** → that status; **"from backlog"** → the backlog; **"most impactful" / "most
  ROI"** → ordered by priority, top first; **"bugs"** → ready bug-type tickets
  (`stories` builds them bug-tier, red test first). All scopes filter to **not In
  Progress / not Done / not blocked**.
- A **question or singular** ask ("do we have bugs?", "what should I start with?") →
  present the set first and let the user pick; don't auto-build everything.
- Skip drift in every mode — anything whose PR already merged (or is in flight) is
  flagged, not rebuilt.
- Present the ready set, confirm the picks, then **auto-invoke the `stories` skill**
  on the picked keys (it owns the build: spec sign-off gate, branch per service,
  draft PRs).
- Read-only in this command — it dispatches; `stories` does the writing.
- Loop it to drain continuously: `/loop 1h /corgi-queue`.

Follow `skills/tracker/SKILL.md` (Job 4 — Pickup).
