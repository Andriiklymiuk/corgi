---
description: Code-review one or more existing PRs/MRs (GitHub or GitLab) against the repo's standards and any linked Linear/Jira ticket, then post a summary comment + inline suggestions behind a preview gate. Pass PR/MR URLs or numbers; add --yes to skip the preview gate.
---

Run the corgi **review** flow for the PR/MR reference(s) in `$ARGUMENTS`.

- `$ARGUMENTS` = one or more PR/MR URLs or bare numbers, optionally `--yes`.
- No `$ARGUMENTS` → ask the user for the PR/MR link(s), or infer from the current branch's open PR/MR if one exists.

Follow the `review` skill (`plugins/corgi/skills/review/SKILL.md`) end to end: resolve targets (Phase 0), fetch diffs without checkout (Phase 1), pull tracker intent (Phase 1.5), build a per-repo standards note (Phase 2), review each PR scoped to its diff (Phase 3), run the cross-service contract pass when the set crosses a service boundary (Phase 3.5), preview findings and confirm (Phase 4 — `--yes` skips), post summary + inline suggestions (Phase 5), and print the grouped report (Phase 6).

Honor every guardrail: comments only, never merge/approve/push, read-only on the repo, gate before posting unless `--yes`, never echo secret values.
