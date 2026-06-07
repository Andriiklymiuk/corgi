---
description: Code-review one or more existing PRs/MRs (GitHub or GitLab) against the repo's standards and any linked Linear/Jira ticket, then post a summary comment + inline suggestions behind a preview gate. OR address review feedback on your own PR — apply the valid comments, reply + resolve the threads, push. Pass PR/MR URLs or numbers (or a story-id for the address mode); add --yes to skip the preview gate.
---

Run the corgi **review** flow for the reference(s) in `$ARGUMENTS`. The skill has two
modes — route from the verb:

- **Give review** (default — *review / look over / check* a PR) → `$ARGUMENTS` = one or
  more PR/MR URLs or bare numbers, optionally `--yes`. No `$ARGUMENTS` → ask for the
  link(s), or infer from the current branch's open PR/MR. Follow the `review` skill
  Phases 0–6: resolve targets (P0), fetch diffs without checkout (P1), pull tracker
  intent (P1.5), per-repo standards note (P2), review each PR scoped to its diff (P3),
  cross-service contract pass (P3.5), preview + confirm (P4 — `--yes` skips), post
  summary + inline suggestions (P5), grouped report (P6).
  Guardrails: **comments only, never merge/approve/push, read-only on the repo**, gate
  before posting unless `--yes`, never echo secret values.
- **Address review** (*fix / address / answer* the comments on **your** PR) →
  `$ARGUMENTS` = a PR/MR link, a bare number, or a **tracker story-id**. Follow the
  skill's **Mode B**: read the incoming threads, apply the valid ones (push back on the
  wrong ones), reply + resolve, gate, then push the fixes.
  Guardrails: **explicit own-branch target only, never infer-and-push**, draft stays
  draft, never force-push/merge/approve, gate before pushing unless `--yes`.

Ambiguous ("check my PR for story X") → ask which mode.
