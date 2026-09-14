---
description: Report where a batch of work stands — "give me a summary", "what's left", "status of all the MRs", "what did we ship", "recap". Re-reads every PR/MR and ticket live and groups them by ticket with clickable links. Pass nothing to summarise the session's own work.
---

Run the corgi **summary** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = optionally a tracker key, a repo, or a list of PR/MR links to scope the
  report. Empty → every PR/MR and ticket this session touched.

Follow the `summary` skill (`plugins/corgi/skills/summary/SKILL.md`): re-fetch the live
state of every PR/MR (state, head pipeline, reviewers, auto-merge, unresolved threads)
and every ticket, then group **by ticket, not by repo** — one block per ticket, a table
row per repo, the MR/PR as a markdown link.

Never report a state from what the session remembers doing; auto-merge fires and
pipelines turn red after the fact, and the one stale row is the one that matters. Never
wrap the report in a fenced code block — it kills the links; a table keeps alignment and
links together.

Lead with what still needs the user. Include what a review caught in the session's own
work, credited to whoever found it, and anything deliberately left undone with the
reason. Merged is not deployed — do not claim a release that was not observed.
