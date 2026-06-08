---
description: Proactively run corgi suggest on a schedule and propose (or, if opted in, auto-file as a DRAFT) ONE tracker ticket for the top measurable win — deduped against open + dismissed ideas, rate-limited, never spammy. Pass a workspace path and optional mode; no args = current workspace, propose mode.
---

Run the corgi **proactive suggest** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = optional absolute workspace path + optional mode (`propose` default, or
  `auto-file-drafts` if the workspace has opted in). Empty → current workspace, propose mode.
- Designed to be invoked by `/schedule` (CronCreate) on a cadence; also runnable by hand.

Follow the `suggest-proactive` skill (`plugins/corgi/skills/suggest-proactive/SKILL.md`)
end to end: resolve workspace + mode + state (Phase 0), reuse `suggest` for the ranked
shortlist (Phase 1), dedupe + rate-limit the top idea against open tickets + the state
file + workspace memory (Phase 2), decide propose-vs-auto-file (Phase 3), then either ask
+ file through the `tracker` write gate or record-and-report (Phase 4), and offer to
arm/maintain the schedule (Phase 5).

Honor every guardrail: ONE ticket per run; per-week cap; default is propose-and-ask;
auto-file is draft-only (never assign / never build / never open a PR); never duplicate an
open or recently-dismissed idea; reuse `suggest` + `tracker` — never re-implement ranking.
