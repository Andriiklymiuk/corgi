---
name: improve-skill
description: "Use to learn from a session and fix what caused the trouble: \"improve my <X> skill\", \"the skill should've caught Z\", \"make a skill for what we just did\", \"retro\", \"what went wrong in my last 30 sessions\". NOT corgi-compose (corgi) or normal code edits."
---

# Improve or scaffold a skill

Read `../_shared/conventions.md` first.

**Done when:** the diff or the new skill is previewed and written, free of workspace detail, or the conversation summary of gaps is printed.

## Overview
Skill improves only from **real observed gap** - thing an agent (often you, just now)
did wrong, slow, or worked around, that sharper wording prevents. This conversation IS
the failing test. Don't imagine the gap - you watched it. Close with smallest generic
edit, stripped of workspace-specific detail AND of environment-specific assumptions -
assert only the lesson that transfers to other apps, not a one-stack quirk as a universal
rule.

**Evidence, never imagination.** No "while I'm here" adds. No session moment (or
explicit user ask) behind a change → don't make it.

## Four modes - branch on the ask, the path and whether it exists
- **Sessions named** ("retro", "the last 30 sessions") → **retro mode.** Read them, rank what
  went wrong, fix each at the layer that stops it.
- **No path** → **summary only, NO edit.** Dump a conversation digest of gaps + fixes,
  stop. User points at a skill later.
- **Path to an EXISTING skill** (folder / `SKILL.md`) → **edit mode.** Close the gap in
  that skill (Edit mode).
- **Path to a skill that does NOT exist yet** → **create mode.** Scaffold it from the
  session evidence + the user's intent (Create mode). Confirm it's genuinely new first.

## Where a fix belongs

A skill is one place a lesson can live, and rarely the strongest. Before writing a rule, pick the
cheapest layer that stops this for good. Ask in this order:

1. **Can a machine catch it?** A fixed pattern, a banned call, a file in the wrong place gets a check:
   a lint rule, a hook, a CI job, a guard that rewrites or refuses the command. A check that exists but
   is unwired or broken is the finding. A repo with no check at all is a finding too.
2. **Is it a judgement call?** Cross-file consistency, "matches the style" goes to the review stage. The
   reviewer gets a diff and has room to think; the one writing the code has the least room.
3. **Could it not find something?** A long hunt for a file, a command or a hidden link between two files
   wants a pointer next to where it looked, or a doc it was never shown.
4. **Did it lack information?** Logs it could not read, a service it could not query: give it read access.
5. **Was it slow or costly?** An expensive call, a tool with a huge answer, a skill loaded for one line:
   shorten it, or split it into a reference loaded on demand.
6. **Only then, a line in a skill.** Always-loaded files (CLAUDE.md, AGENTS.md, skill descriptions) stay
   small: they cost every session, so they hold pointers, not rules.

**Removing counts.** A line that changes no behaviour is a finding. So is a rule a check now enforces.

## Retro mode (sessions named)
1. **Read the primary source, not your memory of it.** Default to this session. For more, run
   `python3 references/mine_sessions.py --last 30` (every `~/.claude*/projects`): tool errors, the same
   failing call sent twice, skill use, where the person stepped in. Open the transcript behind any
   line you will act on. Its output holds real project data and stays out of the skill.
2. **Weigh what the person said over what the tools said.** A correction costs more than an error, and an
   error the agent recovered from in one step may cost nothing. Count, and quote the count.
3. **Group by cause, not by symptom,** and pick the layer above for each.
4. **Rank by what it costs across sessions,** severity first. Show the table: cause, count, layer, fix.
5. **Apply** what the person asked for, under the guardrails below. A retro may touch several skills; each
   edit still has its evidence. Say what you did not read, and that a rule is not proven until a later
   session behaves differently.

## Summary mode (no path)
Edit nothing. Return:
- **Gaps** - each thing that went wrong / slow / got worked around, one line:
  `[skill?] symptom → root cause`.
- **Fix** - per gap, the one-line skill change that prevents it next time.
- **Where** - per gap, the layer from "Where a fix belongs", and the skill or file when it is one.

Pinpoint, concrete. No vague "could be tighter."

## Edit mode (path given)
1. **Name the gap in one sentence**, grounded in the session. Fuzzy → confirm first.
2. **Read target `SKILL.md`.** Find where it belongs. **Already covered?** → sharpen
   wording / close the loophole, not a duplicate bullet.
   **Symlink check:** target folder, `SKILL.md`, or its plugin dir a symlink → say so,
   resolve the real path, edit the real target. The diff (and any commit) lands in THAT
   repo, not the one you're standing in - name it in the preview.
3. **Draft minimal edit** in the skill's voice + density: one bullet, a tightened line,
   a red-flag, a closed loophole. Not a rewrite. `description` = triggers only, never a
   workflow summary (agents follow it instead of reading the body).
4. **De-leak - non-negotiable.** Strip every workspace token: company / product /
   service names, ticket ids, org-repo slugs, internal URLs, secrets, customer data.
   Generalize to `<svc>` / `<producer>` / "an ORM migration". Real example fine only
   after every identifying token gone.
5. **Preview diff + one-line rationale. Gate.** Target often a shared / committed repo -
   wait for OK before write. Apply minimal.
6. **Check after write:** reads clean in place, in voice, zero workspace tokens,
   description leads with triggers. One skill per run - no batch.

Big or discipline-enforcing change (needs baseline / pressure testing) +
`superpowers:writing-skills` installed → use it. This skill = fast path for a precise
edit from a gap you lived.

## Create mode (path given, target missing)
The named folder / `SKILL.md` doesn't exist - the user wants a skill BORN from what just
happened (this session's friction, or a workflow you just drove end to end).
1. **Confirm it's genuinely new** - scan the skills dir; not a typo / rename of an
   existing skill. Close match → it's an edit on that one, not a create.
2. **Name the skill + its trigger** in one sentence: what it's for, the phrases that fire
   it. Fuzzy → confirm before scaffolding.
3. **Scaffold `SKILL.md`** at the path, matching the plugin's voice + density (read a
   sibling skill): frontmatter `name` (the folder name) + a `description` that is
   **triggers only** (phrases the user will say, never a workflow summary), then a tight
   body - Overview, the steps / modes, Guardrails, Red flags - every line grounded in the
   lived evidence, no filler. Pair a command file ONLY if the plugin gives each skill one
   (match its naming).
4. **De-leak - non-negotiable.** Same as edit: strip every workspace / company / service /
   ticket / secret / internal-URL token; generalize to `<svc>` / `<scheme>` / placeholders.
   A real example is fine only after every identifying token is gone.
5. **Preview the new file(s) + one-line rationale. Gate.** Wait for OK before write - a
   shared / committed skill repo. **Symlink check** as in edit: resolve + write the real
   target, name the repo the diff lands in.
6. **Check after write:** frontmatter valid, description leads with triggers, body in
   voice, zero workspace tokens. One skill per run.

Big or discipline-enforcing skill (needs baseline + pressure testing) +
`superpowers:writing-skills` installed → use it; this create path is the fast scaffold for
a skill you can already describe from lived evidence.

## Guardrails
- **Evidence-only** - every change traces to a session moment or explicit ask.
- **Minimum diff** - close the gap, don't refactor around it.
- **No leak** - zero workspace / company / secret tokens reach the skill.
- **Generalize the lesson, don't just de-leak names** - strip over-specific stack /
  version / device assumptions too; assert only what transfers, and frame a setup-specific
  quirk conditionally ("if X in your stack…") so the edit helps other apps, not just the
  one you saw it on. A reproducible bug+fix is universal; one app's rendering quirk is not.
- **Preview before write** - never silently edit a shared skill repo.
- **No path → summary only**, never edit.
- **One skill at a time**, except a retro the person asked for.
- **A check beats a rule** when the mistake has a fixed shape.

## Red flags - stop
- "I'll also add a section on…" no session moment → imagination, drop.
- Real service / company / ticket name about to enter the skill → de-leak first.
- Rewrite half the skill for a one-line gap → minimum diff.
- Edit before naming the gap → name first.
- Writing "always remember to X" for something a linter or a hook could refuse → build the check.
- Adding to a file every session loads → a pointer, or somewhere else.
- Rule already there → sharpen, don't duplicate.
- Asserting a one-stack / one-version / one-device quirk as a universal rule → conditionalize
  it ("if X on yours…") and state only the transferable fix; verify-per-app, don't assume.
- Edit when no path given → wrong mode, switch to summary.
- Scaffolding a new skill that nearly duplicates an existing one → it's an edit on that
  one, not a create.
- Creating from an imagined workflow with no session evidence → only scaffold what you
  actually drove or the user explicitly described.
