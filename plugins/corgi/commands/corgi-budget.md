---
description: How much budget do I have. Reads corgi agent usage --json and answers per account with the 5-hour and 7-day percent, when each window lifts, and whether the current pace is safe; suggests corgi agent carry when a limit is near. Pass an account profile to narrow; no args = every account.
---

Run the corgi **budget** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = an account profile name to narrow to. Empty → every account in
  `corgi agent usage --json`.
- Per account: room left and time left in the 5h and 7d windows, the pace projected to
  `resetsAt` (safe under 90%, tight to 100%, over past it), and which window binds.
- Stale `fetchedAt` (over an hour) → say so and ask for one `/usage` under that
  account before trusting the numbers.
- One verdict line. At 90% or more: name the account with room and print
  `corgi agent carry <session> --profile <name>` for the session about to hit the wall;
  no other account → say when the limit lifts and suggest queueing the work.
- Read-only: never carry, stop, or dismiss a session yourself.

Follow `skills/budget/SKILL.md`.
