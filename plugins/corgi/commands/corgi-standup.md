---
description: What did I do today. Runs corgi agent standup (prompts as headlines, commits as facts, per workspace), reads it back as done / doing / next / blocked, and offers to post it. Pass a window in plain words ("yesterday", "the weekend", "this week") or "write" for prose; no args = the last 24 hours.
---

Run the corgi **standup** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a window ("yesterday", "since Friday", "this week" → `--since 48h`,
  `72h`, `168h`) and/or the word `write` → `corgi agent standup --write` (Claude turns
  the list into a few sentences via `claude -p`). Empty → `corgi agent standup`.
- Read it back grouped by workspace: done, doing, next, blocked. Three to six lines per
  workspace; only the cwd workspace when the answer is for a team standup.
- Cross-check the tracker only for ticket keys that appear in the commits.
- Ask once before posting anywhere; no attribution, no inflating the facts.

Follow `skills/standup/SKILL.md`.
