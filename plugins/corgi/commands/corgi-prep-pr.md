---
description: Prep my PR. One entry point that runs the complexity gate, corgi test --changed against the base branch, a second-reviewer pass and the risk score, then drafts the PR/MR the way stories does.
---

Run the corgi **prep-pr** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a branch, a compose service name, or a ticket key. Empty → the
  current branch in cwd; in a stack folder, every service whose branch is not `<base>`
  (`corgi context --json`).
- Gates, in order, stop at the first red: `complexity` skill in `gate` mode →
  `corgi test --changed --base <base>` (fallback `--service <svc>` when nothing
  changed) plus the repo's own CI gate → a second-reviewer pass on the diff → `risk`
  skill on the diff.
- Then push and open a **draft** PR/MR per repo with the `stories` Phase 5
  conventions (the skill's `../_shared/forge-commands.md` §6), risk card stamped in the body.
- CI is watched in the background and reported when it ends; the ticket moves to review
  only when the user asked. Offer, don't do: post the spec comment.
- Never commit a dirty tree, never flip to ready, never merge.

Follow the `prep-pr` skill (load it with the Skill tool: `corgi:prep-pr`).
