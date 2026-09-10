---
name: prep-pr
description: Use when the user says "prep my PR", "get this ready for review", "open a PR for this branch". One entry point: complexity gate, `corgi test --changed`, risk score, then the draft PR. NOT for reviewing someone else's PR (review).
---

# Prep PR

Read `../_shared/conventions.md` and `../_shared/forge-commands.md` first.

One command to take a branch from "works on my machine" to a draft PR/MR a reviewer can
trust. It runs the gates `stories` runs before opening a PR, in order, and stops at the
first red.

## 0. Resolve

- Repos: the cwd repo, or every compose service whose branch is not `<base>`
  (`corgi context --json`). `<base>` and forge per `conventions.md`.
- Ticket key: from the branch name or the last commits; none → ask once, or go on
  without (the title then has no key).
- Dirty tree → ask whether to commit it first; never commit or checkout without an OK.

## 1. Gates, in order

1. **Complexity**: the `complexity` skill in `gate` mode on the diff vs `<base>`.
   `fail` → apply its tactics to the named function, re-gate. No PR on a failed gate.
2. **Tests**: `corgi test --changed --base <base>` (the whole test script of every repo
   that differs). It finds nothing changed → `corgi test --service <svc>` per repo in
   scope. Also run the repo's own CI gate when it has one (coverage floor, typecheck,
   lint) through `corgi exec <svc> -- <cmd>`.
3. **Risk**: the `risk` skill on the local diff. Keep the card; it goes in the body.

Show the three results in one table before drafting. Red → fix, re-run only the red
gate, continue.

## 2. Draft the PR/MR

Follow `stories` Phase 5: push the branch, create the draft with `forge-commands.md`
§6, title `<subject> [<key>]`, body with what / how / tests / issue link, then stamp the
risk card. Draft only; a human flips it to ready. Multi-repo → one PR per repo, sibling
links in each body, contract lines on every side.

Offer, don't do: watch CI to green, move the ticket to review (`tracker` skill), post
the spec comment.

## Output

```
prep-pr <branch>
complexity  pass (max CC 7, threshold 10)
tests       api 212 passed · web 88 passed (corgi test --changed)
risk        4/10 medium · auto-approve: no (touches auth)
pr          <link> (draft)
next        gh pr checks <n> --watch
```
