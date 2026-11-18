---
name: deps
description: "Use to upgrade dependencies across a repo or the whole corgi workspace: \"update the dependencies\", \"bump React\", \"upgrade to Node 24\", \"fix the audit warnings\", \"we are three majors behind\", \"move every service to the same version\". Reads the changelog, upgrades one thing at a time, proves it on the running stack. NOT an Expo SDK upgrade with its own skill, or a new feature that happens to need a library."
---

# Upgrade dependencies, one at a time

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** each upgrade is its own commit with green tests and a healthy stack behind
it, and the report lists what moved, what was held back and why.

An upgrade is a change to code you did not write. Its risk is in the changelog, not in the
version number: read what changed before you install it.

```
inventory ──▶ group and order ──▶ read the changelog ──▶ upgrade one ──▶ test + boot the stack
                                                              ▲                    │
                                                              └──── next ◀── commit ┘
```

## 1. Inventory

- **Per repo, with its own tool:** `npm outdated` / `pnpm outdated` / `bun outdated`,
  `go list -u -m all`, `pip list --outdated`, `bundle outdated`, `cargo outdated`. Then
  the audit: `npm audit`, `govulncheck ./...`, `pip-audit`, `bundle audit`.
- **Across the workspace:** `corgi context --json` lists the repos. The same library at
  different versions in two services that talk to each other (a shared client, a schema
  library, a protobuf or GraphQL tool) is a finding of its own.
- **The runtime too:** the Node, Go, Python or Ruby version in `.nvmrc`, `go.mod`, the
  Dockerfile and the CI config must agree. One that moved alone is a bug waiting.
- **Baseline before any change:** tests, types, lint, build, and `corgi status` on the
  running stack. A red baseline goes in the report.
- `corgi checkpoint before-deps` marks every repo, so `corgi restore before-deps` puts it
  all back.

## 2. Group and order

1. **Security fixes first**, the ones with a known hole in code the app really runs.
   An audit line in a dev tool that never ships is low.
2. **Patches and minors**, grouped per repo when the changelogs are quiet.
3. **Majors, one each**, never two in one commit.
4. **Things that move together stay together:** React with React DOM, a framework with
   its plugin, a client with its generated types, an ORM with its migration tool.
5. **Shared contracts in dependency order:** the service that produces a schema or a
   client first, its consumers after, each in its own PR with the merge order written
   in both bodies.

Held back, with the reason in the report: a major with no time for its migration, a
version the platform does not support yet, a library pinned by a comment or an issue.
Read the pin's reason before you move it.

## 3. Read before you install

For every major, and every minor of a library the app leans on:

- **The changelog or release notes, from the source** (the repo's releases page, the
  migration guide), for every version between the old one and the new one. Search the
  web when the session can; do not trust memory for an API that changed last month.
- **Breaking changes that touch this code:** grep for each removed or renamed API. No
  hit means it does not apply; a hit is a line in the plan.
- **New peer and engine requirements:** a library that now needs a newer runtime moves
  the runtime first, as its own step.
- **A codemod, when the project ships one**, beats hand edits. Run it, then read its diff.

## 4. Upgrade one, prove it

- **The package manager's own command**, so the lockfile changes with the manifest. Never
  edit a lockfile by hand; never delete it to "fix" a conflict (that upgrades everything
  at once). The lockfile is committed with the change.
- **Then, in order:** install, types, lint, unit tests, build, and the stack:
  `corgi test --changed --base <base>`, `corgi restart`, `corgi status --until-healthy
  --timeout 3m`. A library used at runtime is exercised for real: the screen (`web`,
  `mobile` skill) or the route it serves.
- **Red after an upgrade:** read the failure against the changelog. Fix the code to the
  new API. Three honest tries on one upgrade, then revert that one, hold it back with
  the failing line in the report, and go on to the next.
- **Never silence it:** no `--force`, no `--legacy-peer-deps`, no `overrides` or
  `resolutions` to hide a conflict, no skipped test, without saying so and why in the
  commit's PR.
- **One upgrade, one commit**: `bump <lib> 4.2 → 5.0: <what had to change>`. Each can be
  reverted alone.

## 5. Report

```
moved   next 15.1 → 16.0       async request APIs: 14 call sites changed, codemod + 3 by hand
moved   12 patches in api      quiet changelogs, tests 412/412
fixed   2 high audit findings  (lodash, tar) by the bumps above
held    prisma 6 → 7           needs Node 22; the Dockerfile is on 20: its own PR first
held    aws-sdk 2 → 3          a rewrite of every call: two days, listed not started
none    stack healthy after each; baseline again: nothing worse
```

Every held row has its reason and its first step.

## On a clock

`corgi agent routine add deps` runs this weekly as an unattended run: patches and minors
with quiet changelogs become one draft PR per repo, majors become cards on the board with
their breaking changes listed. A person reads the majors.

## Red flags

- A lockfile deleted and regenerated.
- Twenty packages in one commit and a red test.
- A major installed before its migration guide was read.
- "Tests pass" for a runtime library, with the stack never booted.
- A pin removed without reading why it was there.

## See also

- **[`security`](../security/SKILL.md)** when the question is whether a hole is reachable.
- **[`quality`](../quality/SKILL.md)** for dead dependencies, **[`ci`](../ci/SKILL.md)** when the runtime version lives in the pipeline.
- An Expo SDK upgrade has its own skill in the `expo` plugin; use it when it is installed.
