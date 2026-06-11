---
name: improve-skill
description: Use when the user wants to refine an EXISTING agent skill from what happened this conversation — "improve my <X> skill", "update the skill so next time it does Y", "the skill should've caught Z", "fix the stories/review/debug skill", or hands you a SKILL.md / skill folder to sharpen. Lived failure or friction in this session is the evidence. NOT for a brand-new skill (superpowers:writing-skills), corgi-compose (corgi skill), or normal code edits.
---

# Improve a skill

## Overview
Skill improves only from **real observed gap** — thing an agent (often you, just now)
did wrong, slow, or worked around, that sharper wording prevents. This conversation IS
the failing test. Don't imagine the gap — you watched it. Close with smallest generic
edit, stripped of workspace-specific detail.

**Evidence, never imagination.** No "while I'm here" adds. No session moment (or
explicit user ask) behind a change → don't make it.

## Two modes — branch on path
- **No path** → **summary only, NO edit.** Dump a conversation digest of gaps + fixes,
  stop. User points at a skill later.
- **Path given** (skill folder / `SKILL.md`) → **edit mode.** Close the gap in that
  skill (Method).

## Summary mode (no path)
Edit nothing. Return:
- **Gaps** — each thing that went wrong / slow / got worked around, one line:
  `[skill?] symptom → root cause`.
- **Fix** — per gap, the one-line skill change that prevents it next time.
- **Which skill** — best-guess target per gap, so the user can point you.

Pinpoint, concrete. No vague "could be tighter."

## Edit mode (path given)
1. **Name the gap in one sentence**, grounded in the session. Fuzzy → confirm first.
2. **Read target `SKILL.md`.** Find where it belongs. **Already covered?** → sharpen
   wording / close the loophole, not a duplicate bullet.
3. **Draft minimal edit** in the skill's voice + density: one bullet, a tightened line,
   a red-flag, a closed loophole. Not a rewrite. `description` = triggers only, never a
   workflow summary (agents follow it instead of reading the body).
4. **De-leak — non-negotiable.** Strip every workspace token: company / product /
   service names, ticket ids, org-repo slugs, internal URLs, secrets, customer data.
   Generalize to `<svc>` / `<producer>` / "an ORM migration". Real example fine only
   after every identifying token gone.
5. **Preview diff + one-line rationale. Gate.** Target often a shared / committed repo —
   wait for OK before write. Apply minimal.
6. **Check after write:** reads clean in place, in voice, zero workspace tokens,
   description leads with triggers. One skill per run — no batch.

Big or discipline-enforcing change (needs baseline / pressure testing) +
`superpowers:writing-skills` installed → use it. This skill = fast path for a precise
edit from a gap you lived.

## Guardrails
- **Evidence-only** — every change traces to a session moment or explicit ask.
- **Minimum diff** — close the gap, don't refactor around it.
- **No leak** — zero workspace / company / secret tokens reach the skill.
- **Preview before write** — never silently edit a shared skill repo.
- **No path → summary only**, never edit.
- **One skill at a time.**

## Red flags — stop
- "I'll also add a section on…" no session moment → imagination, drop.
- Real service / company / ticket name about to enter the skill → de-leak first.
- Rewrite half the skill for a one-line gap → minimum diff.
- Edit before naming the gap → name first.
- Rule already there → sharpen, don't duplicate.
- Edit when no path given → wrong mode, switch to summary.
