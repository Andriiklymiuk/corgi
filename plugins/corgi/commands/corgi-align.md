---
description: Agree on a request once, cheaply, then build it. Looks up facts itself, prints one "Aligned: … Going." line when the request has one reading, else one round of at most five forks with a recommended pick each. --diagram adds a flow diagram; off by default.
---

Run the corgi **align** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = the request (free text, a ticket key, or a link). `--diagram` at the
  end turns the diagram on.
- Grep first. One reading → `Aligned: <sentence>. Going.` and start.
- Two readings → `Reading:` (≤5 bullets) + numbered forks (≤5) with picks. Wait for
  answers or "go"; unanswered forks take the pick. Then start, no echo.
- Mid-flight fork → one line, two options, pick, continue.

Follow `skills/align/SKILL.md`.
