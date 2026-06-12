---
name: stories
description: Use when the user wants to ship work across a corgi-compose workspace — EITHER a batch of tracker issues (Linear/Jira links/keys like ABC-123; "do these stories", "implement these tickets", "ship X and Y") OR a free-text feature ("build a feature that …", "add X across the services") OR "find/pick what to work on" with nothing named ("pick the stories", "what should I work on", "grab some agent tickets"). NOT for authoring/running corgi-compose itself (use the corgi skill) or a trivial one-line edit you'd just make directly.
---

# Corgi stories

Work items — tracker issues (Linear/Jira) **or** a free-text feature — → spec each
→ isolated branch(es) → tested + reviewed → **draft** PR/MR per repo → grouped
report. Services, dirs, dependency order: all from `corgi-compose.yml`. Never
hard-code.

## Speed model

Lighter than a full multi-agent pipeline. **One blocking gate** on the
adjustment/bug fast path; complex stories add superpowers checkpoints.

- **Gate (blocking): spec sign-off** (Phase 2). Confirm intent before any branch.
  Cheap, guards the whole batch. Never skip — a clear up-front directive collapses
  it to inline diagnosis (Phase 2 fast-path): still confirms intent, no separate
  pause.
- **No final gate.** Draft PR/MR: push, open draft, scoped review, report diff +
  link; human flips to _ready_. Draft = no CI/notify, reversible.

### Story tiers — set per story (Phase 1), drives rigor

| Tier           | What                                                              | Extra rigor                                                          |
| -------------- | ----------------------------------------------------------------- | -------------------------------------------------------------------- |
| **Adjustment** | clear-spec UI/copy/flag/config; unambiguous                       | just a test for the new behaviour                                    |
| **Bug**        | broken / regressed                                                | regression test **FAILS on base branch** before fix, passes after    |
| **Feature**    | new behaviour, real design, or new/changed cross-service contract | hand to **superpowers** if installed, else equivalent inline (below) |

**Tier ≠ span.** Complexity axis vs single/multi-service (Phase 4). Multi-service
adjustment is still an adjustment. Most stories = adjustments → fastest path.

**Express lane — micro adjustment.** Adjustment whose whole change is a handful of
files you can already name (asset/copy/flag/style swap, one component) → lighter
steps, same guardrails:

- **No `Explore` subagent** — grep + read the 2–3 files inline; a subagent returns a
  full report to map one component, the orchestrator's own search is cheaper. Reserve
  `Explore` for unclear surface or real fan-out.
- **No local `docs/stories/*.md`** — the `## Spec` tracker comment IS the spec, don't
  write both (multi-service/feature still gets the file).
- **Right-size proof** — gate once (typecheck/lint/test) + the single proof the change
  needs (visual swap → one screenshot/serve check). Don't stack every proof.

Still mandatory: gate sign-off, spec + QA comments, branch, per-story review, draft
PR, report. Unsure it's micro → take the normal path.

**Bug sub-type — logic vs visual.** "FAILS on base" assumes a unit test can _see_
it. **Visual/layout bug** (z-index/stacking, overflow, position, breakpoint, CSS
specificity/cascade) → jsdom can't catch it; a green unit test proves nothing.
Don't fake one.

- **Repo has a visual/e2e harness** (Playwright, Cypress, Storybook visual diff;
  Maestro for Expo/RN — `references/expo-verification.md`) → red check **there**,
  same FAILS-on-base.
- **None** → **manual-only is legit, not a skip.** Spec + PR body carry repro steps
  - before/after screenshot; report says manually verified, no auto guard. Still
    post the **QA "what to test" comment** — a human must re-check visual.
  - **Expo/RN service on a macOS host → not manual-only:** drive the simulator
    with Maestro + screenshots even without a committed harness
    (`references/expo-verification.md`).

### Complex story → superpowers

Bigger than adjustment (real design, unclear approach, large surface, new contract)
→ don't force one-shot. **superpowers installed**, via `Skill`:

- `superpowers:brainstorming` — settle intent + approach before code.
- `superpowers:writing-plans` — becomes the spec doc.
- `superpowers:test-driven-development` + `superpowers:executing-plans` — build,
  tests first.
- `superpowers:verification-before-completion` — prove before draft PR.

Not installed → equivalent inline: settle approach with user, plan into spec, tests
first, verify before push. Either way flows back here — same spec doc, one gate,
per-story review, draft PR, grouped report.

## Guardrails (non-negotiable)

- **Never touch `manualRun` services/db_services.** Reference-only — corgi doesn't
  start them, this flow doesn't change them. Fix lands there → STOP, flag
  out-of-band.
- **Draft PRs/MRs only.** Never non-draft, never merge, never force-push.
- **One blocking gate** — spec sign-off (Phase 2); the sign-off _is_ the branch
  authorization.
- **No destructive git without explicit OK** — checkout off a dirty tree, branch
  deletes, force-push, pushing shared branches.
- **Don't push the workspace meta repo** unless asked — only service branches.

## Optional tooling (degrade gracefully)

Stands alone. Used if present, never required:

- **`superpowers:*`** (separate plugin) — complex-story engine + nicest review.
  Missing → inline. Don't block on a missing plugin.
- **`expo:*`** (separate plugin) — when a service is an Expo/RN app, its skills
  (`expo:expo-dev-client`, `expo:building-native-ui`, …) deepen the simulator
  verification in `references/expo-verification.md`. Missing → the reference
  alone suffices.
- **A code-review command** (e.g. `/code-review`) — Phase 3.5 if present; else a
  review subagent works everywhere.

Always available, all the flow needs: `git`, `gh`/`glab`, `corgi`, `Explore`/`Task`
agents, tracker MCP.

---

## Phase 0 — Read workspace from corgi-compose.yml

1. Locate (`ls corgi-compose.yml *.corgi-compose.yml`). None → `/corgi-new` first,
   or ask which repos; don't guess a layout.
2. **Read the yaml, extract only needed keys** —
   `services.<name>.{path,cloneFrom,manualRun}`, `depends_on_services`, `exports`
   (schema: `skills/corgi/references/yml-schema.md`). Don't render the whole
   project: `/corgi-describe` is too many tokens; `corgi --describe` dumps JSON then
   still runs the command. Build:
   - **Service → dir map.** `path:` (local) or `cloneFrom:` (clone target) = the
     repo you branch in. `cloneFrom` not on disk → `corgi init` clones first.
   - **Dependency/order graph** from `depends_on_services` + `exports`/
     `${producer.VAR}`. Depended-on service (schema/contract owner) first; consumers
     follow. Cycles → flag.
   - **manualRun set** → exclude (Guardrails).
3. **Per repo: forge, base, commands.**
   - Forge: `git -C <dir> remote get-url origin` → `*github.com*` = `gh`; `*gitlab*`
     = `glab`. A batch may span both.
   - Base: `git -C <dir> symbolic-ref --short refs/remotes/origin/HEAD` (or
     `gh repo view --json defaultBranchRef -q .defaultBranchRef.name` /
     `glab repo view`). `<base>` for branch, red test, PR target.
   - Test/typecheck/lint/build: discover from `package.json` scripts, `Makefile`,
     `pyproject`/`go.mod`, service `start`/`beforeStart`/`scripts`. Don't assume a
     runner. **Also find the CI gate the PR will actually face** — a coverage-threshold
     script (`test:cov:check`, a `check-coverage` step, a `--coverage` floor) or the CI
     workflow (`.github/workflows`, `.gitlab-ci.yml`). Note it; Phase 3 runs that same
     gate before the PR, not just a scoped test.
4. **Detect tracker.** `linear.app` URL → Linear (`mcp__linear-server__*`).
   `atlassian.net`/Jira → Jira (`mcp__atlassian__*`; `getAccessibleAtlassianResources`
   for sites). Bare key + both connected → ask.

## Phase 1 — Investigate (once), then spec

**Route the intake first — what am I building?**

- **Explicit tickets** (keys/links) or a **free-text feature** → continue below.
- **"Find/pick what to work on"**, nothing named → **don't guess tickets.** Resolve
  via the `tracker` skill's **pickup** — the `agent` queue (label `agent`, not In
  Progress/Done), drift-skipped — confirm picks, then build here. Same selection as
  `/corgi-queue`: selection in `tracker`, building here.

**Tracker issue:** fetch, **view screenshots**, read real code paths.

- Fetch **with relations** — Linear `get_issue` `includeRelations: true`; Jira issue
  links. `duplicateOf` / `relatedTo` / parent epic often already carries or shipped
  the work — see _Already shipped?_ below.
- Screenshots — **visual/QA bug from text alone = guess. Get the image.**
  - **Linear** = `curl` the `uploads.linear.app` URL (signed, expires ~5 min —
    re-fetch issue for a fresh URL), read.
  - **Jira** = `getJiraIssue` gives attachment **metadata only** (filename +
    `content` URL `/rest/api/3/attachment/content/<id>`), no bytes.
    **`mcp__atlassian__fetch` won't get bytes** — ARIs only; an attachment id
    mis-resolves to the wrong issue. No MCP tool returns bytes. Jira's URL needs an
    auth header you don't hold. So: 1. Have creds (`$JIRA_EMAIL`/`$JIRA_API_TOKEN`)?
    `curl -u "$JIRA_EMAIL:$JIRA_API_TOKEN" -L -o /tmp/<name> "<content-url>"`, read.
    Usually not in env. 2. No creds / 401 / 403 → **stop guessing. Ask the user to
    download + share the path** (`~/Downloads`), `ls -t ~/Downloads | head`, read.
- **Local / user-pointed design assets.** User names a folder/file
  (`design-screenshots/`, Figma export on disk, "see mockup") → read direct,
  outranks prose. **Build as drawn** — exact icon/emoji, colour, copy, spacing,
  placement. No near-equivalent; a human compares the result to the design.

### Reuse an existing spec — then re-verify it (may be stale)

Before speccing from scratch, check **three** places a prior spec may live — a
human's, a past `stories` run's, a `decompose` ticket's acceptance criteria:

1. **Ticket description** — the body often carries the intended approach.
2. **Ticket comments** — list them (Linear `list_comments`; Jira via `getJiraIssue`
   / dev panel). A comment whose first heading is `## Spec` is a prior run's spec.
3. **Local `docs/`** — `docs/stories/<issue-key>-*.md` (this skill's output) + any
   hand-written design doc whose name/heading matches.

Found one → **starting hypothesis, not ground truth.** Re-resolve every `file:line`
against the current tree, re-confirm the contract — code moves, specs rot. Say what
drifted, rewrite stale parts, keep what holds. Nothing found → spec from scratch.

**Bug tier — check for a dead prior fix first.** `git blame` / `git log -S"<symptom>"`
the suspect lines. Prior fix present but bug persists → inert (overridden, lost
specificity, runtime style wins, wrong selector, behind a flag). Explain _why_ it's
dead, revive/correct it — don't stack a second half-fix. Note the why in root-cause.

### Already shipped? — verify before spec or branch

Reuse-spec check (above) finds a prior _spec_; this finds the _deliverable already
built on `<base>`_ — by a **sibling / related / duplicate ticket**, or an earlier PR
that bundled it. Re-build wastes the batch + opens a dup PR. Before speccing an
**actionable** story, prove it is NOT already done:

- **Read relations** (fetched above). Open any `duplicateOf` / `relatedTo` / parent
  epic — its description or **merged PR** often already covers this ask.
- **Grep `<base>` for what the story adds** — component, copy string, route, flag:
  `git -C <dir> log --oneline -S"<symbol>" -- <area>`, `git grep "<copy>" origin/<base>`.
  Present + wired → shipped.
- **Open the real file to confirm** — not an `Explore` summary. Explore hands you
  code that already satisfies the story; "found it" ≠ "needs building."

Shipped → **do NOT branch.** Mark `Status: ALREADY DONE`, comment the lineage
(commit/PR that delivered it) + recommend close as duplicate, report no-op.
**Comment ≠ close** — ask before changing ticket state. Phase 1 finding → surfaces
_at the gate_, not after a wasted branch.

### Free-text feature (no ticket) — locate work first

Description, not links → no fetch, nothing says _where_ code goes. (First check
`docs/` for an existing design doc — _Reuse an existing spec_.) Find target
service(s) before speccing:

1. **Map intent → service(s)** from `corgi-compose.yml` (names, paths,
   `depends_on_services`) + the **README next to the compose** + per-service READMEs.
   Don't guess.
2. **Confirm with `Explore`** scoped to candidate service(s) — find real files.
3. Genuinely ambiguous → spec-gate question (ask, or `superpowers:brainstorming`);
   don't guess.

Described feature = usually **Feature tier**: `superpowers:brainstorming` (or inline
Q&A) to settle scope → `superpowers:writing-plans` for the spec. After sign-off
(Phase 2), **offer to create a tracker issue** (Linear `mcp__linear-server__save_issue`
with `title`+`team` and no `id` / Jira `mcp__atlassian__createJiraIssue`) for a key +
auto-link; declined → spec stays
local + on PR, branch drops the key segment (Phase 3). **A caller (e.g. `suggest`)
that already created the issue + hands you key + spec → use that key, don't re-create.**

### Investigate once — don't re-research

Batched stories overlap. Re-exploring per story doubles tokens. So:

1. **Cluster** by **service + area** before dispatching.
2. **One `Explore` sweep per area, not per story** — all that area's questions in
   one agent. Never per-story over the same files. (Micro adjustment → no subagent at
   all; grep inline — _Express lane_.)
3. **Orchestrator = the cache.** Subagents can't share context mid-flight: scope
   sweeps to not overlap, collect each into one **investigation note** (scratch —
   memory or a gitignored file), specs reference it.
4. **Reuse ledger** — shared components/contracts recorded once; stories cite, don't
   re-derive.
5. **Need runtime/deployed data** (staging/prod error, request trace, logs you can't
   get locally)? Invoke the **`debug`** skill (Step 4 — provider data), fold findings
   into the note; don't hand off the whole flow.
6. **Workspace memory (read)** — `.corgi/memory/` exists → read first
   (`corgi memory list --json` / `index.md`, then matching facts; see `memory` skill):
   honor a
   `decision` constraint, reuse an `incident` fix for a regression, ground a
   free-text feature in `domain` facts. Absent → skip.

### Write the spec — every story

`docs/stories/<issue-key>-<slug>.md`, actionable or not (micro adjustment → skip the
file; the `## Spec` comment is the spec — _Express lane_):

- Problem (quote issue) + **which services** (drives branch/PR count).
- **Tier** — adjustment/bug/feature.
- Root cause / current behaviour, `file:line` refs.
- Change plan (snippets) **grouped by service**, tests, manual verification, risks.
  Multi-service: `## Contract` + cross-service order.

### Triage: actionable vs blocked — controls POSTING, not writing

- **Actionable → post.**
  - **Spec → a comment** on the issue (human-readable, not a `.md` attachment).
    Linear `mcp__linear-server__save_comment({ issueId, body })` (create-or-update:
    omit `id` to create, pass `id` to update); Jira
    `mcp__atlassian__addCommentToJiraIssue`. Literal newlines/markdown. **Open with a
    `## Spec` heading** — no HTML comment marker (trackers render `<!-- … -->` as
    visible text), no footer badge. That heading is how a later run (new session, no
    comment id) finds it: list comments, match the one whose first heading is `## Spec`,
    **update** it (`save_comment({ id, body })`) instead of duplicating.
  - **What to test → a separate comment** (non-engineer reads inline). Plain QA:
    clicks + outcome, no code/file refs, end `Expected:`. Skip non-testable stories.
- **Blocked → do NOT post.** Spec local only; mark `Status: BLOCKED` + **Decision
  needed**; surface the choice. Hold it; rest of batch proceeds.

`superpowers:brainstorming` / `superpowers:systematic-debugging` (if installed) to
resolve ambiguity before blocking.

## Phase 2 — Gate: spec sign-off (the one blocking gate)

Present all actionable specs in **one round**; sign-off before any branch —
batch-level, not per-branch. Re-present only changed specs. Blocked held out.
Superpowers-escalated stories pass here too: their `writing-plans` output is the
spec.

**Fast-path — collapse the approval round to inline diagnosis ONLY when all hold.**
Gate just confirms intent before branching. Collapse it (state diagnosis + the exact
change inline, then go — that _is_ the gate, they can still stop you) **only when
every one is true:**

- exactly **one** named item (one ticket, or one specific fix the user named),
- tier **adjustment or bug** (never feature),
- **single service**, no cross-service contract,
- you can state the **exact change in one sentence**, no open question.

**Any one false → full gate:** feature tier · >1 story · multi-service · ambiguous
target · you had to ask yourself a clarifying question · can't name the change in one
sentence. **Unsure → full gate.** "User said 'just fix it'" is **not** clearance if
you can't say exactly what "it" is — a vague directive = _open_ intent → gate. The
fast-path skips the _pause_, never the _thinking_.

**Fast-path drops the approval pause, NOT the tracker artifacts.** Even on a
one-liner, post the **spec comment + QA "what to test" comment** (Phase 1 triage) —
durable record, and a visual/QA defect is what a human must re-verify. Don't skip.

## Phase 3 — Branch + implement + verify per story

Branch: `feature/<issue-key>/<kebab-slug>`, same name in every affected repo.

**Get `<issue-key>` from the tracker, don't invent it** (auto-link token):

- **Linear** — `get_issue` → `identifier` (`ABC-123`) + suggested `gitBranchName`.
  Use `identifier`; Linear links any branch containing it (case-insensitive). (Or
  `gitBranchName` verbatim.)
- **Jira** — `getJiraIssue` → `key` (`PROJ-123`). Dev panel / Smart Commits link by
  that token.
- **No ticket** — `feature/<kebab-slug>`, no key segment (or the key of an issue you
  created in Phase 1).

Key also in commit + PR/MR title (Phases 4–5). Same branch name across repos so
multi-repo PRs group.

**Move the ticket to in-progress + assign when work starts.** As each actionable,
ticketed story's branch is created (post sign-off):

- **Transition to the team's started state** — **resolve, don't hardcode "In
  Progress":** Linear `save_issue({ id, state })` to the `started`-type state (`state`
  takes a type/name/id; resolve via `list_issue_statuses`);
  Jira `transitionJiraIssue` to the transition whose target is In-Progress
  (`mcp__atlassian__getTransitionsForJiraIssue`).
- **Assign to the mover** — current tracker user: Linear `save_issue({ id, assignee: "me" })`
  (`assignee` accepts a user id/name/email/`"me"`); Jira `editJiraIssue` assignee = current user (`mcp__atlassian__atlassianUserInfo`).
  **Don't steal** — assigned to someone else → leave it, note who; unassigned → take
  it.

Idempotent — skip if already set; skip no-ticket + blocked. The in-progress move
also stops a looping `/corgi-queue` re-grabbing a story in flight (auto-pick takes
only not-In-Progress). The **review** transition fires later, at draft PR — Phase 5.

**Pick branch vs worktree per repo — check the working tree first:**
`git -C <dir> status --porcelain --untracked-files=no` — empty = clean, any output =
dirty. (Ignore stray untracked; `checkout -b` doesn't disturb them.)

| Repo state | Stories touching this repo | Mode                                                                                                             |
| ---------- | -------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| **clean**  | one                        | **branch in place**                                                                                              |
| **dirty**  | one                        | **worktree** — don't disturb the user's uncommitted work, and skip the destructive base checkout on a dirty tree |
| any        | several                    | **worktree per story** (parallel isolation)                                                                      |

Count "stories touching this repo" **across the whole batch up front** — two
single-repo stories both hitting one repo = "several" → both worktree.

**Dirty + overlap guard.** A worktree branches from clean `origin/<base>`, so it
**excludes the user's uncommitted edits**. Before routing a dirty repo to a worktree,
check whether those edits touch the story's files. Overlap → work silently diverges →
**STOP, ask the user to commit/stash or confirm**. No overlap → worktree safe.

**Must-run producer in a worktree → run it with `--service-dir`.** A producer that
must be _running_ for a consumer to verify (Phase 4) can live in a worktree:
`corgi run --service-dir <producer>=/tmp/corgi-wt/<wt-id>-<service>` (below). Only if
corgi lacks the flag (`corgi run --help | grep service-dir`) must it go **in place** —
dirty → ask the user to stash/commit first.

- **Branch in place** (clean tree; or a must-run producer when `--service-dir` is
  unavailable, after stash/commit). Branch straight off the fetched remote base — no
  `checkout <base>`/`pull` dance, no local-divergence trap:
  `git -C <dir> fetch origin && git -C <dir> checkout -b <branch> origin/<base>`.
- **Worktree** (dirty tree, or several stories in one repo). Path
  `/tmp/corgi-wt/<wt-id>-<service>` — `<wt-id>` = `<issue-key>` (or `<kebab-slug>`
  for no-ticket), `<service>` = service name, so a multi-repo story's repos never
  collide. Branch off `origin/<base>` — never touches `<dir>`'s tree:
  ```bash
  git -C <dir> fetch origin
  git -C <dir> worktree prune                                # drop stale entries
  rm -rf /tmp/corgi-wt/<wt-id>-<service>                     # clear a leftover dir (re-run/crash)
  git -C <dir> worktree add -b <branch> /tmp/corgi-wt/<wt-id>-<service> origin/<base>
  # deps dir (node_modules / vendor / target / .venv) gitignored → symlink main
  # checkout's for SEQUENTIAL runs; real install for CONCURRENT runs.
  ln -s "$PWD/<dir>/node_modules" /tmp/corgi-wt/<wt-id>-<service>/node_modules
  ```
  The worktree dir is now this repo's **working dir** — implement, gate, review,
  commit, push, open the PR/MR from it (Phases 3.5–5).
  - **Run a worktree'd service with `--service-dir` (only services in
    `corgi-compose.yml`).** corgi resolves a service from its `path:` (main `<dir>`);
    to _run_ the worktree's code — e.g. a producer a consumer verifies against
    (Phase 4) — pass `--service-dir <svc>=/tmp/corgi-wt/<wt-id>-<svc>`; corgi runs
    that service's env, beforeStart/afterStart, process from the worktree, main
    checkout untouched. Per-service, repeatable — mix worktree + compose `path:`
    services:
    ```bash
    corgi run --detach \
      --service-dir api=/tmp/corgi-wt/ABC-200-api \
      --service-dir web=/tmp/corgi-wt/ABC-200-web
    # services not named (admin, worker, db_services) run from their compose path:
    ```
    `--service-dir` runs the **exact** worktree code. (corgi also has
    `--service-branch <svc>=<branch>` — its _own_ reused worktree off a branch — and
    `--service-checkout <svc>=<branch>` for an in-place checkout; handy for ad-hoc
    "run this branch", but for stories point at your impl worktree.) Needs the flag
    (`corgi run --help | grep service-dir`); without it, run such a producer in
    place. A branched repo that **isn't** a corgi service → no `--service-dir`; run
    its runner in the worktree dir.
  - **Success →** `git -C <dir> worktree remove /tmp/corgi-wt/<wt-id>-<service>` once
    the PR is up. **Failure (Stop rule) →** leave it; report its `/tmp` path. Never
    `worktree remove` a failed story.

Implement to spec; reuse before building. **Minimum diff — no opportunistic refactor,
no over-engineering, no code comments** unless the file already comments heavily. Run
the **per-service gate** (tests + typecheck + lint) BEFORE commit. Tests for every
change, matching existing patterns.

- **Run the gate through corgi when the service is in `corgi-compose.yml`** — gives
  the worktree full resolved env, deps, cwd, so you don't guess the runner or
  hand-build env:
  - `test` script → `corgi test --service <svc> --service-dir <svc>=<worktree-dir>`
    (worktree'd) or `corgi test --service <svc>` (in place).
  - Other command (typecheck/lint/migrate/one-off) →
    `corgi exec <svc> --service-dir <svc>=<worktree-dir> --ensure-deps -- <cmd>`.
  - Not in compose, no `test` script, or no compose → run the discovered runner
    (Phase 0) in the worktree dir. Same `--service-dir <svc>=/tmp/corgi-wt/<wt-id>-<svc>`
    mapping as `corgi run` (Phase 3); drop for in-place. Needs the flag.
- **Bug tier: red test first** — write it, confirm **FAILS on base**, then make it
  pass. Adjustments skip.
- **Pre-existing red baseline.** Repo typecheck/lint may already fail on `<base>`,
  unrelated. Don't chase a whole-repo green that never existed; don't let baseline
  noise hide your breakage. Gate on **no NEW errors** — filter the run to changed
  files, or diff the base error set. Touched files clean; baseline left as-is, not
  "fixed" (scope creep).
- **Scoped test run can false-green.** Path/pattern selector can match **nothing**
  yet exit 0 — jest reads `app/(app)/…` parens + `[id]` brackets as regex, so the
  target suite silently never runs while another file prints PASS. Confirm the
  **intended suite ran** (assert test count > 0); match by filename substring or
  escape the path. Green exit ≠ tests ran.
- **Scoped run misses downstream importers — prefer the repo's FULL suite.** Running
  only the suites for the files you touched can pass while a **barrel/`index`/sibling
  suite that imports your changed module** fails to even load — a new import you added
  pulls an unmocked native/heavy dep into that suite's graph. corgi gives you the
  resolved env, so the full suite is cheap: run the repo's whole `test` script via
  `corgi test --service <svc>` (what CI runs) instead of cherry-picking files. If you
  must scope, also run any suite that **imports** the module you changed. A green
  cherry-picked run that skips the importer is how a red CI slips through.
- **Run the repo's CI gate, not just tests — a coverage threshold counts.** Many repos
  fail CI on a **per-changed-file coverage floor** (`test:cov:check` / `check-coverage`),
  computed over the diff vs `<base>` and often **aggregated across all changed files** —
  one 0%-covered file sinks the average. A green test run (scoped or full) says nothing
  about coverage; run that exact gate before pushing (Phase 0 found it). Key trap:
  **touching a previously-UNTESTED file makes it a "changed file"** subject to the floor —
  a pure refactor/extraction into (or an edit of) an uncovered file fails the gate though
  behaviour is unchanged. Editing an uncovered file = add a test for it, or don't touch it.
- **Edited a generated artifact's source → regen + commit the output** (even
  single-service). Touch an i18n catalog, GraphQL schema, snapshot, or other codegen
  input → run the repo's regen step (`generate:types`, `codegen`, …; in
  `package.json` / Makefile), commit the result, or the gate fails on the new
  key/type.
- **Schema migration → author it against a LIVE DB (corgi brings the DB up), never
  hand-write the migration file.** A change to the ORM schema (Prisma / Drizzle /
  TypeORM / knex / Alembic / …) needs a migration **generated + applied against a real
  database** so it's validated — a hand-authored SQL/migration file is unverified and
  breaks CI / deploy. **The DB being down is not a reason to fake it: corgi starts it.**
  `corgi run --services <svc> --with-deps --detach` brings up the service's
  `db_services` (no full stack needed), then run the repo's own migrate command through
  corgi — `corgi exec <svc> --ensure-deps -- <migrate cmd>` (the script that wraps
  `prisma migrate dev` / `drizzle-kit generate` / `knex migrate:make` / `alembic
  revision --autogenerate`). Commit the generated migration **and** the regenerated
  client. `corgi stop` when done.
- **Expo / React Native service → verify on a simulator, not just jest**
  (`references/expo-verification.md`). Detect: `package.json` depends on `expo`
  (or `react-native` + `ios/`/`android/`). Jest-green is not done: Metro
  interop, native modules, permissions/entitlements, and visual layout only
  fail on device. Native-scoped change (new native dep, `app.json`
  plugins/permissions) → rebuild dev client (prebuild → `LANG=en_US.UTF-8 pod
  install` → xcodebuild) and re-install; JS-only → existing build + Metro
  reload. Drive the changed flow with **Maestro** (flows in `e2e/`, committed
  with the PR), confirm with `simctl` screenshots, watch the Metro log for
  runtime errors. Multi-device features (P2P/LAN): clone simulators — they
  share the host's network/Bonjour, so the real radio path is testable. Use the
  `expo:*` plugin skills for SDK-specific guidance when installed. On a
  non-macOS host (no simulator) → fall back to the visual-bug manual-only path:
  spec + PR carry repro steps; say so in the report.
- **Webhook / callback feature** (a new inbound endpoint an external provider calls —
  Stripe, GitHub, Twilio, e-sign…) → **test with a simulated signed payload, not a
  live call:** assert the signature check + handler behaviour against a sample event
  (repeatable, CI-safe). **Don't gate on live delivery** — it needs provider config a
  draft PR can't assume. Put the **live check** in the spec's manual-verification +
  PR body: `corgi tunnel <svc>` for a public URL, point the provider (or its CLI,
  e.g. `stripe listen --forward-to <url>`) at it.
- **Multi-repo consumer:** can't verify (codegen/typecheck) until its producer is
  committed **and running** — do Phase 4's contract-owner-first step (start producer,
  `corgi status --ready`) BEFORE this gate on the consumer. When the consumer's types
  come from **introspecting a running producer** (GraphQL introspection codegen,
  OpenAPI client-gen against a live server), you MUST start the producer with corgi and
  run the real codegen against it — **never hand-edit the generated client/types as a
  shortcut because the producer is down.** A hand-edited generated file drifts from the
  real schema, isn't validated, and the next real `codegen` run silently overwrites it.
  Producer down → bring it up (`corgi run --services <producer> --with-deps --detach`),
  don't fake the output.
- **Stop rule:** can't pass after ~2 honest tries → STOP, leave un-pushed, report
  `needs attention` + failure, rest ships. Never push red.
- **Re-tier mid-flight:** adjustment reveals real design → STOP, bump to feature,
  hand to superpowers. Also **widens span** (another repo or a new contract) → loop
  back to Phase 1–2 — re-spec (add `## Contract`), re-gate, create the producer
  branch — don't escalate in place.

## Phase 3.5 — Per-story review (scoped, right after gate is green)

Review **each story as it finishes**, scoped to **only its diff** — incremental,
bounded context, NOT one giant end-of-batch review (re-reads everything, burns
tokens).

- Review `git -C <branch-dir> diff <base>...HEAD` — a review subagent passed only
  that diff + the spec (works everywhere), or `/code-review` /
  `superpowers:requesting-code-review` if present.
- Fix **blocking** findings (correctness, missing test, scope creep), re-run gate.
  Cap ~1 extra round; still blocked → Stop rule. Non-blocking → PR body.

## Phase 4 — Commit, then multi-repo ordering

**Commit:** match the repo's `git log` style (Conventional prefix only if the repo
does). **Concise subject** + **issue key**; body only if truly needed, never a wall.
**No `Co-authored-by` / AI trailer.** Let pre-commit hooks format; re-stage if
rewritten.

**Record a fact when it'll matter later (confirm first).** A non-obvious bug root
cause that could recur, or a cross-service contract decision, earns a memory fact —
draft, show, write on OK via `corgi memory add …` then `corgi memory index` (see
`memory` skill). After a `fix` fact, run the recurrence check
(`corgi memory list --type fix --json`; a `pattern` ≥ 3× → write a **proposal**, stop —
never auto-install). No `.corgi/memory/` → offer to create; declined → skip.
**Never put a secret in a fact.**

**Multi-service** (one issue → N repos → N PRs):

- **Same branch name** in every repo.
- **Contract owner first.** Consumer regenerates types/clients from a producer
  (GraphQL codegen, OpenAPI, protobuf, shared schema)? Producer must be implemented,
  committed, AND **running** before the consumer verifies:
  ```bash
  corgi run --services <producer> --with-deps --detach   # or: corgi run --detach
  corgi status --ready --service <producer>               # block until healthy
  ```
  Until up, consumer's generated types are stale → won't typecheck. Producer from the
  `depends_on_services`/`exports` graph (Phase 0). `corgi stop` when done. **Producer
  in a worktree?** `corgi run` serves its `path:` (main checkout) by default — add
  `--service-dir <producer>=/tmp/corgi-wt/<wt-id>-<service>` to run the worktree's
  code (Phase 3). Without the flag, implement the producer in place.
- Consumers regenerate, commit generated output, finish their slice.
- **Merge order:** producer PR first, consumers after. State in spec + every PR body.

## Phase 5 — Push + draft PR/MR per repo

Per repo, forge from Phase 0. **`<dir>` = the repo's working dir** — the worktree dir
(`/tmp/corgi-wt/<wt-id>-<service>`) if worktree'd in Phase 3, else the checkout. Run
`gh`/`glab` **from inside that dir** (they read the repo from cwd; `git -C` only sets
git's dir, and the spec `--body-file`/`cat` path is relative to cwd).

**GitHub (`gh`):**

```bash
git -C <dir> push -u origin <branch>
gh pr create --draft --base <base> --head <branch> \
  --title "<subject> [<issue-key>]" --body "<what / how / tests / issue link>"
gh pr comment <n> --body-file docs/stories/<issue-key>-<slug>.md   # spec on PR
# flip ready: gh pr ready <n>   (re-draft: gh pr ready <n> --undo)
```

**GitLab (`glab`):**

```bash
git -C <dir> push -u origin <branch>
glab mr create --draft --source-branch <branch> --target-branch <base> \
  --title "<subject> [<issue-key>]" --description "<what / how / tests / issue link>" --yes
glab mr note create <iid> -m "$(cat docs/stories/<issue-key>-<slug>.md)"   # spec on MR
# flip ready: glab mr update <iid> --ready
```

- **Draft only.** Report each PR/MR's diff summary + link; human flips to ready.
- **Move the ticket to the review state** once its draft PR/MR is up — **resolve,
  don't hardcode:** Linear a `Code Review`/`In Review` state (later `started`-type or
  custom, from `list_issue_statuses`); Jira the transition whose target is named
  _In Review_/_Code Review_ (`getTransitionsForJiraIssue`). **No such state → leave
  In Progress.** Idempotent; skip no-ticket/blocked. Multi-repo → move once **all**
  PRs are open, not per-repo. **Best-effort:** a tracker↔forge automation may treat a
  **draft** PR as _in progress_ and revert this move — the review state only sticks once
  the PR is marked _ready_. Set it once; don't fight a revert.
- **Cross-link** siblings + merge order in each multi-repo PR/MR body.
- **Run-locally line in the body** — the same one-paste
  `corgi run --service-branch <svc>=<branch> … --with-deps` (Grouped report) so a
  reviewer spins the branch up without hunting.
- Canonical spec already on the tracker (Phase 1); PR/MR comment is a convenience
  copy.

### Grouped report (final output)

`<subject>` = the PR/MR title **without** its trailing `[<issue-key>]` (Phase 5 puts
the key in the title — don't print it twice). **No `(draft)` suffix.** One blank line
between stories.

- **Single-repo** → one line `[<issue-key>] <Service>: <subject>`, link directly
  below.
- **Multi-repo** → a **header `[<issue-key>] <story description>`**, then each repo
  on its own `<Service>: <subject>` line with the link below — no key repeated, no
  blank line between repos.
- **No-ticket** → swap `[<issue-key>]` for a short `[<slug>]` tag so the lines still
  group.
- **Run line** → after the link(s), one **copy-paste** `corgi run` spinning up every
  impacted service on its branch via `--service-branch <svc>=<branch>` (corgi builds
  the worktree from the pushed branch — reviewer needs nothing else). Same `<branch>`
  across repos. `--with-deps` so deps/dbs come up. One `--service-branch` per service.
  Skip blocked/failed. Needs the flag (`corgi run --help | grep service-branch`);
  else `git checkout <branch> && corgi run --services <svc>`. Same line for you +
  reviewer — the branch is committed now, no `--service-dir` variant needed.
  (`--service-dir` at the live impl worktree belongs to the Phase 3 gate, code still
  uncommitted.)
- **Review hint** → after the link(s) per actionable (non-blocked) story, one line
  per PR/MR: `↳ review it: /corgi-review <pr-or-mr-link>` — hands the reviewer to the
  `review` skill (checks the diff against repo standards + the ticket, posts inline
  suggestions). Skip blocked/failed.
- **Blocked / failed** → no link, one line:
  `[<key>] <Service>: BLOCKED — <decision needed>` (or `needs attention — <reason>`, +
  the worktree `/tmp` path if partial work is parked there).
- **Review-channel blurb (only when asked)** — user asks for a message for the
  team's review channel → exactly two lines: `<Service>: <short title>` then the
  bare PR/MR link. No pitch, no root-cause paragraph, no emoji, no "please review"
  — the link unfurls; the channel convention is terse.

```
[ABC-123] web: Remove address step from mobile signup
https://github.com/<org>/<repo>/pull/<n>
↳ review it: /corgi-review https://github.com/<org>/<repo>/pull/<n>
▶ corgi run --service-branch web=feature/ABC-123/remove-address-step --with-deps

[ABC-200] Add phone field to user
api: Add phone field to user
https://github.com/<org>/api/pull/<n>
web: Add phone field to user
https://github.com/<org>/web/pull/<n>
↳ review it: /corgi-review https://github.com/<org>/api/pull/<n> https://github.com/<org>/web/pull/<n>
▶ corgi run --with-deps --service-branch api=feature/ABC-200/user-phone --service-branch web=feature/ABC-200/user-phone

[ABC-125] api: BLOCKED — which auth scope gates the endpoint?
```

---

## Scenarios & scaling

- **Big batch → bound context.** >~4–5 stories → dispatch per-branch implementation
  to subagents (`superpowers:subagent-driven-development` /
  `dispatching-parallel-agents`), one per branch, scoped to its spec + the shared
  note. Orchestrator stays gate-keeper, collects reports + reviews. Chunk a huge
  batch.
- **Concurrent same-repo test runs** need a real install per worktree, not the
  symlink (build caches race).
- **One blocked story never blocks the batch** — held aside; actionable ships;
  blocked surface as questions.
- **Mixed forges/trackers in one batch** fine — resolved per repo / per issue
  (Phase 0).
