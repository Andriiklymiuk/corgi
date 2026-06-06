---
description: Suggest real, measurable improvements for a corgi workspace — scans the stack + its business domain + existing features and proposes ranked, evidence-backed ideas across two lenses (product/business and engineering: performance, reliability, security, cost, tech-debt, even a language rewrite when ROI is high), each tied to a measurable outcome. Specs the chosen one and offers to create a tracker story (asks where). Pass a focus in plain words (e.g. "performance ideas", "new business cases", "what's missing"); no args = scan everything, both lenses.
---

Run the corgi **suggest** flow for the focus in `$ARGUMENTS`.

- `$ARGUMENTS` = an optional focus (a lens like "performance"/"security", a service
  name, or a goal like "retention"). Empty → scan the whole workspace, both lenses.
- Run **inside the workspace folder** (the one with `corgi-compose.yml`).

Follow the `suggest` skill (`plugins/corgi/skills/suggest/SKILL.md`) end to end: map
the stack + business (Phase 0), gather **cited** evidence per lens once (Phase 1),
turn signals into measurable suggestion cards and cut the slop (Phase 2), present a
ranked product+engineering shortlist (Phase 3), spec the chosen one (Phase 4), then
offer a story and ask **where** to create it — handing implementation to the
`stories` skill if the user wants it (Phase 5).

Honor every guardrail: evidence + a measurable outcome on every suggestion or it's
dropped; suggest and spec only (never implement here); a rewrite needs a feasibility
+ ROI case; metrics/analytics on demand and after asking; honest effort and ranking.
