---
description: Start a corgi-compose stack (or a slice of it) from chat — detached, then wait until healthy with a timeout and flag anything stuck. Pass what to run in plain words (e.g. "whole stack with tunnel and logs", "web + mobile on the remote backend", "just the api", "for the android emulator"); no args = whole stack.
---

Run the corgi **run** flow for the request in `$ARGUMENTS`.

- `$ARGUMENTS` = plain-words description of what to start (whole stack, a subset,
  with tunnel/logs, frontends against a remote backend, a host/emulator target).
  Empty → whole stack.
- Must run **inside the stack folder** (the one with `corgi-compose.yml`). Not
  there → tell the user to open it first.

Follow the `run` skill (`plugins/corgi/skills/run/SKILL.md`) end to end: locate the
stack (Phase 0), resolve the request to corgi flags from `corgi-compose.yml` +
`Makefile` + `README` (Phase 1), launch **detached** with `--json` (Phase 2), gate
on `corgi status --ready --json --timeout` and quick-triage anything stuck
(Phase 3), then report URLs + how to watch logs and stop (Phase 4).

Honor every guardrail: detached only (never a synchronous foreground `corgi run`),
never start a service the user wants pointed at a remote backend, `corgi ps` before
any `--force`/`restart`, and hand a genuinely broken boot to the `debug` skill.
