---
description: Prep my PR. One entry point that runs the complexity gate, corgi test --changed against the base branch, and the risk score, then drafts the PR/MR the way stories does. Pass a branch, a service, or a ticket key; no args = the current branch of the cwd repo (or every compose service off base).
---

Run the corgi **prep-pr** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a branch, a compose service name, or a ticket key. Empty → the
  current branch in cwd; in a stack folder, every service whose branch is not `<base>`
  (`corgi context --json`).
- Gates, in order, stop at the first red: `complexity` skill in `gate` mode →
  `corgi test --changed --base <base>` (fallback `--service <svc>` when nothing
  changed) plus the repo's own CI gate → `risk` skill on the diff.
- Then push and open a **draft** PR/MR per repo with the `stories` Phase 5
  conventions (`skills/_shared/forge-commands.md` §6), risk card stamped in the body.
- Offer, don't do: watch CI, move the ticket to review, post the spec comment.
- Never commit a dirty tree, never flip to ready, never merge.

Follow `skills/prep-pr/SKILL.md`.
