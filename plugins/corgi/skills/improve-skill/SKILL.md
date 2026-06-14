---
name: improve-skill
description: Use when the user wants to refine OR create an agent skill from what happened this conversation — "improve my <X> skill", "update the skill so next time it does Y", "the skill should've caught Z", "fix the stories/review/debug skill", "make a skill for <workflow we just did>", or hands you a SKILL.md / skill folder to sharpen. Lived failure or friction (or a workflow you just drove) in this session is the evidence. If the path points at a skill that does NOT exist yet, it SCAFFOLDS a new one there from the same evidence (a big or discipline-enforcing skill defers to superpowers:writing-skills). NOT for corgi-compose (corgi skill) or normal code edits.
---

# Improve or scaffold a skill

## Overview
Skill improves only from **real observed gap** — thing an agent (often you, just now)
did wrong, slow, or worked around, that sharper wording prevents. This conversation IS
the failing test. Don't imagine the gap — you watched it. Close with smallest generic
edit, stripped of workspace-specific detail.

**Evidence, never imagination.** No "while I'm here" adds. No session moment (or
explicit user ask) behind a change → don't make it.

## Three modes — branch on path + whether it exists
- **No path** → **summary only, NO edit.** Dump a conversation digest of gaps + fixes,
  stop. User points at a skill later.
- **Path to an EXISTING skill** (folder / `SKILL.md`) → **edit mode.** Close the gap in
  that skill (Edit mode).
- **Path to a skill that does NOT exist yet** → **create mode.** Scaffold it from the
  session evidence + the user's intent (Create mode). Confirm it's genuinely new first.

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
   **Symlink check:** target folder, `SKILL.md`, or its plugin dir a symlink → say so,
   resolve the real path, edit the real target. The diff (and any commit) lands in THAT
   repo, not the one you're standing in — name it in the preview.
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

## Create mode (path given, target missing)
The named folder / `SKILL.md` doesn't exist — the user wants a skill BORN from what just
happened (this session's friction, or a workflow you just drove end to end).
1. **Confirm it's genuinely new** — scan the skills dir; not a typo / rename of an
   existing skill. Close match → it's an edit on that one, not a create.
2. **Name the skill + its trigger** in one sentence: what it's for, the phrases that fire
   it. Fuzzy → confirm before scaffolding.
3. **Scaffold `SKILL.md`** at the path, matching the plugin's voice + density (read a
   sibling skill): frontmatter `name` (the folder name) + a `description` that is
   **triggers only** (phrases the user will say, never a workflow summary), then a tight
   body — Overview, the steps / modes, Guardrails, Red flags — every line grounded in the
   lived evidence, no filler. Pair a command file ONLY if the plugin gives each skill one
   (match its naming).
4. **De-leak — non-negotiable.** Same as edit: strip every workspace / company / service /
   ticket / secret / internal-URL token; generalize to `<svc>` / `<scheme>` / placeholders.
   A real example is fine only after every identifying token is gone.
5. **Preview the new file(s) + one-line rationale. Gate.** Wait for OK before write — a
   shared / committed skill repo. **Symlink check** as in edit: resolve + write the real
   target, name the repo the diff lands in.
6. **Check after write:** frontmatter valid, description leads with triggers, body in
   voice, zero workspace tokens. One skill per run.

Big or discipline-enforcing skill (needs baseline + pressure testing) +
`superpowers:writing-skills` installed → use it; this create path is the fast scaffold for
a skill you can already describe from lived evidence.

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
- Scaffolding a new skill that nearly duplicates an existing one → it's an edit on that
  one, not a create.
- Creating from an imagined workflow with no session evidence → only scaffold what you
  actually drove or the user explicitly described.
