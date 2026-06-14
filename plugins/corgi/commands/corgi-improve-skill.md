---
description: Refine OR create an agent skill from what happened this conversation. Pass the skill folder / SKILL.md to improve (e.g. "plugins/corgi/skills/stories") + the gap in plain words ("should bring the DB up for migrations, not hand-write them"). A path that does NOT exist yet → scaffolds a new skill there from this session's evidence. NO path → just a conversation summary of gaps + fixes (no edit). Previews any diff/new file before writing; strips all workspace-specific detail.
---

Run the **improve-skill** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = (optional) target skill folder / `SKILL.md` path + plain-words gap.
- Evidence = this conversation: the step that went wrong / slow / got worked around, or
  the user's explicit "next time do Y."

Branch on path (per `plugins/corgi/skills/improve-skill/SKILL.md`):

- **No path → summary mode, NO edit.** Return a digest: **Gaps** (each `[skill?] symptom
  → root cause`), **Fix** (one-line skill change per gap), **Which skill** (best-guess
  target per gap so the user can point you). Pinpoint and concrete. Stop there.
- **Path to an EXISTING skill → edit mode.** Name the gap in one sentence (1), read the
  target + check it isn't already covered (2), draft the **minimal** edit in the skill's
  voice (3), **de-leak** every workspace / company / service / ticket / secret token (4),
  preview the diff + rationale and gate before writing (5), sanity-check in place (6).
- **Path to a NON-EXISTENT skill → create mode.** Confirm it's genuinely new (not a typo
  of an existing skill), name it + its trigger, scaffold `SKILL.md` in the plugin's voice
  (frontmatter `name` + triggers-only `description`, then a tight evidence-grounded body),
  pair a command only if the plugin does, **de-leak**, preview the new file(s) + gate.

Honor every guardrail: evidence-only, minimum diff, **no leak**, preview before writing
a shared / committed skill repo, no-path = summary only, one skill at a time. Large or
discipline-enforcing skill → defer to `superpowers:writing-skills` if installed.
