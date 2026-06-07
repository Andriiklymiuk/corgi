---
description: Tracker-side planning for a corgi workspace (Linear or Jira) — status/standup, triage, or decompose an epic into tickets, with each ticket tied to its real PR/CI state across services. Pass what you want in plain words (e.g. "standup", "what's blocked", "triage the inbox", "break EPIC-9 into tickets"); no args = a status digest of the active cycle/board.
---

Run the corgi **tracker** flow for the request in `$ARGUMENTS`.

- `$ARGUMENTS` = plain words: a status ask ("standup", "where are we", "what's
  blocked"), a triage ask ("triage the inbox", "label these", "dupes"), or a
  decompose ask ("break <EPIC> into tickets", "turn this feature into stories").
  Empty → a status digest of the active cycle/board.
- Reads Linear or Jira **and** `corgi-compose.yml`, correlates each ticket with its
  real branch/PR/CI state across services, and surfaces drift.
- Read-only until a single confirm gate guards any tracker write. Hands tickets to
  `stories` to build.

Follow `skills/tracker/SKILL.md`.
