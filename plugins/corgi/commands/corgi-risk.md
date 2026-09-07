---
description: Score how much human review a change or a story deserves — "risk assessment for this PR/MR", "how risky is this story", "can this be auto-approved", "who should review this", "add the risk score to the description". Pass a PR/MR link or number, a branch, a tracker key, or nothing for the local diff; add "stamp" to write the card into the description.
---

Run the corgi **risk** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = one or more PR/MR links or numbers, a branch name, a tracker story
  key (Linear/Jira), or a feature sentence. Empty → the local diff against the
  base branch in the current service dir.
- The word `stamp` anywhere in the arguments → write the card into each PR/MR
  description (idempotent, previewed first). Otherwise print it only.
- A story key or free text with no code yet → a forecast marked `confidence: low`,
  with the decisions a human must make before an agent builds it.

Follow the `risk` skill (`plugins/corgi/skills/risk/SKILL.md`): resolve the target
without a checkout, gather the evidence table, score with `references/rubric.md`
(highest dimension first, floors never lowered), pick the Check lines from
`references/checklists.md`, and end with the one-line
`risk: N/10 <tier> · auto-approve: yes|no — <reason>` summary.

Every line on the card cites a file, a test, a check, or a ticket line; a risk you
cannot point at is not on the card. Never mark a story, a red CI, or a cross-repo set
as auto-approvable.
