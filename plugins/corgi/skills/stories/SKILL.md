---
name: stories
description: "Use to ship work across a corgi workspace: a ticket key (ABC-123), a tracker link or several, \"do this story\", \"implement these tickets\"; a bare tracker link means this too. Also a free-text feature (\"add X across the services\") or \"what should I work on\". NOT authoring or running compose, or one-line edits."
---

# Corgi stories

**Done when:** every story in batch has, per touched repo, pushed branch + draft PR/MR whose checks polled to conclusion and whose review loop ran - or BLOCKED line with its one question; then report. Story corgi tracks (sits in `corgi agent watch status`) also gets `corgi agent watch done <KEY>` once work pushed, so inbox rows close on this side; `watch comment` and `watch pr` do that on their own.

Work items - tracker issues (Linear/Jira) **or** free-text feature - → spec each
→ isolated branch(es) → tested + reviewed → **draft** PR/MR per repo → review
loop until clean (Phase 5.5) → grouped report. Services, dirs, dependency order: all from `corgi-compose.yml`. Never
hard-code.

## When the work is an idea page

Request naming page - `/idea/<slug>`, slug, "the spec on the page", `corgi idea …` link - already specified and argued there. Read with
`corgi idea show <slug> --json`: `markdown` = spec, `tickets` = stories
(one branch each; `after` = order; ticket with `ref` already board task
or tracker ticket - use that key), `comments` = decisions, so Phase 2 sign-off already given when no comment is `open`. Work tickets as stories.
Report progress on page, not only chat:
`corgi idea comment <slug> "<what shipped, the PR link>" --as-agent` - story changed plan → keep split true with `corgi_idea_edit`. `idea` skill has rest.

## Speed model

**One blocking gate** on adjustment/bug fast path; complex stories add
superpowers checkpoints.

- **Gate (blocking): spec sign-off** (Phase 2). Confirm intent before any branch.
  Cheap, guards whole batch. Never skip - clear up-front directive collapses
  it to inline diagnosis (Phase 2 fast-path): still confirms intent, no separate
  pause.
- **No final gate.** Draft PR/MR: push, open draft, scoped review, review loop
  until clean (Phase 5.5), report diff + link; human flips to _ready_. Draft = not ready-for-review, no merge; CI still runs
  if repo runs it on drafts.
- **Pre-authorized autonomous run (opt-in).** Blanket approval up front - "I approve
  all changes", "just do it and open the PRs/MRs", "ship it and watch CI" - = standing
  sign-off for whole batch: collapse gate regardless of tier/span (Phase 2),
  open draft PR/MR, **watch CI to green** (Phase 5). Don't re-ask each step.
  Guardrails hold - draft-only, never merge, never force-push, blocked stories
  still surface as questions.
- **Automatic mode (unattended).** Daemon runs this skill itself for ticket
  assigned to user when workspace has `corgi agent watch enable --action fix
  --auto-for tickets` (or `all`); prompt then carries `corgi watch · …` trail
  and "I approve all changes". That = pre-authorized run above + one rule
  from `../_shared/conventions.md`: **never ask** - every choice takes recommended
  option, named in one line; story that cannot go on ends in `corgi agent handoff
  --ref <key> --blocked "<why>"`, not question. Run still posts spec
  comment, opens draft PRs/MRs, runs review loop, reports; `corgi agent today`,
  inbox, kanban show it same as any run. Turning on = person's
  choice per workspace, never this skill's.

### Story tiers - set per story (Phase 1), drives rigor

| Tier           | What                                                              | Extra rigor                                                          |
| -------------- | ----------------------------------------------------------------- | -------------------------------------------------------------------- |
| **Adjustment** | clear-spec UI/copy/flag/config; unambiguous                       | test for new behaviour only                                          |
| **Bug**        | broken / regressed                                                | regression test **FAILS on base branch** before fix, passes after    |
| **Feature**    | new behaviour, real design, or new/changed cross-service contract | hand to **superpowers** if installed, else equivalent inline (below) |

**Tier ≠ span.** Complexity axis vs single/multi-service (Phase 4). Multi-service change = **Feature** whatever size: contract crosses boundary, design lives there.

**How the tier is picked.** `--mode bug|feature|adjustment` in invocation
wins. Else ticket's own words: type or labels `bug`, `defect`, `regression`,
`incident`, `hotfix` → **Bug**. Everything else **Feature** until Phase 1
**proves** adjustment: one service, surface nameable in one
sentence before reading code, no new behaviour, no contract. Proof = investigation, never ticket brevity - one-line ticket for "premium
frequency" can be feature across four repos. Unsure → Feature rigor costs one
plan; adjustment that was feature costs rewrite.

**Risk lane - always a plan, whatever the size.** Change touching
migrations, auth, sessions, permissions, payments, billing, secrets or
cross-service contract gets Feature rigor even at three lines: interface or schema change written and confirmed first (Phase 2 gate never
collapses for it), spec names rollback. Workspace may extend
list with `riskPaths:` under its entry in user agent config.

**Bug lane - logs first, theories second.** Before hypothesis: reproduce
against running stack (`corgi run`, `corgi logs <svc> --errors-only`,
`corgi_wait_for_log`), quote failing line or request, write
regression test that fails on `<base>`, then smallest fix. No plan file, no
subagent fan-out: bug = straight line from log to test. Quoted
line travels with fix - spec comment, PR body, report each
carry it verbatim, not pointer to where it lives.

**Express lane - small-surface adjustment.** Only proven adjustment (above) in
single service, no cross-service contract, surface nameable up front
(one component or few files: asset/copy/flag/style swap, small wiring), takes
lighter machinery. Open design question (which variant? what scope?) takes it
out of express lane: feature question, feature path.
Lighter steps, same guardrails:

- **No `Explore` subagent** - grep + read 2-3 files inline; subagent returns
  full report to map one component, orchestrator's own search cheaper. Reserve
  `Explore` for unclear surface or real fan-out.
- **No local `docs/stories/*.md`** - `## Spec` tracker comment IS spec, don't
  write both (multi-service/feature still gets file).
- **Right-size proof** - gate once (typecheck/lint/test) + single proof change
  needs (visual swap → serve check, or one screenshot you read when looking is
  quickest way to know). Don't stack every proof.

Still mandatory: gate sign-off, spec comment (QA section folded in), branch, per-story review, draft
PR, report. Unsure on scope → full path; express lane for change
whose scope never in doubt.

**Bug sub-type - logic vs visual.** "FAILS on base" assumes unit test can _see_
it. **Visual/layout bug** (z-index/stacking, overflow, position, breakpoint, CSS
specificity/cascade) → jsdom can't catch it; green unit test proves nothing.
Don't fake one.

- **Repo has visual/e2e harness** (Playwright, Cypress, Storybook visual diff;
  Maestro for Expo/RN - `references/expo-verification.md`) → red check **there**,
  same FAILS-on-base.
- **None** → **manual-only legit, not skip.** Spec + PR body carry repro steps;
  report says manually verified, no auto guard. Still include **QA "what to
  test" section** in spec comment - human must re-check visual. Screenshots
  follow Phase 3's screenshot bullet (user asked, or looking is quickest way
  to see fix) - not standing requirement of this path.
  - **Expo/RN service on macOS host → not manual-only:** drive simulator
    with argent MCP or Maestro even without committed harness and look at
    screen fix touches (`references/expo-verification.md`, Phase 3's
    screenshot bullet).

**Before/after asked for → run `before-after`.** Any phrasing of "screenshot
comparison", "before and after", "show the visual diff", "prove the UI changed" = request for **`before-after`** skill, not second after-only screenshot: builds
base branch too, captures same screen twice, puts both where user
asked - PR body when they said PR, else ticket (§6a). Costs two builds, so
runs **only when user asks** - never own initiative, however
visual change; gate + repo's harness = default proof. Never satisfy
request by describing old state from diff; diff exactly what does
not show it.

### Complex story → superpowers

Bigger than adjustment (real design, unclear approach, large surface, new contract)
→ don't force one-shot. **superpowers installed**, via `Skill`:

- `superpowers:brainstorming` - settle intent + approach before code.
- `superpowers:writing-plans` - becomes spec doc.
- `superpowers:test-driven-development` + `superpowers:executing-plans` - build,
  tests first.
- `superpowers:verification-before-completion` - prove before draft PR.

Not installed → equivalent inline: settle approach with user, plan into spec, tests
first, verify before push. Either way flows back here - same spec doc, one gate,
per-story review, draft PR, grouped report.

## Guardrails (non-negotiable)

- Read `../_shared/conventions.md` first (attribution, `manualRun`, preflight, worktrees).
- **Draft PRs/MRs only.** Never merge, never force-push. PR leaves draft only through
  Phase 5.6, only when workspace CLAUDE.md asks.
- **One blocking gate** - spec sign-off (Phase 2); sign-off _is_ branch
  authorization.
- **No destructive git without explicit OK** - checkout off dirty tree, branch
  deletes, force-push, pushing shared branches.
- **Don't push workspace meta repo** unless asked - only service branches.
- **Code as `../_shared/conventions.md` › Code you write says.** No comments, names
  carry meaning, smallest change that does job, nothing speculative.
- **Read repo's own agent docs before running anything in it.** Service's
  CLAUDE.md/AGENTS.md may forbid specific commands (dev server that breaks build,
  script that mutates state). Triage from forbidden command = noise -
  rerun documented way before believing it.
- **Don't spam ticket.** One comment per story (spec, QA section folded in),
  updated in place on re-runs - never pile of new comments.

## Optional tooling (degrade gracefully)

Stands alone. Used if present, never required:

- **`superpowers:*`** (separate plugin) - complex-story engine + nicest review.
  Missing → inline. Don't block on missing plugin.
- **`expo:*`** (separate plugin) - Expo/RN service: its skills
  (`expo:expo-dev-client`, `expo:building-native-ui`, …) deepen simulator
  verification in `references/expo-verification.md`. Missing → reference
  alone suffices.
- **Code-review command** (e.g. `/code-review`) - Phase 3.5 if present; else
  review subagent works everywhere.

Always available, all flow needs: `git`, `gh`/`glab`, `corgi`, `Explore`/`Task`
agents, tracker MCP.

---

## Phase 0 - Read workspace from corgi-compose.yml

1. Locate compose file (preflight in `../_shared/conventions.md`). None →
   `/corgi-new` first, or ask which repos; don't guess layout.
   **Pasted context may not be this workspace.** Diff, MR body, ticket or
   screenshot in prompt can come from other product entirely - may be *example of shape wanted*, not target. Never identify
   workspace by hunting filesystem for code matching paste: confirm
   which one before Phase 1, re-confirm when paste names services
   compose file doesn't.
2. **Read yaml, extract only needed keys** -
   `services.<name>.{path,cloneFrom,manualRun}`, `depends_on_services`, `exports`
   (schema: `skills/corgi/references/yml-schema.md`). Don't render whole
   project: `/corgi-describe` too many tokens; `corgi --describe` dumps JSON then
   still runs command. Build:
   - **Service → dir map.** `path:` (local) or `cloneFrom:` (clone target) = repo you branch in. `cloneFrom` not on disk → `corgi init` clones first.
   - **Dependency/order graph** from `depends_on_services` + `exports`/
     `${producer.VAR}`. Depended-on service (schema/contract owner) first; consumers
     follow. Cycles → flag.
   - **manualRun set** → exclude (Guardrails).
3. **Per repo: forge, base, commands.**
   - Forge and `<base>`: per `conventions.md` (base used for branch, red
     test, PR target).
   - Test/typecheck/lint/build: discover from `package.json` scripts, `Makefile`,
     `pyproject`/`go.mod`, service `start`/`beforeStart`/`scripts`. Don't assume
     runner. **Also find CI gate PR will face** - coverage-threshold
     script (`test:cov:check`, `check-coverage` step, `--coverage` floor) or CI
     workflow (`.github/workflows`, `.gitlab-ci.yml`). Note it; Phase 3 runs same
     gate before PR, not only scoped test.
4. **Detect tracker.** Read `../_shared/tracker-mcp.md` first.

## Phase 1 - Investigate (once), then spec

**Route the intake first - what am I building?**

- **Explicit tickets** (keys/links) or **free-text feature** → continue below.
  Link IS ticket: `linear.app/<org>/issue/ABC-123/…` and
  `<site>.atlassian.net/browse/ABC-123` both carry key - take from URL,
  carry on, never ask which ticket meant. Several links or keys in one
  message = **one batch**, not one run each: specced together.
- **"Find/pick what to work on"**, nothing named → **don't guess tickets.** Resolve
  via `tracker` skill's **pickup** - `agent` queue (label `agent`, not In
  Progress/Done), drift-skipped - confirm picks, build here. Same selection as
  `/corgi-queue`: selection in `tracker`, building here.

**Tracker issue:** fetch, **view screenshots**, read real code paths.

- Fetch **with relations** - Linear `get_issue` `includeRelations: true`; Jira issue
  links. `duplicateOf` / `relatedTo` / parent epic often already carries or shipped
  work - see _Already shipped?_ below.
- Screenshots - **visual/QA bug from text alone = guess. Get image.**
  - **Linear** = `curl` the `uploads.linear.app` URL (signed, expires ~5 min -
    re-fetch issue for fresh URL), read.
  - **Jira** = `getJiraIssue` gives attachment **metadata only** (filename +
    `content` URL `/rest/api/3/attachment/content/<id>`), no bytes.
    **`mcp__atlassian__fetch` won't get bytes** - ARIs only; attachment id
    mis-resolves to wrong issue. No MCP tool returns bytes. Jira URL needs
    auth header you don't hold. So: 1. Have creds (`$JIRA_EMAIL`/`$JIRA_API_TOKEN`)?
    `curl -u "$JIRA_EMAIL:$JIRA_API_TOKEN" -L -o /tmp/<name> "<content-url>"`, read.
    Usually not in env. 2. No creds / 401 / 403 → **stop guessing. Ask user to
    download + share path** (`~/Downloads`), `ls -t ~/Downloads | head`, read.
- **Local / user-pointed design assets.** User names folder/file
  (`design-screenshots/`, Figma export on disk, "see mockup") → read direct,
  outranks prose. **Build as drawn** - exact icon/emoji, colour, copy, spacing,
  placement. No near-equivalent.
- **Design reference = verify target, not just spec input.** Any visual
  reference (screenshot, mockup, attachment, design-tool link, or **reference
  app/prototype repo** - e.g. AI-generated), **wherever it surfaces** -
  ticket, comments, prior spec/docs, chat, disk - is what Phase 3's visual
  compare checks implementation against; record path/URL in spec.
  Link with no image on disk → fetch/export (design-tool MCP if connected,
  else ask user). Reference = runnable app/repo → run it, screenshot
  relevant screens (capture becomes target image), read styles/components direct for exact values. Don't build UI story from prose
  while design exists unseen. **No design, or user waives → fine:** build from
  prose, verify functionally, note "no design reference" in spec/PR - compare only applies when reference exists, never blocks story without one.

### Reuse an existing spec - then re-verify it (may be stale)

Before speccing from scratch, check **three** places prior spec may live - human's, past `stories` run's, `decompose` ticket's acceptance criteria:

1. **Ticket description** - body often carries intended approach.
2. **Ticket comments** - list them (Linear `list_comments`; Jira via `getJiraIssue`
   / dev panel). Comment whose first heading is `## Spec` = prior run's spec.
3. **Local `docs/`** - `docs/stories/<issue-key>-*.md` (this skill's output) + any
   hand-written design doc whose name/heading matches.

Found one → **starting hypothesis, not ground truth.** Re-resolve every `file:line`
against current tree, re-confirm contract - code moves, specs rot. Say what
drifted, rewrite stale parts, keep what holds. Nothing found → spec from scratch.

**Bug tier - check for dead prior fix first.** `git blame` / `git log -S"<symptom>"`
suspect lines. Prior fix present but bug persists → inert (overridden, lost
specificity, runtime style wins, wrong selector, behind flag). Explain _why_ dead, revive/correct - don't stack second half-fix. Note why in root-cause.
Re-opened ticket whose prior fix already **merged**? Read prior PR/MR state
(`gh pr view <n> --json state` / `glab mr view`): MERGED + closed → branch
dead (often deleted), fix already on `<base>` - branch **fresh off
`<base>`**, never reopen/continue merged branch; old `## Spec` comment = insufficient fix, so update that comment, don't follow it.

### Already shipped? - verify before spec or branch

Reuse-spec check (above) finds prior _spec_; this finds _deliverable already
built on `<base>`_ - by **sibling / related / duplicate ticket**, or earlier PR
that bundled it. Re-build wastes batch + opens dup PR. Before speccing **actionable** story, prove NOT already done:

- **Read relations** (fetched above). Open any `duplicateOf` / `relatedTo` / parent
  epic - description or **merged PR** often already covers ask.
- **Grep `<base>` for what story adds** - component, copy string, route, flag:
  `git -C <dir> log --oneline -S"<symbol>" -- <area>`, `git grep "<copy>" origin/<base>`.
  Present + wired → shipped.
- **Open real file to confirm** - not `Explore` summary. Explore hands
  code already satisfying story; "found it" ≠ "needs building."

Shipped → **do NOT branch.** Mark `Status: ALREADY DONE`, comment lineage
(commit/PR that delivered it) + recommend close as duplicate, report no-op.
**Comment ≠ close** - ask before changing ticket state. Phase 1 finding → surfaces
_at the gate_, not after wasted branch.

### Free-text feature (no ticket) - locate work first

Description, not links → no fetch, nothing says _where_ code goes. (First check
`docs/` for existing design doc - _Reuse an existing spec_.) Find target
service(s) before speccing:

1. **Map intent → service(s)** from `corgi-compose.yml` (names, paths,
   `depends_on_services`) + **README next to compose** + per-service READMEs.
   Don't guess.
2. **Confirm with `Explore`** scoped to candidate service(s) - find real files.
3. Genuinely ambiguous → spec-gate question (ask, or `superpowers:brainstorming`);
   don't guess.

Described feature = usually **Feature tier**: `superpowers:brainstorming` (or inline
Q&A) to settle scope → `superpowers:writing-plans` for spec. After sign-off
(Phase 2), **offer to create tracker issue** (Linear `mcp__linear-server__save_issue`
with `title`+`team` and no `id` / Jira `mcp__atlassian__createJiraIssue`) for key +
auto-link; declined → spec stays
local + on PR, branch drops key segment (Phase 3). **Caller (e.g. `suggest`)
that already created issue + hands key + spec → use that key, don't re-create.**

### Investigate once - don't re-research

Batched stories overlap. Re-exploring per story doubles tokens. So:

1. **Cluster** by **service + area** before dispatching.
2. **One `Explore` sweep per area, not per story** - all area's questions in
   one agent. Never per-story over same files. (Micro adjustment → no subagent at
   all; grep inline - _Express lane_.)
3. **Orchestrator = the cache.** Subagents can't share context mid-flight: scope
   sweeps to not overlap, collect each into one **investigation note** (scratch -
   memory or gitignored file), specs reference it.
4. **Reuse ledger** - shared components/contracts recorded once; stories cite, don't
   re-derive.
5. **Need runtime/deployed data** (staging/prod error, request trace, logs not
   available locally)? Invoke **`debug`** skill (Step 4 - provider data), fold findings
   into note; don't hand off whole flow.
6. **Workspace memory (read)** - `.corgi/memory/` exists → read first
   (`corgi memory list --json` / `index.md`, then matching facts; see `memory` skill):
   honor
   `decision` constraint, reuse `incident` fix for regression, ground
   free-text feature in `domain` facts. Absent → skip.
7. **Story = conformance to EXTERNAL contract** (platform integration
   requirements, protocol revision, third-party API)? Read vendor's
   current docs before speccing. Repo shows only what was implemented, never
   what other side requires today - **limits** (payload caps,
   timeouts, supported versions) bind hard as endpoints, fail
   only on real data. Cite doc in spec.

### Forecast the risk - every story

Before spec, run `risk` skill on **story** (ticket text + code area it
names): forecast, `confidence: low`, never auto-approve. Decides two things:
which **tier** when ticket alone unclear (forecast 7+ never adjustment), and what goes in spec's **Before building** list - decisions human makes at Phase 2 gate (flag, migration strategy, rollout,
which service owns data). Put forecast line in spec comment so sign-off
made knowing it; real score comes from diff at Phase 5.

### Write the spec - every story

`docs/stories/<issue-key>-<slug>.md`, actionable or not - except single-service
bug (_Bug lane_) or express adjustment (_Express lane_): no file, `## Spec`
comment is spec. Tracker unreachable → spec goes into report and
PR body, never into repo file standing in for comment:

- Problem (quote issue) + **which services** (drives branch/PR count).
- **Tier** - adjustment/bug/feature.
- Root cause / current behaviour, `file:line` refs.
- Change plan (snippets) **grouped by service**, tests, manual verification, risks.
  Risks name kinds from `../_shared/second-reviewer.md` that apply, each with
  one-line answer (`check then write: one transaction, conditional update`;
  `installed apps: still read items[0] - kept`).
  Name what plan deliberately does **not** build and what covers it instead (native element, existing helper) - cheapest spec has fewest
  new parts. Multi-service: `## Contract` + cross-service order.

### Triage: actionable vs blocked - controls POSTING, not writing

- **Actionable → ONE comment on issue** (human-readable, not `.md` attachment).
  **Open with `## Spec` heading**; when change user-testable, end with
  short `## What to test` section (plain QA: clicks + outcome, no code/file refs, end
  `Expected:`) - folded into **same** comment, not second. No HTML comment
  marker (trackers render `<!-- … -->` as visible text), no footer badge. Linear
  `mcp__linear-server__save_comment({ issueId, body })` (omit `id` to create, pass `id`
  to update); Jira `mcp__atlassian__addCommentToJiraIssue`. Literal newlines/markdown.
  Picture, video or file goes through `corgi agent watch comment <KEY> "<text>" --file <file>`
  (`../_shared/conventions.md` § Comments people read).
  `## Spec` heading = how later run (new session, no comment id) finds it: list
  comments, match one whose first heading is `## Spec`, **update**
  (`save_comment({ id, body })`), don't duplicate. Skip QA section for
  non-testable stories.
- **Blocked → do NOT post.** Spec local only; mark `Status: BLOCKED` + **Decision
  needed**; surface choice. Hold it; rest of batch proceeds.
- **Env-gated dependency = blocked, not guessed.** Registry behind VPN/auth,
  build needing creds only human holds - mark blocked naming
  exactly what's missing; never fake verification or report as run.

`superpowers:brainstorming` / `superpowers:systematic-debugging` (if installed) to
resolve ambiguity before blocking.

## Phase 2 - Gate: spec sign-off (the one blocking gate)

Present all actionable specs in **one round**; sign-off before any branch -
batch-level, not per-branch. Re-present only changed specs. Blocked held out. Open
questions inside spec use `align` skill's fork format (`❓ Qn` + `➡️` pick, ≤5 per
spec); story with more forks not ready to gate.
Superpowers-escalated stories pass here too: `writing-plans` output = spec.

**Fast-path - collapse the approval round to inline diagnosis.** Gate confirms
intent before branching; collapses (state diagnosis + exact change inline, then
go - that _is_ gate, user can still stop you) in exactly two cases:

- **One unambiguous item:** one ticket or one named fix, tier adjustment or bug, single
  service, no cross-service contract, exact change statable in one
  sentence with no open question. Anything else - feature tier, >1 story,
  multi-service, ambiguous target, clarifying question you had to ask yourself -
  = full gate. Vague "just fix it" with no scope = open intent, not clearance.
- **Blanket pre-authorization:** user explicitly pre-approved changes
  sight-unseen and told you proceed to PR/MR ("I approve all changes", "do them all
  and open the MRs"), or run unattended (`../_shared/conventions.md` - the
  `corgi watch ·` trail). That = sign-off for any tier, span, batch size; state
  each spec inline and go. Signal = explicit approval + directive, not
  impatience.

**Workspace says wait.** `corgi_watch_switches` (or `corgi agent watch`) may carry
`planReview: always | risk>=N` for this workspace (`corgi agent watch enable --plan-review`).
Matches story's forecast → neither fast-path applies: post spec, ask
sign-off with `AskUserQuestion` so phone can answer, cut no branch
until it returns. Unset or `off` (default) changes nothing above.

Either way fast-path drops pause - never thinking, not artifacts:
post spec comment (with QA section) even for one-liner. Durable
record; visual/QA defect is what human must re-verify.

## Phase 3 - Branch + implement + verify per story

**One at a time by default.** Stories run in order on this thread, no subagent dispatch, unless
person asked for parallel work in this conversation. "No agents", "one at a time" stand for
rest of thread. When asked, read load (`uptime`) before fanning out: more than one full test gate at a time only while
load stays below core count, never more worktrees than stories. Subagent cut
short by usage limit or 429 → resume with same brief once limit over -
re-read clock before calling anything "still rate-limited"; limit message
carries reset time, passed reset = over.

**Local side effects are reported, never silent.** QA pointing service at other
database, editing `.env`, stopping container or killing process says so when done, restores after; report lists what changed on machine and
that it was restored.

Branch: `feature/<issue-key>/<kebab-slug>`, same name in every affected repo.

**Write the scope before the first edit.** Spec named files or areas
that change and what done means; put on record so hooks hold you
to it:

```bash
corgi agent scope set <issue-key> --path "api/limits/**" --path "web/src/limits/**" \
  --lines <diff budget> --tests <new test files> --done "<criterion>" --done "…"
```

Paths = workspace-relative globs (`**` crosses directories; directory
covers contents). Scope = **tripwire for wandered run, not target to build down to**: name every area spec touches, set line
budget at three times spec's guess (adjustment ≈ 200, bug ≈ 400, feature ≈
1200 per service), tests one file per acceptance criterion, core flow first.
Write outside paths refused with way to widen - `corgi agent scope
add <key> --path …` - widening normal the moment
code needs it; say why in one line of PR. Diff over budget reported
once when turn ends: raise budget on record (`scope set --lines N`)
with one line why. **Never trim real work, tests or error handling to
fit number**, never widen silently, never disable hook.

**Get `<issue-key>` from the tracker, don't invent it** (auto-link token):

- **Linear** - `get_issue` → `identifier` (`ABC-123`) + suggested `gitBranchName`.
  Use `identifier`; Linear links any branch containing it (case-insensitive). (Or
  `gitBranchName` verbatim.)
- **Jira** - `getJiraIssue` → `key` (`PROJ-123`). Dev panel / Smart Commits link by
  that token.
- **No ticket** - `feature/<kebab-slug>`, no key segment (or key of issue
  created in Phase 1).

Key also in commit + PR/MR title (Phases 4-5). Same branch name across repos so
multi-repo PRs group.

**Move the ticket to in-progress + assign when work starts.** As each actionable,
ticketed story's branch created (post sign-off):

- **Transition to team's started state** - **resolve, don't hardcode "In
  Progress":** Linear `save_issue({ id, state })` to `started`-type state (`state`
  takes type/name/id; resolve via `list_issue_statuses`);
  Jira `transitionJiraIssue` to transition targeting In-Progress
  (`mcp__atlassian__getTransitionsForJiraIssue`).
- **Assign to mover** - current tracker user: Linear `save_issue({ id, assignee: "me" })`
  (`assignee` accepts user id/name/email/`"me"`); Jira `editJiraIssue` assignee = current user (`mcp__atlassian__atlassianUserInfo`).
  **Don't steal** - assigned to someone else → leave, note who; unassigned → take.

Idempotent - skip if already set; skip no-ticket + blocked. In-progress move
also stops looping `/corgi-queue` re-grabbing story in flight (auto-pick takes
only not-In-Progress). **Review** transition fires later, at draft PR - Phase 5.

**Pick branch vs worktree per repo - check the working tree first:**
`git -C <dir> status --porcelain --untracked-files=no` - empty = clean, any output =
dirty. (Ignore stray untracked; `checkout -b` doesn't disturb them.)

| Repo state | Stories touching this repo | Mode                                                                                                  |
| ---------- | -------------------------- | ----------------------------------------------------------------------------------------------------- |
| **clean**  | one                        | **branch in place**                                                                                   |
| **dirty**  | one                        | **worktree** - don't disturb user's uncommitted work, skip destructive base checkout on dirty tree    |
| any        | several                    | **worktree per story** (parallel isolation)                                                           |

Count "stories touching this repo" **across whole batch up front** - two
single-repo stories hitting one repo = "several" → both worktree.

**Dirty + overlap guard.** Worktree gate in `../_shared/conventions.md` applies:
edits overlapping story's files → STOP and ask; no overlap → worktree safe.

**Must-run producer in a worktree → run it with `--service-dir`.** Producer that
must be _running_ for consumer to verify (Phase 4) can live in worktree:
`corgi run --service-dir <producer>=/tmp/corgi-wt/<wt-id>-<service>` (below).

- **Branch in place** (clean tree). Branch straight off fetched remote base - no
  `checkout <base>`/`pull` dance, no local-divergence trap:
  `git -C <dir> fetch origin && git -C <dir> checkout -b <branch> origin/<base>`.
- **Worktree** (dirty tree, or several stories in one repo). Path
  `/tmp/corgi-wt/<wt-id>-<service>` - `<wt-id>` = `<issue-key>` (or `<kebab-slug>`
  for no-ticket), `<service>` = service name, so multi-repo story's repos never
  collide. Branch off `origin/<base>` - never touches `<dir>`'s tree:
  ```bash
  git -C <dir> fetch origin
  git -C <dir> worktree prune                                # drop stale entries
  W=/tmp/corgi-wt/<wt-id>-<service>
  [ -e "$W" ] && W="$W-$(date +%s)"                          # a leftover (re-run/crash): new path, never rm -rf
  git -C <dir> worktree add -b <branch> "$W" origin/<base>
  # deps dir (node_modules / vendor / target / .venv) gitignored → symlink main
  # checkout's for SEQUENTIAL gate runs; real install for CONCURRENT runs and for
  # any worktree corgi will run (--service-dir, below).
  ln -s "$PWD/<dir>/node_modules" "$W/node_modules"
  ```
  `rm -rf` denied in hardened workspace and by auto mode, so leftover dir
  never deleted - next path taken, `/tmp` is OS's to clean. Use
  path you got (`$W`) for every later step of this repo.
  **Worktree corgi will run gets real install, never symlink.**
  `--service-dir` (below) runs service's `beforeStart` in worktree, and
  install step rebuilding deps dir (`npm ci`, clean reinstall) follows
  symlink and empties main checkout's deps. Symlink for gate runs you start
  yourself; before `corgi run --service-dir`, `unlink` it and install for real. Main
  checkout's deps dir suddenly empty → that's what happened: reinstall there,
  report as local side effect.
  Worktree dir now this repo's **working dir** - implement, gate, review,
  commit, push, open PR/MR from it (Phases 3.5-5).
  - **Run a worktree'd service with `--service-dir` (only services in
    `corgi-compose.yml`).** corgi resolves service from `path:` (main `<dir>`);
    to _run_ worktree's code - e.g. producer consumer verifies against
    (Phase 4) - pass `--service-dir <svc>=/tmp/corgi-wt/<wt-id>-<svc>`; corgi runs
    service's env, beforeStart/afterStart, process from worktree, main
    checkout untouched. Per-service, repeatable - mix worktree + compose `path:`
    services:
    ```bash
    corgi run --detach \
      --service-dir api=/tmp/corgi-wt/ABC-200-api \
      --service-dir web=/tmp/corgi-wt/ABC-200-web
    # services not named (admin, worker, db_services) run from their compose path:
    ```
    `beforeStart` steps run from worktree too, so step leaving
    service dir (`cd .. && make …`, `../scripts/…`) lands in worktree's parent, fails.
    `corgi run --detach` still starts process, then exits non-zero with
    `beforeStart failed for <svc>` - stack keeps running without what step
    made (seeds, generated files, policies). Treat exit as real failure, not
    healthy boot: run failed step from compose dir by hand, name it in
    report. `corgi status` shows service ❌, `corgi why <svc>` says
    `setup_failed`. Lasting fix in compose file: step needing
    compose folder says `cd "$CORGI_COMPOSE_DIR"` (corgi 2.35.4+), not `cd ..` -
    offer as follow-up, never fold into story's diff.
    `--service-dir` runs **exact** worktree code. (corgi also has
    `--service-branch <svc>=<branch>` - own reused worktree off branch - and
    `--service-checkout <svc>=<branch>` for in-place checkout; handy for ad-hoc
    "run this branch", but for stories point at impl worktree.) Branched repo
    **not** corgi service → no `--service-dir`; run its runner in
    worktree dir.
  - **Success →** `git -C <dir> worktree remove /tmp/corgi-wt/<wt-id>-<service>` once
    PR up. **Failure (Stop rule) →** leave it; report `/tmp` path. Never
    `worktree remove` failed story.

Implement to spec, and **before each piece, walk ladder in
`references/smallest-change.md`** - not needed → already in repo → language
ships it → platform ships it → installed dependency does it → one clear line →
least code passing check. Stop at first rung that holds. **Minimum
diff - no opportunistic refactor, no abstraction nobody asked for, no new dependency
for what installed one covers, no code comments** unless file already comments
heavily. Validation at trust boundaries, data-loss error handling, security,
accessibility, tier's test never trimmed. Shortcut with known
ceiling goes in PR body's `Deferred` list (Phase 5), not comment. Run
**per-service gate** (tests + typecheck + lint) BEFORE commit. Tests for every change,
matching existing patterns.

- **Run gate through corgi when service in `corgi-compose.yml`** - gives
  worktree full resolved env, deps, cwd, no guessing runner or
  hand-building env:
  - `test` script → `corgi test --changed --base <base>` runs whole test script of
    every repo differing from `<base>` (uncommitted work counts); add
    `--service-dir <svc>=<worktree-dir>` for worktree'd services. Reports nothing
    changed → fall back to `corgi test --service <svc>` (in place, or with same
    `--service-dir` mapping).
  - Other command (typecheck/lint/migrate/one-off) →
    `corgi exec <svc> --service-dir <svc>=<worktree-dir> --ensure-deps -- <cmd>`.
  - Not in compose, no `test` script, or no compose → run discovered runner
    (Phase 0) in worktree dir. Same `--service-dir <svc>=/tmp/corgi-wt/<wt-id>-<svc>`
    mapping as `corgi run` (Phase 3); drop for in-place.
- **Bug tier: red test first** - write, confirm **FAILS on base**, make
  pass. Adjustments skip.
- **New code lowers complexity, never raises it.** Before per-service gate, run
  `complexity` skill in `gate` mode on diff (touched functions vs `<base>`): function started at or under repo's threshold ends at or under, one
  started over does not rise, new function starts under. `fail` → apply
  skill's tactics to function, re-gate. Review loop (Phase 5.5) checks
  same rule, so regression caught here = one round saved there.
- **Pre-existing red baseline.** Repo typecheck/lint may already fail on `<base>`,
  unrelated. Don't chase whole-repo green that never existed; don't let baseline
  noise hide your breakage. Gate on **no NEW errors** - filter run to changed
  files, or diff base error set. Touched files clean; baseline left as-is, not
  "fixed" (scope creep).
- **Run what CI runs, not cherry-picked subset.** `corgi test --changed --base <base>`
  runs each changed repo's whole `test` script in resolved env (`--service <svc>`
  when `--changed` finds nothing), so suites importing changed module load
  too. Then run CI gate Phase 0 found: coverage floor over changed files counts
  untested file you merely touched - add test or leave it alone. Green
  exit proves nothing unless intended suites ran: check test count above
  zero (path filter containing `(…)` or `[…]` can match nothing and still pass).
- **Edited generated artifact's source → regen + commit output** (even
  single-service). Touch i18n catalog, GraphQL schema, snapshot, or other codegen
  input → run repo's regen step (`generate:types`, `codegen`, …; in
  `package.json` / Makefile), commit result, or gate fails on new
  key/type.
- **Schema migration → author against LIVE DB (corgi brings DB up), never
  hand-write migration file.** ORM schema change (Prisma / Drizzle /
  TypeORM / knex / Alembic / …) needs migration **generated + applied against real
  database** so validated - hand-authored SQL/migration file unverified,
  breaks CI / deploy. **DB down no reason to fake it: corgi starts it.**
  `corgi run --services <svc> --with-deps --detach` brings up service's
  `db_services` (no full stack needed), then run repo's own migrate command through
  corgi - `corgi exec <svc> --ensure-deps -- <migrate cmd>` (script wrapping
  `prisma migrate dev` / `drizzle-kit generate` / `knex migrate:make` / `alembic
  revision --autogenerate`). Commit generated migration **and** regenerated
  client. `corgi stop` when done.
- **Screenshots - tool you reach for, not box to tick.** Decide once:
  1. **User asked** ("with screenshots", "attach screens", "show me how it looks")
     → capture, attach to **ticket** (Linear/Jira, through tracker's own
     upload - §6a); where asked-for screenshot goes when no place named.
     Put in **PR body when user said PR** ("screenshots in the PR",
     "include them in the PR"), both when asked for both.
  2. **Nobody asked, but look helps you test** (layout fix jsdom can't see,
     native control, design reference to compare, Expo flow driven on
     sim) → take it, **read it**, verify - what drivers below are for.
     Keep as working file by default, say "verified on <device/browser>" in
     report; shot genuinely helps reviewer (shows one thing diff can't) → attaching fine - judgement call, not requirement.
  3. **Neither** → nothing to capture, no apology. Api-only, config, CI,
     test-only, most logic changes have nothing to show; "a screen changed" not by itself reason to shoot every state.
  Don't shoot terminal, don't stack screens to look thorough. When capturing,
  pick driver in order, first present wins:
  1. **argent MCP** (`mcp__argent__*` tools in tool list; `argent --version` on
     PATH) - iOS simulator, Android emulator, or Chromium page over CDP (web
     service in browser started with `--remote-debugging-port`): `list-devices`,
     `launch-app` / `open-url`, `describe` for tap coordinates, `gesture-tap`,
     `screenshot`. Every action returns screen after it, so no flow file, no
     guessed coordinates. Read argent rule/skill it ships before first call.
  2. **Maestro** (`mobile` skill; its MCP when connected) - flow file with
     `takeScreenshot` inside flow, `--device <udid>`.
  3. **Repo's web harness** (Playwright / Cypress screenshot) for browser
     surface with no argent; else headless browser (`npx playwright screenshot`,
     `chrome --headless --screenshot`).
  4. **Bare capture** last - `xcrun simctl io <udid> screenshot`, `adb exec-out
     screencap -p`.
  Whatever captured: log in as account class story is about, seed
  state ticket describes, **read each image** before attaching or concluding.
  **Asked-for** set (trigger 1) complete: every state spec names (empty /
  in progress / done / error) and every rendered side of multi-service story - web or admin console showing same record gets shot too. **Verification** shot (trigger 2) covers one spot change touches, nothing
  more. Name each file `<n>-<what>.png`. Attaching: ticket through tracker's own
  upload, PR per Phase 5 (§6a link form). No driver, no device (host with no
  simulator or browser) → repro steps in spec and PR instead; say so in
  report when user asked for shots.
- **Ticket links design (Figma / mockup) → story done at READ side-by-side**,
  not green tests. Pull frames into `docs/design/<ticket>/`, capture same states
  from app, compose labelled design-vs-app images, put deviation table (fixed vs
  deliberate divergence) in PR/MR - table = record; composed images
  go with it per screenshot bullet above (ticket when user asked for
  screenshots, PR when they said PR, else your call). Feature still behind flag or without seeded data
  forced on with temporary, clearly-marked local switch - reverted before commit. Full
  loop: `design-parity` skill.
- **Expo / React Native service → verify on simulator, not just jest**
  (`references/expo-verification.md`). Detect: `package.json` depends on `expo`
  (or `react-native` + `ios/`/`android/`). Jest-green not done: Metro
  interop, native modules, permissions/entitlements, visual layout only
  fail on device. Native-scoped change (new native dep, `app.json`
  plugins/permissions) → rebuild dev client (prebuild → `LANG=en_US.UTF-8 pod
  install` → xcodebuild), re-install; JS-only → existing build + Metro
  reload. Drive changed flow with driver screenshot bullet picked -
  argent MCP when present, else **Maestro** (flows in `e2e/`, committed with
  PR) - confirm by reading screen driver returns (screenshot bullet's
  trigger 2: verification, not attachment), watch Metro log for runtime
  errors. Multi-device features (P2P/LAN): clone simulators - share
  host's network/Bonjour, so real radio path testable. Use
  `expo:*` plugin skills for SDK-specific guidance when installed;
  same-plugin **`mobile`** skill = canonical device-driving loop + build
  gotchas (Maestro flow-as-file + `--device`, non-login `LANG` pod builds,
  `--clear` redbox, apple-targets extension creds + App Group, stale autolinking,
  tee/exit-code masking) - invoke for VERIFY. **Never run its
  local-build → TestFlight/Play *ship* from story** - shipping separate,
  owner-approved step, only when ticket/spec explicitly calls for release.
  Non-macOS host (no simulator) → fall back to visual-bug manual-only
  path: spec + PR carry repro steps; say so in report.
- **Frontend change with design reference → screenshot + compare, not just
  green tests.** Spec recorded reference (Phase 1: screenshot, mockup, design
  export, prototype-app capture)? Done = rendered UI **matches it**. Capture
  implemented screen (driver order in screenshot bullet above - argent
  MCP, Maestro, repo's web harness, bare capture), **Read reference + capture
  side by side**, compare layout, spacing, colour, typography, icons, copy.
  Bug screenshot = "before" - capture must show fixed at that spot.
  Mismatch → fix, or list as intentional deviation in PR body. Compare
  for you; images attach only per screenshot bullet (user asked → ticket, PR
  when they said PR). Functionally perfect + visually off = not done.
- **Webhook / callback feature** (new inbound endpoint external provider calls -
  Stripe, GitHub, Twilio, e-sign…) → **test with simulated signed payload, not
  live call:** assert signature check + handler behaviour against sample event
  (repeatable, CI-safe). **Don't gate on live delivery** - needs provider config
  draft PR can't assume. Put **live check** in spec's manual-verification +
  PR body: `corgi tunnel <svc>` for public URL, point provider (or CLI,
  e.g. `stripe listen --forward-to <url>`) at it.
- **Multi-repo consumer:** can't verify (codegen/typecheck) until producer
  committed **and running** - do Phase 4's contract-owner-first step (start producer,
  `corgi status --ready`) BEFORE this gate on consumer. Consumer's types
  come from **introspecting running producer** (GraphQL introspection codegen,
  OpenAPI client-gen against live server) → MUST start producer with corgi,
  run real codegen against it - **never hand-edit generated client/types as
  shortcut because producer down.** Hand-edited generated file drifts from
  real schema, not validated, next real `codegen` silently overwrites.
  Producer down → bring up (`corgi run --services <producer> --with-deps --detach`),
  don't fake output.
- **Stop rule:** can't pass after ~3 honest tries, each on different cause → STOP, leave un-pushed, report
  `needs attention` + failure, rest ships. Never push red.
- **Re-tier mid-flight:** adjustment reveals real design → STOP, bump to feature,
  hand to superpowers. Also **widens span** (another repo or new contract) → loop
  back to Phase 1-2 - re-spec (add `## Contract`), re-gate, create producer
  branch - don't escalate in place.

## Phase 3.5 - Per-story review (scoped, right after gate is green)

Review **each story as it finishes**, scoped to **only its diff** - incremental,
bounded context, NOT one giant end-of-batch review (re-reads everything, burns
tokens).

- Review `git -C <branch-dir> diff <base>...HEAD` - review subagent passed only
  that diff + spec (works everywhere), or `/code-review` /
  `superpowers:requesting-code-review` if present.
- Reviewer gets `../_shared/second-reviewer.md` with diff, hunts every
  kind that applies, from whole tree, each finding a scenario (who does
  what, in which order, what breaks). Scenario cheap to run gets probe
  spec failing on head commit. "No findings" means each applicable kind
  checked - name them in one line.
- Fix **blocking** findings (correctness, missing test, scope creep), re-run gate.
  Cap ~1 extra round; still blocked → Stop rule. Non-blocking → PR body.
- Before push, re-read PR body against head commit: every claim
  (what enforced, what tested, what other PR does) true now and proved
  (conventions › Comments people read; same bar for spec comment and replies), product
  values nobody gave you under `## Assumptions`. **Decide, don't delegate.** Spec
  silent → pick reading ticket, repo's conventions, user's
  earlier words imply, build it, name pick under `## Assumptions` - never
  "open decisions" section for reviewer to settle, never stop. Only reading
  that builds something materially different = question (Phase 1 gate).
  Someone already raised it as product call → theirs (`review` Mode B step 2).

## Phase 4 - Commit, then multi-repo ordering

**Commit:** match repo's `git log` style (Conventional prefix only if repo
does). **Concise subject** + **issue key**; body only if truly needed, never wall.
Attribution and hook rules: `../_shared/conventions.md`.

**Record a fact when it'll matter later (confirm first).** Non-obvious bug root
cause that could recur, or cross-service contract decision, earns memory fact -
draft, show, write on OK via `corgi memory add …` then `corgi memory index` (see
`memory` skill). After `fix` fact, run recurrence check
(`corgi memory list --type fix --json`; `pattern` ≥ 3× → write **proposal**, stop -
never auto-install). No `.corgi/memory/` → offer to create; declined → skip.
**Never put secret in fact.**

**Multi-service** (one issue → N repos → N PRs):

- **Same branch name** in every repo.
- **Contract owner first.** Consumer regenerates types/clients from producer
  (GraphQL codegen, OpenAPI, protobuf, shared schema)? Producer must be implemented,
  committed, AND **running** before consumer verifies:
  ```bash
  corgi run --services <producer> --with-deps --detach   # or: corgi run --detach
  corgi status --ready --service <producer>               # block until healthy
  ```
  Until up, consumer's generated types stale → won't typecheck. Producer from
  `depends_on_services`/`exports` graph (Phase 0). `corgi stop` when done. **Producer
  in worktree?** `corgi run` serves `path:` (main checkout) by default - add
  `--service-dir <producer>=/tmp/corgi-wt/<wt-id>-<service>` to run worktree's
  code (Phase 3).
- Consumers regenerate, commit generated output, finish slice.
- **Merge order:** producer PR first, consumers after. State in spec + every PR body.
- **Consumer starts now.** Its branch cut from producer's branch (not
  `<base>`), so schema or contract there to build against; nothing waits for
  producer's merge. After producer lands, rebase consumer onto `<base>`, say
  so (`../_shared/conventions.md` › Gates).

## Phase 5 - Push + draft PR/MR per repo

Per repo, forge from Phase 0. **`<dir>` = repo's working dir** - worktree dir
(`/tmp/corgi-wt/<wt-id>-<service>`) if worktree'd in Phase 3, else checkout. Push,
create draft, attach spec with commands in `../_shared/forge-commands.md`
§6, run from inside `<dir>`. Title `<subject> [<issue-key>]`; body = what / how / tests /
issue link.

- **Draft only.** Report each PR/MR's diff summary + link; human flips to ready.
- **Risk card in the body.** Right after draft up, run `risk` skill in
  `stamp` mode on it (pre-authorised here - run already passed spec gate, so
  print card, no prompt). Card lands at end of description between markers,
  tells human flipping to ready how much review needed and
  whether mergeable on score alone. Re-stamp after every push in
  Phase 5.5 loop so score follows diff. Multi-repo → stamp each PR/MR; set's
  score = maximum, contract lines go on every side.
- **Watch CI to green (when asked, or in pre-authorized autonomous run).** After
  draft up, poll checks to conclusion, report pass/fail per PR/MR - don't stop at
  "opened".
  - Commands in `../_shared/forge-commands.md` §6 (GitHub `gh pr checks --watch`, GitLab
    `glab ci status`).
  - Batch spanning both forges → watch each PR/MR on own forge. On red, surface
    failing job (name + log tail), fix on branch, push, re-watch (Phase 3 Stop rule);
    never flip to ready to dodge red check. Still draft here - Phase 5.6 decides
    whether it leaves draft.
- **Move ticket to review state** once draft PR/MR up - **resolve,
  don't hardcode:** Linear `Code Review`/`In Review` state (later `started`-type or
  custom, from `list_issue_statuses`); Jira transition whose target named
  _In Review_/_Code Review_ (`getTransitionsForJiraIssue`). **No such state → leave
  In Progress.** Idempotent; skip no-ticket/blocked. Multi-repo → move once **all**
  PRs open, not per-repo. **Best-effort:** tracker↔forge automation may treat
  **draft** PR as _in progress_ and revert move - review state sticks only once
  PR marked _ready_. Set once; don't fight revert.
- **Cross-link** siblings + merge order in each multi-repo PR/MR body.
- **Screenshots in the body** when user asked for them in PR (Phase 3's
  screenshot bullet, or `before-after` run with PR as target), or when
  verification shot shows reviewer something diff can't. Asked for screenshots
  with no place named → ticket, not here. Shot that only served own check
  needs no attachment - one line in `## Evidence` ("verified on iPhone 16 sim")
  enough. When they go in, attach way
  `../_shared/forge-commands.md` §6a says - `corgi assets push <files> --key <key>
  --dir <repo>` puts them on repo's `pr-assets/<key>` branch and prints
  `![name](…blob/pr-assets/<key>/<path>?raw=true)` lines to paste (GitLab may use
  project upload instead) - paste exactly what it printed. Never `raw.githubusercontent.com` (empty box on private repo), never local path, never image on PR branch
  (gone after merge). Shots go in table, one line naming device, environment,
  account, what to look at. Ticket-bound screenshots go into `## Spec`
  comment through tracker's own upload (§6a) - forge link unreadable there.
- **Changed surface at top of body.** `corgi surface --branch <branch>`
  (or `corgi_diff` with `surface: true`) prints `## Changed surface` block.
  `--branch` (here and on `docs check` below) finds any git worktree on that branch,
  this skill's `/tmp/corgi-wt` ones included. corgi older than 2.35.4 sees only
  worktrees it made itself, fails with `no worktrees exist for branch` - then
  `corgi upd`, or write block from `git -C <worktree> diff <base>...HEAD` per
  repo. Block lists exported symbols, routes, contracts, migrations,
  config PR changed, removals and signature changes marked breaking. Paste first - reviewer reads it before diff; breaking line with no caller updated in
  same batch = bug you should've caught here. Re-run after every push
  in Phase 5.5 loop.
- **Docs that name what changed.** `corgi docs check --branch <branch>` lists
  every Markdown file mentioning symbol or route batch changed, and
  every `file:line` pointer in CLAUDE.md or `.claude/rules` no longer
  landing. Fix ones change made wrong in same PR; list rest under
  `Deferred` with file name. Stale doc = bug reviewer can't see
  in diff.
- **Evidence in the body, always.** Reviewer who didn't watch run needs
  four things to trust it, never appear on own: `## Evidence`
  section with (1) **changed files with reason each** - one line per file or
  group, why changed, not what; (2) **commands run + results** -
  test, build, check commands verbatim with pass/fail, so reviewer can
  re-run exactly; (3) **tests → acceptance criteria** - each new or changed
  test mapped to criterion it protects, core flow first; criterion with no
  test says so; (4) **known limitations + residual risk** - what left out,
  what could still go wrong, one line each. Facts only, no adjectives; skip
  nothing done, invent nothing not done. Multi-repo → per PR, own
  files. Before `Deferred` list and risk card.
- **`## You do` in the body** - every step only person can do (statement on
  hosted database, flag, deployed env value, console click), numbered, with
  exact value and when it runs; same block goes on ticket's spec comment,
  final report ends with it (`../_shared/conventions.md` › Steps only a person can
  do). None → omit section.
- **Run-locally line in the body** - same one-paste
  `corgi run --service-branch <svc>=<branch> … --with-deps` (Per-story lines) so
  reviewer spins branch up without hunting.
- **`Deferred` in the body** - one line per deliberate shortcut from Phase 3
  (`<what> · ceiling: <limit> · revisit when: <trigger>`), one line per thing
  ladder left out with rung that covered it. Omit section when nothing; never pad. `risk` card reads it, so ceiling below today's load
  = Check line, not surprise.
- Canonical spec already on tracker (Phase 1); PR/MR comment = convenience
  copy.

## Phase 5.5 - Review loop on the opened PRs/MRs (always, before the report)

Phase 3.5 reviewed each story's raw diff mid-flight; this = **closing check on
actual PRs/MRs** - run **`review` skill** (`/corgi-review` engine) on
every PR/MR this run opened, don't just hand reviewer hint line. Skip
blocked/failed stories.

- One review run over whole set when spanning services - keeps
  cross-service contract check; single PR → review alone.
- **Findings → fix, push, re-review.** Apply valid findings on branch, re-run
  Phase 3 gate on any story whose code changed, push, review again. Wrong
  finding → push back in PR thread, don't blind-apply.
- **Loop until pass has zero blocking findings** - `0 findings - clean.` or
  totals line with `0 blocking` (declined nits don't keep loop
  alive). Cap ~3 rounds; still blocking → stop looping, surface open findings
  as _needs attention_ in report (Stop rule).
- **Cancelable.** "skip the review loop" / "just open the PRs" up front → skip
  phase; "stop the loop" mid-run (or any interrupt) → finish nothing further, keep
  fixes already pushed. Report line stays honest: `review skipped` or
  `✗ review open - round <n>: stopped by user`.
- Still no merge - clean review ≠ merge. Whether PR leaves draft =
  Phase 5.6's call.

## Phase 5.6 - Ready hand-off (only when the workspace asks)

Default: PR/MR stays draft, human flips. Workspace opts in with line
in CLAUDE.md such as "mark agent PRs ready when done" - no line, no flip. Opted in → flip **per PR/MR, only when every one holds**:

- Phase 5.5 ended clean for story (`0 blocking`), not `stopped` or capped.
- PR/MR's own CI green (re-read live; queued pipeline not green).
- No unresolved review thread from anyone but you.
- Story not blocked, partial, or `needs attention`.

Then `gh pr ready <url>` / `glab mr update <iid> --ready`, plus follow-up
workspace CLAUDE.md names (manual CI job to play, reviewer to request). One story
failing checks keeps only that story's PRs in draft; rest still flip. Say which
flipped and which stayed in report.

### Per-story lines (under each ticket block of the report)

Report = `summary` skill's shape (Phase 6): one `## [<issue-key>] <title>`
block per story, table row per repo with PR/MR as markdown link and state
with nuance. No-ticket → short `[<slug>]` tag as key. Under each block:
- **Run line** → after link(s), one **copy-paste** `corgi run` spinning up every
  impacted service on its branch via `--service-branch <svc>=<branch>` (corgi builds
  worktree from pushed branch - reviewer needs nothing else). Same `<branch>`
  across repos. `--with-deps` so deps/dbs come up. One `--service-branch` per service.
  Skip blocked/failed. Same line for you + reviewer - branch committed now,
  no `--service-dir` variant needed.
  (`--service-dir` at live impl worktree belongs to Phase 3 gate, code still
  uncommitted.)
- **Review hint** → after link(s) per actionable (non-blocked) story, one line
  per PR/MR: `↳ review it: /corgi-review <pr-or-mr-link>` - hands reviewer to
  `review` skill (checks diff against repo standards + ticket, posts inline
  suggestions). Skip blocked/failed.
- **CI line (when watched)** → one line per PR/MR after link: `✓ CI green` or
  `✗ CI red - <failing job>`. Omit if CI not watched.
- **Risk line** → one line per story after link(s): `risk N/10 <tier> ·
  auto-approve: yes|no - <reason>` (set's maximum for multi-repo). Exactly
  `risk` skill's `gate` line, so reader - or `autopilot` - can sort batch by it.
- **Review line** → one line per PR/MR after link: `✓ review clean (<n> rounds)`,
  `✗ review open - round <n>: <short finding or "stopped by user">`, or
  `review skipped` when phase skipped up front (Phase 5.5 - one canonical
  form, no variants).
- **Blocked / failed** → no link, one line:
  `[<key>] <Service>: BLOCKED - <decision needed>` (or `needs attention - <reason>`, +
  worktree `/tmp` path if partial work parked there).
- **Review-channel blurb (print it last, every run)** - one part leaving
  terminal, never make user ask → terse. No pitch, no root-cause
  paragraph, no emoji, no "please review" - link unfurls; channel
  convention terse.
  - **Single-repo change** → two lines: `<Service>: <short title>` then bare
    PR/MR link.
  - **Related multi-repo change** → one `<short title>` line for whole change,
    then one `<repo>: <bare link>` line per repo.
  - **Unrelated changes never share title** - each gets own blurb, blank
    line between:

  ```
  Profile: phone field + validation localized
  api: https://github.com/<org>/api/pull/<n>
  web: https://github.com/<org>/web/pull/<n>

  api: fix pagination cursor on empty page
  https://github.com/<org>/api/pull/<n>
  ```

Example (rendered as markdown, not fenced):

```markdown
## [ABC-200] Add phone field to user

| Repo | PR | State |
|---|---|---|
| api | [#41](https://github.com/<org>/api/pull/41) | OPEN - draft, CI ✓ |
| web | [#37](https://github.com/<org>/web/pull/37) | OPEN - draft, CI ✓ |

risk 7/10 high · auto-approve: no - cross-service contract
✓ review clean (1 round)
↳ review it: /corgi-review https://github.com/<org>/api/pull/41 https://github.com/<org>/web/pull/37
▶ corgi run --with-deps --service-branch api=feature/ABC-200/user-phone --service-branch web=feature/ABC-200/user-phone

## [ABC-125] api - BLOCKED - which auth scope gates the endpoint?
```

### Then clean up the worktrees

Phase 3's worktrees under `/tmp/corgi-wt/` are **yours**, not corgi's - `corgi worktree
prune` only manages `.corgi/corgi_services/.worktrees/` (`--service-branch` ones),
won't touch them. Nothing else removes them, so they accumulate one per story per
repo, each carrying `node_modules` symlink and checked-out branch.

Clean **after report**, once every branch pushed - branch lives on
remote, removing worktree loses nothing:

```bash
git -C <dir> worktree remove /tmp/corgi-wt/<wt-id>-<service>   # no --force
git -C <dir> worktree prune                                    # drop the admin entry
```

Rules:

- **Never `--force` here.** Plain `remove` refuses worktree with uncommitted or
  untracked changes; refusal = signal something not committed. Report
  path, leave it - don't discard work to tidy up.
- **Only worktrees this run created.** `/tmp/corgi-wt/` dir from earlier session may
  still be someone's working state.
- **Blocked story → keep its worktree.** Nothing pushed; removing destroys
  only copy.
- Removing worktree deletes `node_modules` **symlink**, never main
  checkout's real directory.
- Leftovers cheap to spot later: `git -C <dir> worktree list` shows every one still
  registered, stale entry whose dir gone clears with `worktree prune`.

---

## Phase 6 - Report

**Per-story lines above = report's content - render them, never prose
write-up around them.** Findings essay not hand-back: nothing in it can be
pasted anywhere.

Hand batch back in **`summary`** skill's shape: one block per ticket, table
row per repo, PR/MR as markdown link, state carrying nuance (`MERGED ✓`,
`OPEN - auto-merge armed`, `OPEN - 2 unresolved threads`). Re-read each PR/MR and ticket
live, don't report what this run did to them - auto-merge fires and pipelines
turn red between push and report, stale row always the one that
matters.

Lead with anything still needing user: blocked story, missing credential,
manual job to play, open question. Then tables, then tracker roll-up. Include
what Phase 5.5 review caught in this run's own work, one line each - highest-signal part, easiest to quietly drop.

Never wrap in fenced code block: renders monospace, kills every link.

**Announce in chat, once, when PRs/MRs were opened** (after Phase 5.6, so flipped PR
announced ready): run
`corgi agent chat announce "<[KEY] story title>" <pr-url> <pr-url> …` with every PR/MR
this run opened (one call per story), bare URLs only. Posts title, then link
alone for one PR or one `repo: link` line per PR for several, in workspace's review channel, in voice workspace configured; no
review channel configured → prints one line, posts nothing - don't work around
that, never post with `chat post` yourself. Skip for story that opened
nothing, and for re-run whose PRs announced before.

---

## Scenarios & scaling

- **Big batch → bound context.** >~4-5 stories → dispatch per-branch implementation
  to subagents (`superpowers:subagent-driven-development` /
  `dispatching-parallel-agents`), one per branch, scoped to spec + shared
  note. Orchestrator stays gate-keeper, collects reports + reviews. Chunk huge
  batch.
- **Concurrent same-repo test runs** need real install per worktree, not
  symlink (build caches race).
- **One blocked story never blocks batch** - held aside; actionable ships;
  blocked surface as questions.
- **Mixed forges/trackers in one batch** fine - resolved per repo / per issue
  (Phase 0).