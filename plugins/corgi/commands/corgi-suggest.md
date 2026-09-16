---
description: Suggest the few things worth building next in a corgi workspace — reads the stack, the READMEs, the memory and the board once, then proposes three to six evidence-backed ideas across three lenses (magic a user would feel, friction a developer trips on, risk that will bite), each with how you will know it worked. Specs the chosen one and puts it on the board as a task or a tracker ticket, then offers to hand it to stories. Pass a focus in plain words ("performance", "the api", "retention"); no args = the whole stack.
---

Run the corgi **suggest** flow for the focus in `$ARGUMENTS`.

- `$ARGUMENTS` = an optional focus: a lens ("magic", "friction", "risk", "performance",
  "security"), a service name, or a goal ("retention"). Empty → the whole stack.
- Run **inside the workspace folder** (the one with `corgi-compose.yml`).

Follow the `suggest` skill (`plugins/corgi/skills/suggest/SKILL.md`) end to end: read the
stack, the promise, the memory and what is already taken once (Phase 0), walk the three
lenses in one pass (Phase 1), write cards and cut what is not real (Phase 2), present a
ranked shortlist, magic first (Phase 3), spec the chosen one in the stories shape (Phase
4), then give it a home — a corgi task, a tracker ticket or just the spec — record it in
the suggest history and offer to hand it to `stories` (Phase 5).

Honour every guardrail: one pass in this session, no agent per lens; evidence on every
card or it is cut; suggest and spec only, never implement here; a rewrite needs an ROI
case; metrics read-only and after asking; what the board, the history or a memory
decision already holds is not proposed again.
