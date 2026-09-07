---
name: risk
description: Use when someone needs to know how much human review a change deserves — "risk assessment for this PR/MR", "how risky is this story", "can this be auto-approved", "score this diff", "who should review this", "add a risk score to the description", "which of these PRs need a second reviewer" — or as the gate the stories, review and autopilot skills run on every change they produce. Scores a change (a PR/MR, a branch, the local diff) or a not-yet-built story 1–10 from evidence in the diff and the repo — blast radius, reversibility, data and auth, verification strength, cross-service contract, operations, mobile/native surface, novelty — and writes a short risk card into the PR/MR description saying what a reviewer must check, what they can skip, and whether the change qualifies for auto-approval. NOT for finding bugs (review skill), measuring complexity (complexity skill), or building the change (stories skill).
---

# Risk assessment

## Overview
Agents open more pull requests than a team can read line by line. The question a
reviewer needs answered before they open one is not "is it correct" but **"how much of
my attention does this deserve, and where"**. This skill answers that with one number,
one sentence of review guidance, and a card that names the exact places to look.

Three modes:

- **assess** — score and print the card. Edits nothing. The default for `/corgi-risk`.
- **stamp** — assess, then write the card into the PR/MR description (idempotent,
  behind a preview). What `stories` does when it opens a draft and what `review` offers
  on an existing one.
- **gate** — the one summary line and nothing else,
  `risk N/10 <tier> · auto-approve: yes|no — <reason>`. What a caller that wants only
  the verdict prints — `stories` under each PR link, `tracker` beside each pickup.

Two targets:

- **A change** — a PR/MR link or number, a branch, or the local diff. Scored from the
  diff. This is the normal case and the one with real evidence.
- **A story** — a tracker ticket or a feature sentence with no code yet. Scored from
  the ticket and the code area it will touch, marked `confidence: low` and re-scored
  from the diff once it exists. Use it to decide *before* building whether the story
  is one an agent may take alone or one that needs a human in the loop.

## Guardrails (non-negotiable)
- **Every point on the card cites evidence** — a file, a hunk, a test name, a CI
  status, a ticket line. A risk you cannot point at is not on the card. No generic
  advice ("consider adding tests"), no filler, no repeating the diff summary.
- **Floors are floors.** A change that touches the things in *Floors* below carries at
  least that score whatever else the rubric says. The rubric can raise a floor, never
  lower one.
- **Auto-approve is a claim about evidence, not about confidence.** `yes` needs every
  condition in *Auto-approval* met and visible; one missing → `no` with the missing
  item named. Never `yes` on a story target.
- **Description edits are surgical and idempotent.** The card lives between
  `<!-- corgi-risk -->` markers; re-stamping replaces that block and touches nothing
  else in the body. Never rewrite the author's text, never post as a comment when the
  description was asked for.
- **Human voice, no attribution.** The card reads as a reviewer's note: no "generated
  by", no bot signature, no emoji badge. The HTML marker is the only machine artefact.
- **Preview before writing outward.** In `stamp` mode print the card and the exact
  description diff, confirm, then write — the same gate `review` uses for comments.
  Pre-authorised autonomous runs (`stories`, `autopilot`) skip the prompt, not the
  print.
- **Score the change, not the author.** Generated code is a novelty signal (see
  rubric), never a verdict on its own.

## Phase 0 — Resolve the target
- **PR/MR link or number** → forge from the link or `corgi-compose.yml` (`review`
  Phase 0 rules). Fetch title, body, base, head SHA, diff, changed files, check
  status, linked ticket. **No checkout.**
- **Branch** → `git -C <dir> diff <base>...<branch>`, and `--numstat` for the sizes.
- **Local diff** → `git diff <base>...HEAD` in the current service dir; uncommitted
  work included only when asked.
- **Story** → the ticket (Linear/Jira via the `tracker` skill's read path, or the
  text given) and a grep of the areas it names. Say up front that the score is a
  forecast.
- **A set** (several links, a story spanning repos) → score each, then one line for
  the set carrying the **maximum** and the cross-service contract dimension once.

Read the repo's standards note if `review` already built one (CLAUDE.md / AGENTS.md /
lint config) — a repo that declares a hot path or a protected directory has told you
where its risk lives. `.corgi/memory/` `decision` facts count too.

## Phase 1 — Gather evidence (facts before points)
Collect, per change, without judging yet:

| Fact | How |
|------|-----|
| files, lines added/removed, services touched | `--numstat`, map paths to compose services |
| shared or hot code touched | paths in `shared/`, `lib/`, `core/`, middleware, auth, payment, migration dirs; the standards note's hot-path list |
| schema / contract shapes | migrations, `*.proto`, OpenAPI, GraphQL SDL, TS types exported across packages, API handlers' request/response types |
| config / infra | CI files, Dockerfiles, `corgi-compose.yml`, env templates, IaC, feature-flag definitions, app config (`app.json`, `eas.json`, `Info.plist`, `AndroidManifest.xml`, entitlements) |
| dependencies | lockfile diff, native modules, major-version bumps |
| tests | test files in the diff, tests deleted or skipped, whether changed non-test lines have a test touching them (name-match or coverage report if CI publishes one) |
| CI | check status on head; which checks exist at all |
| ticket | acceptance criteria present, scope of the diff vs the ticket's words |
| proof of behaviour | screenshots / recordings in the body, a FAILS-on-base regression test, a manual test plan |
| history | `git log --oneline -20 -- <touched dirs>`: how often this area changes, whether the last change here was reverted |

## Phase 2 — Score (the rubric)
Eight dimensions; each carries points per `references/rubric.md`. **Score = the
highest single dimension, plus one for every other dimension at 2 or more, capped at
10, then raised to any floor that applies.** Highest-dimension-first because risks do
not average: a tiny, well-tested change to the auth middleware is still an auth change.

| Dimension | What raises it |
|-----------|----------------|
| **Blast radius** | many files/services, shared modules, public API, anything every request passes through |
| **Reversibility** | data migrations, deletes, irreversible side effects (payments, emails, push, store submission), no flag to turn it off, a binary release rather than an OTA/deploy |
| **Data & access** | auth, permissions, session, PII, secrets, crypto, input validation, new third-party data flows |
| **Verification** | no tests for changed lines, tests deleted or skipped, CI red or absent, UI change with no screenshot, bug fix with no FAILS-on-base proof |
| **Contract** | request/response or event shape changes, schema, shared types, merge-order dependency between repos |
| **Operations** | CI/build/infra/env changes, background jobs, retries, concurrency, caching, timeouts, hot-path performance |
| **Mobile & native** | native modules, ABI/SDK bumps, permissions (push, ATT, location), deep links, offline persistence, navigation stack, store metadata / IAP, app config, platform-specific code — see `references/checklists.md` |
| **Novelty** | new dependency, new pattern for this repo, first change in an area, large generated diff, dependency upgrades with changelog-worthy breaks |

Two rules on top of the sum:

- **Escalation.** When a floor applies and verification scores 3 or more, add 1: a
  change that is risky by nature *and* unproven is the case the critical band exists
  for. This is the only way past 8 short of a 4 with five other dimensions raised.
- **Story targets** score verification 3 — nothing is proven yet — and the floors apply
  to what the ticket says the change will touch, since there is no diff to read.

### Floors
| Condition (evidence in the diff) | Minimum |
|----------------------------------|---------|
| touches auth / permissions / session handling | 7 |
| touches payment, billing, IAP, or store submission | 8 |
| destructive or non-reversible data migration (drop, rename, type change, backfill without a down path) | 8 |
| secret, token, or credential handling changed | 8 |
| CI/CD or release pipeline changed | 6 |
| native module added or native SDK bumped (mobile) | 6 |
| changed lines have no test and CI has no coverage signal | 5 |
| cross-service contract changed | 5 |
| a story target (no diff yet) | never below 3, never auto-approve |

### Bands → review tier
| Score | Tier | What the reviewer does |
|-------|------|------------------------|
| 1–2 | **trivial** | one reviewer glances; the only band where auto-approve can be `yes` |
| 3–4 | **low** | one reviewer reads the diff; ~10 min |
| 5–6 | **moderate** | one reviewer reads carefully **and runs it** (the `corgi run --service-branch` line); ~30 min |
| 7–8 | **high** | two reviewers, one owning the touched area; manual check on the real environment or device; ship behind a flag or in a staged rollout |
| 9–10 | **critical** | two reviewers including the owner, the test plan executed and recorded, a written rollback, a deploy window; never merged the same day it opened |

### Auto-approval
`auto-approve: yes` only when **all** hold and are visible in the evidence:
1. score ≤ 2 after floors and escalation;
2. every CI check on head is green (and at least one check exists);
3. every changed non-test line is exercised by a test in the diff or a named existing
   test, or the change is copy/config with a screenshot;
4. no contract, schema, auth, payment, migration, infra or native change;
5. single service, no merge-order dependency;
6. the diff does what the ticket says and nothing else (no scope creep);
7. the change target is a diff, not a story.

Otherwise `no — <first failing condition>`. "Auto-approve" here means a human may
merge on the score alone; it never means the skill merges (nothing in corgi does).

## Phase 3 — Write the card
Short, evidence-first, phone-readable. Exact shape:

```
<!-- corgi-risk score=7 tier=high -->
## Risk 7/10 — high · two reviewers, one owning api; run it before merging

**Why**
- blast radius: two services — `api/handlers/user.go` and `web/src/user.ts` both change
- contract: `api` adds `address` to the user response; `web` reads it (`web/src/user.ts:41`)
- data: a new user field is stored and returned (`api/migrations/0042_address.sql`)
- verification: 3 tests added for the handler; no test touches the `web` reader
- reversibility: additive column, migration has a down path

**Check**
- `web/src/user.ts:41` — null guard on `address` before `api` deploys
- merge order: api first, then web
- run: `corgi run --with-deps --service-branch api=feature/ABC-200 --service-branch web=feature/ABC-200`

**Skip** — the renamed test helpers (`api/handlers/user_test.go`) are mechanical.

auto-approve: no — cross-service contract
<!-- /corgi-risk -->
```

Rules for the card:
- **Why** lists only the dimensions that scored ≥ 2, each with its evidence. A 2/10
  card has one or two lines; a 9/10 card has one line per floor hit plus the rest.
- **Check** is the reviewer's route through the diff: files and lines in the order
  worth reading, the run line when the tier says to run it, the manual step for
  mobile/UI. Three to six items; more means the change should be split — say so.
- **Skip** names what is safe to skim so the reviewer's time goes where the risk is.
  Omit the line when nothing qualifies.
- A story card adds `confidence: low — scored from the ticket; re-run on the diff` and
  a **Before building** list: the decisions a human must make first (which flag, which
  migration strategy, which rollout).
- No prose beyond those lines. No restating the diff. No "consider".

## Phase 4 — Stamp (only in stamp mode)
1. Fetch the current description. Find an existing `<!-- corgi-risk … -->` …
   `<!-- /corgi-risk -->` block.
2. Replace it, or append the card after the author's text under a blank line. Keep
   everything else byte-for-byte.
3. Preview the resulting body (or the diff of it) and confirm — unless pre-authorised.
4. Write: GitHub `gh pr edit <n> --body-file <tmp>`; GitLab `glab mr update <iid>
   --description "$(cat <tmp>)"`. Read the body back and confirm the marker is present
   exactly once.
5. Optional, when the forge supports labels and the repo already uses them: apply
   `risk:<tier>` so the list view carries the score. Never create a label scheme the
   repo does not have.

No write permission → print the card and say it was not stamped; never fall back to a
comment silently.

## Output
`assess` and `stamp` end with the card, then one line per target for a set, then the
summary line — one form everywhere, so a caller can match it as a prefix:

```
risk 7/10 high · auto-approve: no — cross-service contract
```

`stamp` appends ` · stamped: yes (api#42, web#37)` (or `stamped: no — <why>`) to that
line; `gate` prints the line alone. The `review` skill folds the card's first line
into its summary headline and prints this line in its report; `stories` prints it
under each PR link and `tracker` beside each pickup; `autopilot` names over-ceiling
stories in its heartbeat note with it. Keep the form exact.

## Red flags — stop
- A card with no file references → you scored from the title. Re-read the diff.
- Score fell because the diff is small → size is one dimension; the floors decide.
- "Well tested" with no test file in the diff and no named existing test → verification
  scores 3 until one is named.
- Auto-approve `yes` on a story, a red CI, or a set with a merge order → wrong by
  definition.
- The card is longer than the change → cut to the evidence lines; the number and Check
  are what people read.
- A reviewer disagrees with the score → their read wins; re-stamp with the corrected
  score and the reason, don't argue in the thread.

## See also
- `references/rubric.md` — the point scale per dimension and worked examples.
- `references/checklists.md` — what to check per area (backend, web, mobile, data,
  infra) so Check lines are specific, not generic.
- **`review`** — runs `assess` on every PR it reviews and offers `stamp`.
- **`stories`** — runs `stamp` when it opens a draft PR/MR; prints the score in its report.
- **`autopilot`** — compares the line against its `maxRisk`; an over-ceiling story is left for a human.
- **`complexity`** — a complexity-gate fail is verification evidence here, not a score of its own.
