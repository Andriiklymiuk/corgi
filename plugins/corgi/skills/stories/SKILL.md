---
name: stories
description: "Use when the user wants to ship work across a corgi-compose workspace: tracker issues named by key (ABC-123), by tracker LINK (linear.app/…/issue/…, …atlassian.net/browse/…), or by several links or keys at once — \"do this story <link>\", \"do <link>, <link>\", \"do these stories\", \"implement these tickets\"; a bare tracker link with no verb means this too. Also a free-text feature (\"build a feature that ...\", \"add X across the services\"), or \"what should I work on\", \"grab some agent tickets\". NOT for authoring or running compose, or one-line edits."
---

# Corgi stories

Work items — tracker issues (Linear/Jira) **or** a free-text feature — → spec each
→ isolated branch(es) → tested + reviewed → **draft** PR/MR per repo → review
loop until clean (Phase 5.5) → grouped report. Services, dirs, dependency order: all from `corgi-compose.yml`. Never
hard-code.

## Speed model

**One blocking gate** on the adjustment/bug fast path; complex stories add
superpowers checkpoints.

- **Gate (blocking): spec sign-off** (Phase 2). Confirm intent before any branch.
  Cheap, guards the whole batch. Never skip — a clear up-front directive collapses
  it to inline diagnosis (Phase 2 fast-path): still confirms intent, no separate
  pause.
- **No final gate.** Draft PR/MR: push, open draft, scoped review, review loop
  until clean (Phase 5.5), report diff + link; human flips to _ready_. Draft = not ready-for-review, no merge; CI still runs
  if the repo runs it on drafts.
- **Pre-authorized autonomous run (opt-in).** Blanket approval up front — "I approve
  all changes", "just do it and open the PRs/MRs", "ship it and watch CI" — is standing
  sign-off for the whole batch: collapse the gate regardless of tier/span (Phase 2),
  open the draft PR/MR, and **watch CI to green** (Phase 5). Don't re-ask at each step.
  Guardrails still hold — draft-only, never merge, never force-push, blocked stories
  still surface as questions.
- **Automatic mode (unattended).** The daemon runs this skill on its own for a ticket
  assigned to the user when the workspace has `corgi agent watch enable --action fix
  --auto-for tickets` (or `all`); the prompt then carries the `corgi watch · …` trail
  and "I approve all changes". That is the pre-authorized run above with one more rule
  from `../_shared/conventions.md`: **never ask** — every choice takes the recommended
  option, named in one line; a story that cannot go on ends in `corgi agent handoff
  --ref <key> --blocked "<why>"`, not in a question. The run still posts the spec
  comment, opens draft PRs/MRs, runs the review loop and reports; `corgi agent today`,
  the inbox and the kanban show it the same as any run. Turning it on is a person's
  choice per workspace, never this skill's.

### Story tiers — set per story (Phase 1), drives rigor

| Tier           | What                                                              | Extra rigor                                                          |
| -------------- | ----------------------------------------------------------------- | -------------------------------------------------------------------- |
| **Adjustment** | clear-spec UI/copy/flag/config; unambiguous                       | just a test for the new behaviour                                    |
| **Bug**        | broken / regressed                                                | regression test **FAILS on base branch** before fix, passes after    |
| **Feature**    | new behaviour, real design, or new/changed cross-service contract | hand to **superpowers** if installed, else equivalent inline (below) |

**Tier ≠ span.** Complexity axis vs single/multi-service (Phase 4). Multi-service
adjustment is still an adjustment. Most stories = adjustments → fastest path.

**How the tier is picked.** `--mode bug|feature|adjustment` in the invocation
wins. Else the ticket's own words: type or labels `bug`, `defect`, `regression`,
`incident`, `hotfix` → **Bug**; `feature`, `story`, `epic`, `design` → **Feature**;
anything else → **Adjustment** until Phase 1 proves otherwise. If the diff fits
one sentence, it is an adjustment whatever the label says.

**Risk lane — always a plan, whatever the size.** A change that touches
migrations, auth, sessions, permissions, payments, billing, secrets or a
cross-service contract gets Feature rigor even when it is three lines: the
interface or schema change is written and confirmed first (Phase 2 gate never
collapses for it), and the spec names the rollback. The workspace may extend
the list with `riskPaths:` under its entry in the user agent config.

**Bug lane — logs first, theories second.** Before a hypothesis: reproduce
against the running stack (`corgi run`, `corgi logs <svc> --errors-only`,
`corgi_wait_for_log`), quote the failing line or request, then write the
regression test that fails on `<base>`, then the smallest fix. No plan file, no
subagent fan-out: a bug is a straight line from the log to the test.

**Express lane — small-surface adjustment.** An adjustment or bug in a single service
with no cross-service contract, whose surface you can name up front (one component or
a few files: an asset/copy/flag/style swap, small wiring), takes lighter machinery. An
open design question (which variant? what scope?) does not change that — it only
decides whether the Phase 2 gate pauses, and the express lane never skips the gate.
Lighter steps, same guardrails:

- **No `Explore` subagent** — grep + read the 2–3 files inline; a subagent returns a
  full report to map one component, the orchestrator's own search is cheaper. Reserve
  `Explore` for unclear surface or real fan-out.
- **No local `docs/stories/*.md`** — the `## Spec` tracker comment IS the spec, don't
  write both (multi-service/feature still gets the file).
- **Right-size proof** — gate once (typecheck/lint/test) + the single proof the change
  needs (visual swap → a serve check, or one screenshot you read when looking is the
  quickest way to know). Don't stack every proof.

Still mandatory: gate sign-off, the spec comment (QA section folded in), branch, per-story review, draft
PR, report. Unsure on scope → a quick inline grep settles it; don't default to the
heavy path.

**Bug sub-type — logic vs visual.** "FAILS on base" assumes a unit test can _see_
it. **Visual/layout bug** (z-index/stacking, overflow, position, breakpoint, CSS
specificity/cascade) → jsdom can't catch it; a green unit test proves nothing.
Don't fake one.

- **Repo has a visual/e2e harness** (Playwright, Cypress, Storybook visual diff;
  Maestro for Expo/RN — `references/expo-verification.md`) → red check **there**,
  same FAILS-on-base.
- **None** → **manual-only is legit, not a skip.** Spec + PR body carry repro steps;
  report says manually verified, no auto guard. Still include the **QA "what to
  test" section** in the spec comment — a human must re-check visual. Screenshots
  follow Phase 3's screenshot bullet (the user asked, or looking is the quickest way
  for you to see the fix) — they are not a standing requirement of this path.
  - **Expo/RN service on a macOS host → not manual-only:** drive the simulator
    with argent MCP or Maestro even without a committed harness and look at the
    screen the fix touches (`references/expo-verification.md`, Phase 3's
    screenshot bullet).

**Before/after asked for → run `before-after`.** Any phrasing of "screenshot
comparison", "before and after", "show the visual diff", "prove the UI changed" is a
request for the **`before-after`** skill, not a second after-only screenshot: it builds
the base branch too, captures the same screen twice, and puts both where the user
asked — the PR body when they said PR, else the ticket (§6a). It costs two builds, so
it runs **only when the user asks for it** — never on your own initiative, however
visual the change; the gate plus the repo's harness is the default proof. Never satisfy
the request by describing the old state from the diff; the diff is exactly what does
not show it.

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

- Read `../_shared/conventions.md` first (attribution, `manualRun`, preflight, worktrees).
- **Draft PRs/MRs only.** Never merge, never force-push. A PR leaves draft only through
  Phase 5.6, and only when the workspace CLAUDE.md asks for it.
- **One blocking gate** — spec sign-off (Phase 2); the sign-off _is_ the branch
  authorization.
- **No destructive git without explicit OK** — checkout off a dirty tree, branch
  deletes, force-push, pushing shared branches.
- **Don't push the workspace meta repo** unless asked — only service branches.
- **Minimal code comments.** Don't narrate the change in code comments — it reads from
  the diff and the PR/commit. Add one only for a genuinely non-obvious invariant that
  would otherwise be lost, and only where the file already comments. No "// added X" notes.
- **Read the repo's own agent docs before running anything in it.** A service's
  CLAUDE.md/AGENTS.md may forbid specific commands (a dev server that breaks the build,
  a script that mutates state). A triage produced by a forbidden command is noise —
  rerun it the documented way before believing it.
- **Don't spam the ticket.** One comment per story (spec, with its QA section folded in),
  updated in place on re-runs — never a pile of new comments.

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

1. Locate the compose file (preflight in `../_shared/conventions.md`). None →
   `/corgi-new` first, or ask which repos; don't guess a layout.
   **The pasted context may not be this workspace.** A diff, MR body, ticket or
   screenshot dropped into the prompt can come from another product entirely — and
   may be an *example of the shape wanted*, not the target. Never identify the
   workspace by hunting the filesystem for code that matches the paste: confirm
   which one before Phase 1, and re-confirm when the paste names services the
   compose file doesn't.
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
   - Forge and `<base>`: per `conventions.md` (base is used for the branch, the red
     test, and the PR target).
   - Test/typecheck/lint/build: discover from `package.json` scripts, `Makefile`,
     `pyproject`/`go.mod`, service `start`/`beforeStart`/`scripts`. Don't assume a
     runner. **Also find the CI gate the PR will actually face** — a coverage-threshold
     script (`test:cov:check`, a `check-coverage` step, a `--coverage` floor) or the CI
     workflow (`.github/workflows`, `.gitlab-ci.yml`). Note it; Phase 3 runs that same
     gate before the PR, not just a scoped test.
4. **Detect tracker.** Read `../_shared/tracker-mcp.md` first.

## Phase 1 — Investigate (once), then spec

**Route the intake first — what am I building?**

- **Explicit tickets** (keys/links) or a **free-text feature** → continue below.
  A link IS a ticket: `linear.app/<org>/issue/ABC-123/…` and
  `<site>.atlassian.net/browse/ABC-123` both carry the key — take it from the URL
  and carry on, never ask which ticket was meant. Several links or keys in one
  message are **one batch**, not one run each: they get specced together.
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
  placement. No near-equivalent.
- **Design reference = the verify target, not just spec input.** Any visual
  reference (screenshot, mockup, attachment, design-tool link, or a **reference
  app/prototype repo** — e.g. an AI-generated one), **wherever it surfaces** —
  ticket, comments, prior spec/docs, chat, disk — is what Phase 3's visual
  compare checks the implementation against; record its path/URL in the spec.
  Link with no image on disk → fetch/export it (design-tool MCP if connected,
  else ask the user). Reference is a runnable app/repo → run it and screenshot
  the relevant screens (that capture becomes the target image), and read its
  styles/components direct for exact values. Don't build a UI story from prose
  while a design exists unseen. **No design, or the user waives it → fine:** build from
  prose, verify functionally, note "no design reference" in the spec/PR — the
  compare only applies when a reference exists, never blocks a story without one.

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
Re-opened ticket whose prior fix already **merged**? Read the prior PR/MR's state
(`gh pr view <n> --json state` / `glab mr view`): MERGED + closed → its branch is
dead (often deleted) and the fix already sits on `<base>` — branch **fresh off
`<base>`**, never reopen/continue the merged branch; its old `## Spec` comment is the
insufficient fix, so update that comment, don't follow it.

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
7. **Story is conformance to an EXTERNAL contract** (a platform's integration
   requirements, a protocol revision, a third-party API)? Read that vendor's
   current docs before speccing. The repo only shows what was implemented, never
   what the other side requires today — and their **limits** (payload caps,
   timeouts, supported versions) bind as hard as their endpoints, while failing
   only on real data. Cite the doc in the spec.

### Forecast the risk — every story

Before the spec, run the `risk` skill on the **story** (ticket text + the code area it
names): a forecast, `confidence: low`, never auto-approve. It decides two things here:
which **tier** the story gets when the ticket alone is unclear (a forecast of 7+ is
never an adjustment), and what goes in the spec's **Before building** list — the
decisions the human makes at the Phase 2 gate (flag, migration strategy, rollout,
which service owns the data). Put the forecast line in the spec comment so the sign-off
is made knowing it; the real score comes from the diff at Phase 5.

### Write the spec — every story

`docs/stories/<issue-key>-<slug>.md`, actionable or not (micro adjustment → skip the
file; the `## Spec` comment is the spec — _Express lane_):

- Problem (quote issue) + **which services** (drives branch/PR count).
- **Tier** — adjustment/bug/feature.
- Root cause / current behaviour, `file:line` refs.
- Change plan (snippets) **grouped by service**, tests, manual verification, risks.
  Name what the plan deliberately does **not** build and what covers it instead (a
  native element, an existing helper) — the cheapest spec is the one with the fewest
  new parts. Multi-service: `## Contract` + cross-service order.

### Triage: actionable vs blocked — controls POSTING, not writing

- **Actionable → ONE comment on the issue** (human-readable, not a `.md` attachment).
  **Open with a `## Spec` heading**; when the change is user-testable, end it with a
  short `## What to test` section (plain QA: clicks + outcome, no code/file refs, end
  `Expected:`) — folded into the **same** comment, not a second one. No HTML comment
  marker (trackers render `<!-- … -->` as visible text), no footer badge. Linear
  `mcp__linear-server__save_comment({ issueId, body })` (omit `id` to create, pass `id`
  to update); Jira `mcp__atlassian__addCommentToJiraIssue`. Literal newlines/markdown.
  The `## Spec` heading is how a later run (new session, no comment id) finds it: list
  comments, match the one whose first heading is `## Spec`, and **update** it
  (`save_comment({ id, body })`) instead of duplicating. Skip the QA section for
  non-testable stories.
- **Blocked → do NOT post.** Spec local only; mark `Status: BLOCKED` + **Decision
  needed**; surface the choice. Hold it; rest of batch proceeds.
- **Env-gated dependency = blocked, not guessed.** A registry behind VPN/auth, a
  build that needs credentials only a human holds — mark the story blocked naming
  exactly what's missing; never fake the verification or report it as run.

`superpowers:brainstorming` / `superpowers:systematic-debugging` (if installed) to
resolve ambiguity before blocking.

## Phase 2 — Gate: spec sign-off (the one blocking gate)

Present all actionable specs in **one round**; sign-off before any branch —
batch-level, not per-branch. Re-present only changed specs. Blocked held out. Open
questions inside a spec use the `align` skill's fork format (`❓ Qn` + `➡️` pick, ≤5 per
spec); a story with more forks than that is not ready to gate.
Superpowers-escalated stories pass here too: their `writing-plans` output is the
spec.

**Fast-path — collapse the approval round to inline diagnosis.** The gate confirms
intent before branching; it collapses (state diagnosis + the exact change inline, then
go — that _is_ the gate, the user can still stop you) in exactly two cases:

- **One unambiguous item:** one ticket or one named fix, tier adjustment or bug, single
  service, no cross-service contract, and you can state the exact change in one
  sentence with no open question. Anything else — feature tier, >1 story,
  multi-service, an ambiguous target, a clarifying question you had to ask yourself —
  is the full gate. A vague "just fix it" with no scope is open intent, not clearance.
- **Blanket pre-authorization:** the user explicitly pre-approved the changes
  sight-unseen and told you to proceed to PR/MR ("I approve all changes", "do them all
  and open the MRs"), or the run is unattended (`../_shared/conventions.md` — the
  `corgi watch ·` trail). That is the sign-off for any tier, span, or batch size; state
  each spec inline and go. The signal is explicit approval plus a directive, not
  impatience.

**Workspace says wait.** `corgi_watch_switches` (or `corgi agent watch`) may carry
`planReview: always | risk>=N` for this workspace (`corgi agent watch enable --plan-review`).
When it matches the story's forecast, neither fast-path applies: post the spec, then ask
for the sign-off with `AskUserQuestion` so the phone can answer it, and cut no branch
until it comes back. Unset or `off` (the default) changes nothing above.

Either way the fast-path drops the pause — never the thinking, and not the artifacts:
post the spec comment (with its QA section) even for a one-liner. It is the durable
record, and a visual/QA defect is what a human must re-verify.

## Phase 3 — Branch + implement + verify per story

Branch: `feature/<issue-key>/<kebab-slug>`, same name in every affected repo.

**Write the scope before the first edit.** The spec named the files or areas
that change and what done means; put that on the record so the hooks hold you
to it while you work:

```bash
corgi agent scope set <issue-key> --path "api/limits/**" --path "web/src/limits/**" \
  --lines <diff budget> --tests <new test files> --done "<criterion>" --done "…"
```

Paths are workspace-relative globs (`**` crosses directories; a directory
covers what is in it). The line budget is the spec's honest guess at the diff
(adjustment ≈ 60, bug ≈ 150, feature ≈ 400 per service) and the test budget one
file per acceptance criterion, core flow first. From then on a write outside the
paths is refused with the way to widen — `corgi agent scope add <key> --path …`
— and widening is fine when the change needs it; say why in the PR. A diff over
budget is reported once when the turn ends: trim it, or raise the budget on the
record (`scope set --lines N`) with one line in the PR saying why. Never widen
silently, never disable the hook.

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

**Dirty + overlap guard.** The worktree gate in `../_shared/conventions.md` applies:
edits overlapping the story's files → STOP and ask; no overlap → worktree safe.

**Must-run producer in a worktree → run it with `--service-dir`.** A producer that
must be _running_ for a consumer to verify (Phase 4) can live in a worktree:
`corgi run --service-dir <producer>=/tmp/corgi-wt/<wt-id>-<service>` (below).

- **Branch in place** (clean tree). Branch straight off the fetched remote base — no
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
    "run this branch", but for stories point at your impl worktree.) A branched repo
    that **isn't** a corgi service → no `--service-dir`; run its runner in the
    worktree dir.
  - **Success →** `git -C <dir> worktree remove /tmp/corgi-wt/<wt-id>-<service>` once
    the PR is up. **Failure (Stop rule) →** leave it; report its `/tmp` path. Never
    `worktree remove` a failed story.

Implement to spec, and **before each piece, walk the ladder in
`references/smallest-change.md`** — not needed → already in the repo → the language
ships it → the platform ships it → an installed dependency does it → one clear line →
the least code that passes the check. Stop at the first rung that holds. **Minimum
diff — no opportunistic refactor, no abstraction nobody asked for, no new dependency
for what an installed one covers, no code comments** unless the file already comments
heavily. Validation at trust boundaries, data-loss error handling, security,
accessibility and the tier's test are never what gets trimmed. A shortcut with a known
ceiling goes in the PR body's `Deferred` list (Phase 5), not in a comment. Run the
**per-service gate** (tests + typecheck + lint) BEFORE commit. Tests for every change,
matching existing patterns.

- **Run the gate through corgi when the service is in `corgi-compose.yml`** — gives
  the worktree full resolved env, deps, cwd, so you don't guess the runner or
  hand-build env:
  - `test` script → `corgi test --changed --base <base>` runs the whole test script of
    every repo that differs from `<base>` (uncommitted work counts); add
    `--service-dir <svc>=<worktree-dir>` for worktree'd services. It reports nothing
    changed → fall back to `corgi test --service <svc>` (in place, or with the same
    `--service-dir` mapping).
  - Other command (typecheck/lint/migrate/one-off) →
    `corgi exec <svc> --service-dir <svc>=<worktree-dir> --ensure-deps -- <cmd>`.
  - Not in compose, no `test` script, or no compose → run the discovered runner
    (Phase 0) in the worktree dir. Same `--service-dir <svc>=/tmp/corgi-wt/<wt-id>-<svc>`
    mapping as `corgi run` (Phase 3); drop for in-place.
- **Bug tier: red test first** — write it, confirm **FAILS on base**, then make it
  pass. Adjustments skip.
- **New code lowers complexity, never raises it.** Before the per-service gate, run
  the `complexity` skill in `gate` mode on the diff (touched functions vs `<base>`): a
  function that started at or under the repo's threshold ends at or under it, one that
  started over it does not rise, a new function starts under it. `fail` → apply the
  skill's tactics to that function, re-gate. The review loop (Phase 5.5) checks the
  same rule, so a regression caught here is one round saved there.
- **Pre-existing red baseline.** Repo typecheck/lint may already fail on `<base>`,
  unrelated. Don't chase a whole-repo green that never existed; don't let baseline
  noise hide your breakage. Gate on **no NEW errors** — filter the run to changed
  files, or diff the base error set. Touched files clean; baseline left as-is, not
  "fixed" (scope creep).
- **Run what CI runs, not a cherry-picked subset.** `corgi test --changed --base <base>`
  runs each changed repo's whole `test` script in its resolved env (`--service <svc>`
  when `--changed` finds nothing), so the suites that import your changed module load
  too. Then run the CI gate Phase 0 found: a coverage floor over changed files counts
  an untested file you merely touched — add a test for it or leave it alone. A green
  exit proves nothing unless the intended suites ran: check the test count is above
  zero (a path filter containing `(…)` or `[…]` can match nothing and still pass).
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
- **Screenshots — a tool you reach for, not a box to tick.** Decide once:
  1. **The user asked** ("with screenshots", "attach screens", "show me how it looks")
     → capture and attach to the **ticket** (Linear/Jira, through the tracker's own
     upload — §6a); that is where an asked-for screenshot goes when no place is named.
     Put them in the **PR body when the user said PR** ("screenshots in the PR",
     "include them in the PR"), both when they asked for both.
  2. **Nobody asked, but a look helps you test it** (a layout fix jsdom can't see, a
     native control, a design reference to compare against, an Expo flow driven on the
     sim) → take it, **read it**, verify — that is what the drivers below are for.
     Keep it as a working file by default and say "verified on <device/browser>" in
     the report; if the shot genuinely helps a reviewer (it shows the one thing the
     diff can't), attaching it is fine — a judgement call, not a requirement.
  3. **Neither** → nothing to capture, and no apology for it. Api-only, config, CI,
     test-only and most logic changes have nothing to show; "a screen changed" is
     not by itself a reason to shoot every state.
  Don't shoot a terminal, and don't stack screens to look thorough. When capturing,
  pick the driver in this order, first one present wins:
  1. **argent MCP** (`mcp__argent__*` tools in the tool list; `argent --version` on
     PATH) — iOS simulator, Android emulator, or a Chromium page over CDP (a web
     service in a browser started with `--remote-debugging-port`): `list-devices`,
     `launch-app` / `open-url`, `describe` for tap coordinates, `gesture-tap`,
     `screenshot`. Every action returns the screen after it, so no flow file and no
     guessed coordinates. Read the argent rule/skill it ships before the first call.
  2. **Maestro** (the `mobile` skill; its MCP when connected) — a flow file with
     `takeScreenshot` inside the flow, `--device <udid>`.
  3. **The repo's web harness** (Playwright / Cypress screenshot) for a browser
     surface with no argent; else a headless browser (`npx playwright screenshot`,
     `chrome --headless --screenshot`).
  4. **Bare capture** last — `xcrun simctl io <udid> screenshot`, `adb exec-out
     screencap -p`.
  Whatever captured it: log in as the account class the story is about, seed the
  state the ticket describes, and **read each image** before attaching or concluding.
  An **asked-for** set (trigger 1) is complete: every state the spec names (empty /
  in progress / done / error) and every rendered side of a multi-service story — a
  web or admin console that shows the same record gets its shot too. A
  **verification** shot (trigger 2) covers the one spot the change touches, nothing
  more. Name each file `<n>-<what>.png`. Attaching: ticket through the tracker's own
  upload, PR per Phase 5 (§6a link form). No driver and no device (a host with no
  simulator or browser) → repro steps in the spec and PR instead; say so in the
  report when the user had asked for shots.
- **Ticket links a design (Figma / mockup) → the story is done at a READ side-by-side**,
  not at green tests. Pull the frames into `docs/design/<ticket>/`, capture the same states
  from the app, compose labelled design-vs-app images, and put the deviation table (fixed vs
  deliberate divergence) in the PR/MR — the table is the record; the composed images
  go with it per the screenshot bullet above (ticket when the user asked for
  screenshots, PR when they said PR, else your call). A feature still behind a flag or without seeded data
  is forced on with a temporary, clearly-marked local switch — reverted before commit. Full
  loop: the `design-parity` skill.
- **Expo / React Native service → verify on a simulator, not just jest**
  (`references/expo-verification.md`). Detect: `package.json` depends on `expo`
  (or `react-native` + `ios/`/`android/`). Jest-green is not done: Metro
  interop, native modules, permissions/entitlements, and visual layout only
  fail on device. Native-scoped change (new native dep, `app.json`
  plugins/permissions) → rebuild dev client (prebuild → `LANG=en_US.UTF-8 pod
  install` → xcodebuild) and re-install; JS-only → existing build + Metro
  reload. Drive the changed flow with the driver the screenshot bullet picked —
  argent MCP when present, else **Maestro** (flows in `e2e/`, committed with the
  PR) — confirm by reading the screen the driver returns (the screenshot bullet's
  trigger 2: verification, not an attachment), watch the Metro log for runtime
  errors. Multi-device features (P2P/LAN): clone simulators — they
  share the host's network/Bonjour, so the real radio path is testable. Use the
  `expo:*` plugin skills for SDK-specific guidance when installed; the
  same-plugin **`mobile`** skill is the canonical device-driving loop + the build
  gotchas (Maestro flow-as-file + `--device`, non-login `LANG` pod builds,
  `--clear` redbox, apple-targets extension creds + App Group, stale autolinking,
  tee/exit-code masking) — invoke it for the VERIFY. **Never run its
  local-build → TestFlight/Play *ship* from a story** — shipping is a separate,
  owner-approved step, only when the ticket/spec explicitly calls for a release.
  On a non-macOS host (no simulator) → fall back to the visual-bug manual-only
  path: spec + PR carry repro steps; say so in the report.
- **Frontend change with a design reference → screenshot + compare, not just
  green tests.** Spec recorded a reference (Phase 1: screenshot, mockup, design
  export, prototype-app capture)? Done = the rendered UI **matches it**. Capture the
  implemented screen (the driver order in the screenshot bullet above — argent
  MCP, Maestro, the repo's web harness, bare capture), **Read reference + capture
  side by side** and compare layout, spacing, colour, typography, icons, copy.
  Bug screenshot = the "before" — your capture must show it fixed at that spot.
  Mismatch → fix, or list as intentional deviation in the PR body. The compare is
  for you; the images attach only per the screenshot bullet (user asked → ticket, PR
  when they said PR). Functionally perfect + visually off = not done.
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
Attribution and hook rules: `../_shared/conventions.md`.

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
  code (Phase 3).
- Consumers regenerate, commit generated output, finish their slice.
- **Merge order:** producer PR first, consumers after. State in spec + every PR body.

## Phase 5 — Push + draft PR/MR per repo

Per repo, forge from Phase 0. **`<dir>` = the repo's working dir** — the worktree dir
(`/tmp/corgi-wt/<wt-id>-<service>`) if worktree'd in Phase 3, else the checkout. Push,
create the draft, and attach the spec with the commands in `../_shared/forge-commands.md`
§6, run from inside `<dir>`. Title `<subject> [<issue-key>]`; body = what / how / tests /
issue link.

- **Draft only.** Report each PR/MR's diff summary + link; human flips to ready.
- **Risk card in the body.** Right after the draft is up, run the `risk` skill in
  `stamp` mode on it (pre-authorised here — the run already passed the spec gate, so
  print the card, no prompt). The card lands at the end of the description between its
  markers and tells the human who flips it to ready how much review it needs and
  whether it may be merged on the score alone. Re-stamp after every push in the
  Phase 5.5 loop so the score follows the diff. Multi-repo → stamp each PR/MR; the set's
  score is the maximum, and the contract lines go on every side.
- **Watch CI to green (when asked, or in a pre-authorized autonomous run).** After the
  draft is up, poll checks to conclusion and report pass/fail per PR/MR — don't stop at
  "opened".
  - Commands in `../_shared/forge-commands.md` §6 (GitHub `gh pr checks --watch`, GitLab
    `glab ci status`).
  - Batch spanning both forges → watch each PR/MR on its own forge. On red, surface the
    failing job (name + log tail), fix on the branch, push, re-watch (Phase 3 Stop rule);
    never flip to ready to dodge a red check. Still draft here — Phase 5.6 decides
    whether it ever leaves draft.
- **Move the ticket to the review state** once its draft PR/MR is up — **resolve,
  don't hardcode:** Linear a `Code Review`/`In Review` state (later `started`-type or
  custom, from `list_issue_statuses`); Jira the transition whose target is named
  _In Review_/_Code Review_ (`getTransitionsForJiraIssue`). **No such state → leave
  In Progress.** Idempotent; skip no-ticket/blocked. Multi-repo → move once **all**
  PRs are open, not per-repo. **Best-effort:** a tracker↔forge automation may treat a
  **draft** PR as _in progress_ and revert this move — the review state only sticks once
  the PR is marked _ready_. Set it once; don't fight a revert.
- **Cross-link** siblings + merge order in each multi-repo PR/MR body.
- **Screenshots in the body** when the user asked for them in the PR (Phase 3's
  screenshot bullet, or `before-after` run with the PR as its target), or when a
  verification shot shows a reviewer something the diff can't. Asked for screenshots
  with no place named → the ticket, not here. A shot that only served your own check
  needs no attachment — one line in `## Evidence` ("verified on iPhone 16 sim") is
  enough. When they do go in, attach them the way
  `../_shared/forge-commands.md` §6a says — `corgi assets push <files> --key <key>
  --dir <repo>` puts them on the repo's `pr-assets/<key>` branch and prints the
  `![name](…blob/pr-assets/<key>/<path>?raw=true)` lines to paste (GitLab may use the
  project upload instead) — and paste exactly what it printed. Never `raw.githubusercontent.com` (an
  empty box on a private repo), never a local path, never an image on the PR branch
  (gone after the merge). Shots go in a table, one line naming device, environment,
  account and what to look at. Ticket-bound screenshots go into the `## Spec`
  comment through the tracker's own upload (§6a) — a forge link is unreadable there.
- **Changed surface at the top of the body.** `corgi surface --branch <branch>`
  (or `corgi_diff` with `surface: true`) prints the `## Changed surface` block:
  exported symbols, routes, contracts, migrations and config this PR changed,
  removals and signature changes marked breaking. Paste it first — it is what a
  reviewer reads before the diff, and a breaking line with no caller updated in
  the same batch is a bug you should have caught here. Re-run after every push
  in the Phase 5.5 loop.
- **Docs that name what changed.** `corgi docs check --branch <branch>` lists
  every Markdown file that mentions a symbol or route the batch changed, and
  every `file:line` pointer in CLAUDE.md or `.claude/rules` that no longer
  lands. Fix the ones the change made wrong in the same PR; list the rest under
  `Deferred` with the file name. A stale doc is a bug the reviewer cannot see
  in the diff.
- **Evidence in the body, always.** A reviewer who did not watch the run needs
  four things to trust it, and they never appear on their own: a `## Evidence`
  section with (1) **changed files with a reason each** — one line per file or
  group, why it changed, not what; (2) **commands run and their results** — the
  test, build and check commands verbatim with pass/fail, so a reviewer can
  re-run exactly that; (3) **tests → acceptance criteria** — each new or changed
  test mapped to the criterion it protects, core flow first; a criterion with no
  test says so; (4) **known limitations and residual risk** — what was left out,
  what could still go wrong, in one line each. Facts only, no adjectives; skip
  nothing you did, invent nothing you did not. Multi-repo → per PR, its own
  files. Before the `Deferred` list and the risk card.
- **Run-locally line in the body** — the same one-paste
  `corgi run --service-branch <svc>=<branch> … --with-deps` (Per-story lines) so a
  reviewer spins the branch up without hunting.
- **`Deferred` in the body** — one line per deliberate shortcut from Phase 3
  (`<what> · ceiling: <limit> · revisit when: <trigger>`), and one line per thing
  the ladder left out with the rung that covered it. Omit the section when there is
  nothing; never pad it. The `risk` card reads it, so a ceiling below today's load
  is a Check line rather than a surprise.
- Canonical spec already on the tracker (Phase 1); PR/MR comment is a convenience
  copy.

## Phase 5.5 — Review loop on the opened PRs/MRs (always, before the report)

Phase 3.5 reviewed each story's raw diff mid-flight; this is the **closing check on
the actual PRs/MRs** — run the **`review` skill** (the `/corgi-review` engine) on
every PR/MR this run opened, don't just hand the reviewer the hint line. Skip
blocked/failed stories.

- One review run over the whole set when it spans services — keeps the
  cross-service contract check; single PR → review it alone.
- **Findings → fix, push, re-review.** Apply valid findings on the branch, re-run
  the Phase 3 gate on any story whose code changed, push, review again. Wrong
  finding → push back in the PR thread, don't blind-apply.
- **Loop until a pass has zero blocking findings** — `0 findings — clean.` or a
  totals line with `0 blocking` (nits you decline to apply don't keep the loop
  alive). Cap ~3 rounds; still blocking → stop looping, surface the open findings
  as _needs attention_ in the report (Stop rule).
- **Cancelable.** "skip the review loop" / "just open the PRs" up front → skip the
  phase; "stop the loop" mid-run (or any interrupt) → finish nothing further, keep
  fixes already pushed. Report line stays honest: `review skipped` or
  `✗ review open — round <n>: stopped by user`.
- Still no merge — a clean review is not a merge. Whether the PR leaves draft is
  Phase 5.6's call.

## Phase 5.6 — Ready hand-off (only when the workspace asks)

Default: the PR/MR stays draft and a human flips it. A workspace opts in with a line
in its CLAUDE.md such as "mark agent PRs ready when done" — no line, no flip. When it
opts in, flip **per PR/MR, only when every one of these holds**:

- Phase 5.5 ended clean for this story (`0 blocking`), not `stopped` or capped.
- The PR/MR's own CI is green (re-read it live; a queued pipeline is not green).
- No unresolved review thread from anyone but you.
- The story is not blocked, partial, or `needs attention`.

Then `gh pr ready <url>` / `glab mr update <iid> --ready`, plus whatever follow-up the
workspace CLAUDE.md names (a manual CI job to play, a reviewer to request). One story
failing the checks keeps only that story's PRs in draft; the rest still flip. Say which
flipped and which stayed in the report.

### Per-story lines (under each ticket block of the report)

The report is the `summary` skill's shape (Phase 6): one `## [<issue-key>] <title>`
block per story, a table row per repo with the PR/MR as a markdown link and the state
with its nuance. No-ticket → a short `[<slug>]` tag as the key. Under each block:
- **Run line** → after the link(s), one **copy-paste** `corgi run` spinning up every
  impacted service on its branch via `--service-branch <svc>=<branch>` (corgi builds
  the worktree from the pushed branch — reviewer needs nothing else). Same `<branch>`
  across repos. `--with-deps` so deps/dbs come up. One `--service-branch` per service.
  Skip blocked/failed. Same line for you + reviewer — the branch is committed now,
  no `--service-dir` variant needed.
  (`--service-dir` at the live impl worktree belongs to the Phase 3 gate, code still
  uncommitted.)
- **Review hint** → after the link(s) per actionable (non-blocked) story, one line
  per PR/MR: `↳ review it: /corgi-review <pr-or-mr-link>` — hands the reviewer to the
  `review` skill (checks the diff against repo standards + the ticket, posts inline
  suggestions). Skip blocked/failed.
- **CI line (when watched)** → one line per PR/MR after the link: `✓ CI green` or
  `✗ CI red — <failing job>`. Omit if CI wasn't watched.
- **Risk line** → one line per story after the link(s): `risk N/10 <tier> ·
  auto-approve: yes|no — <reason>` (the set's maximum for multi-repo). Exactly the
  `risk` skill's `gate` line, so a reader — or `autopilot` — can sort the batch by it.
- **Review line** → one line per PR/MR after the link: `✓ review clean (<n> rounds)`,
  `✗ review open — round <n>: <short finding or "stopped by user">`, or
  `review skipped` when the phase was skipped up front (Phase 5.5 — one canonical
  form, no variants).
- **Blocked / failed** → no link, one line:
  `[<key>] <Service>: BLOCKED — <decision needed>` (or `needs attention — <reason>`, +
  the worktree `/tmp` path if partial work is parked there).
- **Review-channel blurb (print it last, every run)** — the one part that leaves the
  terminal, so never make the user ask for it → terse. No pitch, no root-cause
  paragraph, no emoji, no "please review" — the link unfurls; the channel
  convention is terse.
  - **Single-repo change** → two lines: `<Service>: <short title>` then the bare
    PR/MR link.
  - **Related multi-repo change** → one `<short title>` line for the whole change,
    then one `<repo>: <bare link>` line per repo.
  - **Unrelated changes never share a title** — each gets its own blurb, blank
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
| api | [#41](https://github.com/<org>/api/pull/41) | OPEN — draft, CI ✓ |
| web | [#37](https://github.com/<org>/web/pull/37) | OPEN — draft, CI ✓ |

risk 7/10 high · auto-approve: no — cross-service contract
✓ review clean (1 round)
↳ review it: /corgi-review https://github.com/<org>/api/pull/41 https://github.com/<org>/web/pull/37
▶ corgi run --with-deps --service-branch api=feature/ABC-200/user-phone --service-branch web=feature/ABC-200/user-phone

## [ABC-125] api — BLOCKED — which auth scope gates the endpoint?
```

### Then clean up the worktrees

Phase 3's worktrees under `/tmp/corgi-wt/` are **yours**, not corgi's — `corgi worktree
prune` only manages `.corgi/corgi_services/.worktrees/` (the `--service-branch` ones) and will
not touch them. Nothing else removes them either, so they accumulate one per story per
repo, each carrying a `node_modules` symlink and a checked-out branch.

Clean them **after the report**, once every branch is pushed — the branch lives on the
remote, so removing its worktree loses nothing:

```bash
git -C <dir> worktree remove /tmp/corgi-wt/<wt-id>-<service>   # no --force
git -C <dir> worktree prune                                    # drop the admin entry
```

Rules:

- **Never `--force` here.** Plain `remove` refuses a worktree with uncommitted or
  untracked changes; that refusal is the signal something was not committed. Report the
  path and leave it — do not discard work to tidy up.
- **Only worktrees this run created.** A `/tmp/corgi-wt/` dir from an earlier session may
  still be someone's working state.
- **Blocked story → keep its worktree.** Nothing was pushed; removing it destroys the
  only copy.
- Removing the worktree deletes the `node_modules` **symlink**, never the main
  checkout's real directory.
- Leftovers are cheap to spot later: `git -C <dir> worktree list` shows every one still
  registered, and a stale entry whose dir is gone clears with `worktree prune`.

---

## Phase 6 — Report

**The per-story lines above are the report's content — render them, never a prose
write-up around them.** A findings essay is not a hand-back: nothing in it can be
pasted anywhere.

Hand the batch back in the **`summary`** skill's shape: one block per ticket, a table
row per repo, the PR/MR as a markdown link, the state carrying its nuance (`MERGED ✓`,
`OPEN — auto-merge armed`, `OPEN — 2 unresolved threads`). Re-read each PR/MR and ticket
live rather than reporting what this run did to them — auto-merge fires and pipelines
turn red between the push and the report, and the stale row is always the one that
matters.

Lead with anything still needing the user: a blocked story, a missing credential, a
manual job to play, an open question. Then the tables, then the tracker roll-up. Include
what the Phase 5.5 review caught in this run's own work, one line each — that is the
highest-signal part and the easiest to quietly drop.

Never wrap it in a fenced code block: it renders monospace and kills every link.

**Announce in chat, once, when PRs/MRs were opened** (after Phase 5.6, so a flipped PR
is announced ready): run
`corgi agent chat announce "<[KEY] story title>" <pr-url> <pr-url> …` with every PR/MR
this run opened (one call per story). It posts the title and one `repo: link` line per
PR in the workspace's review channel, in the voice the workspace configured; with no
review channel configured it prints one line and posts nothing — do not work around
that, and never post with `chat post` yourself. Skip it for a story that opened
nothing, and for a re-run whose PRs were announced before.

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
