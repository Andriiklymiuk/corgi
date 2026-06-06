---
description: Diagnose a misbehaving corgi stack, or gather runtime data while investigating a bug. Local-first (corgi ps/status/doctor/logs + targeted retries), escalating to whatever logs/analytics provider the repo uses (auto-detected from the README) only on demand and after asking. Pass a symptom or a data request (e.g. "api won't start", "everything hangs on boot", "pull the staging logs for this 500"); no args = snapshot the whole stack.
---

Run the corgi **debug** flow for the request in `$ARGUMENTS`.

- `$ARGUMENTS` = a stack symptom ("won't start", "crashed", "stuck", "slow") **or** a
  data request while investigating a ticket ("pull the logs/traces for X"). Empty →
  snapshot the whole stack and triage.
- Run **inside the stack folder** (the one with `corgi-compose.yml`).

Follow the `debug` skill (`plugins/corgi/skills/debug/SKILL.md`) end to end — pick
the entry mode (broken stack → Steps 0–3 local; bug needing data → often jump to
Step 4), and stop as soon as the cause is explained. The skill owns the
classification, the retry recipes, and the provider list.

Honor every guardrail: never foreground a `corgi run`, bound every log/file read,
local before external, analytics only on demand + after the ask-gate (provider read
from the repo — corgi is provider-agnostic), read-only and scoped on any external
query, never echo secret values, ~2 honest tries per service then report.
