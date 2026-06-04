---
name: stories
description: Use when the user wants to ship work across a corgi-compose workspace — EITHER a batch of tracker issues (Linear or Jira links/keys like ABC-123; "do these stories", "implement these tickets", "ship X and Y") OR a free-text feature description with no ticket ("build a feature that …", "add X across the services"). Investigates once, writes a spec per item behind one sign-off gate, branches per service (paths from corgi-compose.yml), tests + reviews each, opens draft PRs/MRs on GitHub or GitLab, and can create a tracker issue from the approved spec. NOT for authoring/running corgi-compose itself (use the corgi skill) or a trivial one-line edit you'd just make directly.
---

# Corgi stories

Work items — tracker issues (Linear/Jira) **or** a free-text feature description
— → spec each → isolated branch(es) → tested + reviewed code → **draft** PR/MR
per repo → grouped report. Services, dirs, dependency order: all from
`corgi-compose.yml`. Never hard-code them.

## Speed model

Lighter than a full multi-agent pipeline. **One blocking gate** on the
adjustment/bug fast path; complex stories add superpowers checkpoints on top.

- **Gate (blocking): spec sign-off** (Phase 2). Confirm intent before any branch
  spawns. Cheap, guards whole batch at once. Never skip.
- **No final gate.** Draft PR/MR instead: push, open draft, scoped review, report
  diff + link; human flips to *ready*. Draft = no CI/notify, reversible.

### Story tiers — set per story (Phase 1), drives rigor

| Tier | What | Extra rigor |
|------|------|-------------|
| **Adjustment** | clear-spec UI/copy/flag/config; unambiguous | just a test for the new behaviour |
| **Bug** | broken / regressed | regression test **FAILS on base branch** before fix, passes after |
| **Feature** | new behaviour, real design, or new/changed cross-service contract | hand to **superpowers** if installed, else equivalent inline (below) |

**Tier ≠ span.** Complexity axis vs single/multi-service (Phase 4). Multi-service
adjustment is still an adjustment. Most stories = adjustments → fastest path.

### Complex story → superpowers

Bigger than adjustment (real design, unclear approach, large surface, new
contract) → don't force one-shot. If **superpowers installed**, via `Skill`:
- `superpowers:brainstorming` — settle intent + approach before code.
- `superpowers:writing-plans` — becomes the story's spec doc.
- `superpowers:test-driven-development` + `superpowers:executing-plans` — build,
  tests first.
- `superpowers:verification-before-completion` — prove before draft PR.

Not installed → equivalent inline: settle approach with user, write plan into
spec, tests first, verify before push. Either way flows back through this skill —
same spec doc, one gate, per-story review, draft PR, grouped report.

## Guardrails (non-negotiable)

- **Never touch `manualRun` services/db_services.** Reference-only — corgi
  doesn't start them, this flow doesn't change them. Fix lands there → STOP, flag
  as out-of-band.
- **Draft PRs/MRs only.** Never non-draft, never merge, never force-push.
- **One blocking gate** — spec sign-off (Phase 2); the sign-off *is* the branch
  authorization.
- **No destructive git without explicit OK** — checkout off a dirty tree, branch
  deletes, force-push, pushing shared branches.
- **Don't push the workspace meta repo** unless asked — only service branches.

## Optional tooling (degrade gracefully)

Stands alone. Used if present, never required:
- **`superpowers:*`** (separate plugin) — complex-story engine + nicest review.
  Missing → do it inline. Don't block on a missing plugin.
- **A code-review command** (e.g. `/code-review`) — Phase 3.5 if you have it;
  else a review subagent works everywhere.

Always available, all the flow needs: `git`, `gh`/`glab`, `corgi`,
`Explore`/`Task` agents, tracker MCP.

---

## Phase 0 — Read workspace from corgi-compose.yml

1. Locate it (`ls corgi-compose.yml *.corgi-compose.yml`). None → `/corgi-new`
   first, or ask which repos; don't guess a layout.
2. **Read the yaml, extract only needed keys** —
   `services.<name>.{path,cloneFrom,manualRun}`, `depends_on_services`,
   `exports` (schema: `skills/corgi/references/yml-schema.md`). Don't render the
   whole project: `/corgi-describe` is a full doc (too many tokens here), and
   `corgi --describe` doesn't short-circuit (it dumps JSON then still runs the
   command). Build:
   - **Service → dir map.** `path:` (local) or `cloneFrom:` (clone target) = the
     repo you branch in. `cloneFrom` not on disk → `corgi init` clones it first.
   - **Dependency/order graph** from `depends_on_services` + `exports`/
     `${producer.VAR}`. Depended-on service (schema/contract owner) goes first;
     consumers follow. Cycles → flag.
   - **manualRun set** → exclude (Guardrails).
3. **Per repo: forge, base branch, commands.**
   - Forge: `git -C <dir> remote get-url origin` → `*github.com*` = `gh`;
     `*gitlab*` = `glab`. A batch may span both.
   - Base: `git -C <dir> symbolic-ref --short refs/remotes/origin/HEAD` (or
     `gh repo view --json defaultBranchRef -q .defaultBranchRef.name` /
     `glab repo view`). This is `<base>` for branch, red test, PR target.
   - Test/typecheck/lint/build: discover from `package.json` scripts, `Makefile`,
     `pyproject`/`go.mod`, and the service's `start`/`beforeStart`/`scripts`.
     Don't assume a runner.
4. **Detect tracker.** `linear.app` URL → Linear (`mcp__linear-server__*`).
   `atlassian.net`/Jira project → Jira (`mcp__atlassian__*`;
   `getAccessibleAtlassianResources` for sites). Bare key + both connected → ask.

## Phase 1 — Investigate (once), then spec

**Tracker issue:** fetch, **view screenshots**, read real code paths.
- Fetch: Linear `get_issue`; Jira `getJiraIssue`.
- Screenshots: Linear = `curl` the `uploads.linear.app` URLs (signed, expire
  ~5 min — re-fetch issue for fresh URLs) then read. Jira = `getJiraIssue`
  returns attachment metadata, not image bytes — fetch the attachment
  (`mcp__atlassian__fetch` / its URL; may need auth) then read.

### Free-text feature (no ticket) — locate work first

Description, not links → no fetch, nothing says *where* code goes. Find target
service(s) before speccing:
1. **Map intent → service(s)** from `corgi-compose.yml` (names, paths,
   `depends_on_services`) + the **README next to the compose** + per-service
   READMEs (they say what each service does). Don't guess.
2. **Confirm with `Explore`** scoped to candidate service(s) — find the real
   files.
3. Genuinely ambiguous service → spec-gate question (ask, or
   `superpowers:brainstorming`); don't guess.

Described feature = usually **Feature tier**: `superpowers:brainstorming` (or
inline Q&A) to settle scope → `superpowers:writing-plans` for the spec. After
sign-off (Phase 2), **offer to create a tracker issue** (Linear `save_issue` /
Jira `createJiraIssue`) for a key + auto-link; declined → spec stays local + on
PR, branch drops the key segment (Phase 3).

### Investigate once — don't re-research

Batched stories overlap. Re-exploring per story doubles tokens. So:
1. **Cluster** by **service + area** before dispatching.
2. **One `Explore` sweep per area, not per story** — all that area's questions in
   one agent. Never per-story over the same files.
3. **Orchestrator = the cache.** Subagents can't share context mid-flight: scope
   sweeps to not overlap, collect each into one **investigation note** (scratch —
   memory or a gitignored file), specs reference it. Read the map, don't
   re-explore.
4. **Reuse ledger** — shared components/contracts recorded once; stories cite,
   don't re-derive.

### Write the spec — every story

`docs/stories/<issue-key>-<slug>.md`, actionable or not:
- Problem (quote issue) + **which services** (drives branch/PR count).
- **Tier** — adjustment/bug/feature.
- Root cause / current behaviour, `file:line` refs.
- Change plan (snippets) **grouped by service**, tests, manual verification,
  risks. Multi-service: `## Contract` + cross-service order.

### Triage: actionable vs blocked — controls POSTING, not writing

- **Actionable → post.**
  - **Spec → a comment** on the issue (human-readable, not a `.md` attachment).
    Linear `save_comment({ issueId, body })`; Jira `addCommentToJiraIssue`.
    Literal newlines / markdown; re-run with the returned comment id to update,
    not duplicate.
  - **What to test → a separate comment** (non-engineer reads inline). Plain QA:
    clicks + outcome, no code/file refs, end `Expected:`. Skip non-testable stories.
- **Blocked → do NOT post.** Spec local only; mark `Status: BLOCKED` + **Decision
  needed**; surface the choice to the user. Hold it; rest of batch proceeds.

`superpowers:brainstorming` / `superpowers:systematic-debugging` (if installed) to
resolve ambiguity before blocking.

## Phase 2 — Gate: spec sign-off (the one blocking gate)

Present all actionable specs in **one round**; sign-off before any branch —
batch-level, not per-branch. Re-present only changed specs. Blocked held out.
Superpowers-escalated stories pass here too: their `writing-plans` output is the
spec.

## Phase 3 — Branch + implement + verify per story

Branch: `feature/<issue-key>/<kebab-slug>`, same name in every affected repo.

**Get `<issue-key>` from the tracker, don't invent it** (it's the auto-link
token):
- **Linear** — `get_issue` → `identifier` (`ABC-123`) + suggested `gitBranchName`.
  Use `identifier`; Linear links any branch containing it (case-insensitive). (Or
  use `gitBranchName` verbatim.)
- **Jira** — `getJiraIssue` → `key` (`PROJ-123`). Jira dev panel / Smart Commits
  link by that token.
- **No ticket** — `feature/<kebab-slug>`, no key segment (or the key of an issue
  you created in Phase 1).

Key also goes in the commit + PR/MR title (Phases 4–5). Same branch name across
repos so multi-repo PRs group.

**Pick branch vs worktree per repo — check the working tree first:**
`git -C <dir> status --porcelain` — empty = clean, any output = dirty.

| Repo state | Stories touching this repo | Mode |
|------------|---------------------------|------|
| **clean** | one | **branch in place** |
| **dirty** | one | **worktree** — don't disturb the user's uncommitted work, and skip the destructive base checkout/pull on a dirty tree |
| any | several | **worktree per story** (parallel isolation) |

- **Branch in place** (clean tree only). Update base, then branch:
  `git -C <dir> fetch origin && git -C <dir> checkout <base> && git -C <dir> pull --ff-only && git -C <dir> checkout -b <branch>`.
- **Worktree** (dirty tree, or several stories in one repo). Branch straight off
  `origin/<base>` — never touches `<dir>`'s working tree, so uncommitted work
  stays put:
  ```bash
  git -C <dir> fetch origin
  git -C <dir> worktree add -b <branch> /tmp/corgi-wt/<issue-key> origin/<base>
  # deps dir (node_modules / vendor / target) gitignored → symlink main checkout's
  # for SEQUENTIAL runs; real install for CONCURRENT runs.
  ln -s "$PWD/<dir>/node_modules" /tmp/corgi-wt/<issue-key>/node_modules
  ```
  Implement / test / commit / push / open the PR/MR from the worktree dir, then
  `git -C <dir> worktree remove /tmp/corgi-wt/<issue-key>` once the PR is up.

Implement to spec; reuse before building. **Minimum diff — no opportunistic
refactor, no over-engineering, no code comments** unless the file already
comments heavily. Run the **per-service gate** (tests + typecheck + lint) BEFORE
commit. Tests for every change, matching existing patterns.

- **Bug tier: red test first** — write it, confirm it **FAILS on base**, then make
  it pass. Adjustments skip.
- **Multi-repo consumer:** can't verify (codegen/typecheck) until its producer is
  committed **and running** — do Phase 4's contract-owner-first step (start
  producer, `corgi status --ready`) BEFORE running this gate on the consumer.
- **Stop rule:** can't pass the gate after ~2 honest tries → STOP, leave
  un-pushed, report `needs attention` + failure, rest ships. Never push red.
- **Re-tier mid-flight:** adjustment reveals real design → STOP, bump to feature,
  hand to superpowers. If it also **widens span** (now needs another repo or a
  new contract), loop back to Phase 1–2 — re-spec (add `## Contract`), re-gate,
  create the producer branch — don't escalate in place.

## Phase 3.5 — Per-story review (scoped, right after gate is green)

Review **each story as it finishes**, scoped to **only its diff** — incremental,
bounded context, NOT one giant end-of-batch review (re-reads everything, burns
tokens).
- Review `git -C <branch-dir> diff <base>...HEAD` — via a review subagent passed
  only that diff + the spec (works everywhere), or `/code-review` /
  `superpowers:requesting-code-review` if you have them.
- Fix **blocking** findings (correctness, missing test, scope creep), re-run gate.
  Cap ~1 extra round; still blocked → Stop rule. Non-blocking → PR body.

## Phase 4 — Commit, then multi-repo ordering

**Commit:** match the repo's `git log` style (Conventional prefix only if the
repo already does). **Concise subject** + the **issue key**; body only if truly
needed, never a wall. **No `Co-authored-by` / AI trailer.** Let pre-commit hooks
format; re-stage if rewritten.

**Multi-service** (one issue → N repos → N PRs):
- **Same branch name** in every repo.
- **Contract owner first.** Consumer regenerates types/clients from a producer
  (GraphQL codegen, OpenAPI, protobuf, shared schema)? Producer must be
  implemented, committed, AND **running** before the consumer verifies:
  ```bash
  corgi run --services <producer> --with-deps --detach   # or: corgi run --detach
  corgi status --ready --service <producer>               # block until healthy
  ```
  Until up, consumer's generated types are stale → won't typecheck. Producer from
  the `depends_on_services`/`exports` graph (Phase 0). `corgi stop` when done.
- Consumers regenerate, commit generated output, finish their slice.
- **Merge order:** producer PR first, consumers after. State in spec + every PR
  body.

## Phase 5 — Push + draft PR/MR per repo

Per repo, forge from Phase 0. Run `gh`/`glab` **from inside `<dir>`** (they read
the repo from cwd; `git -C` only sets git's dir).

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
- **Cross-link** siblings + merge order in each multi-repo PR/MR body.
- Canonical spec already on the tracker (Phase 1); PR/MR comment is a convenience
  copy.

### Grouped report (final output)

One line per PR/MR: `[<issue-key>] <Service>: <PR/MR title>`, link on its own line
directly below. **No `(draft)` suffix** on the title — draft status is implied.
No-ticket story → drop the `[<issue-key>] ` prefix. Multi-repo story repeats the
line per repo (same key). Blocked → one-line question instead of a link.

```
[ABC-123] web: Remove address step from mobile signup
https://github.com/<org>/<repo>/pull/<n>

[ABC-124] api: Add rate limit to login
https://gitlab.com/<group>/<repo>/-/merge_requests/<n>
[ABC-124] web: Add rate limit to login
https://gitlab.com/<group>/<repo>/-/merge_requests/<n>
```

---

## Scenarios & scaling

- **Big batch → bound context.** >~4–5 stories → dispatch per-branch
  implementation to subagents (`superpowers:subagent-driven-development` /
  `dispatching-parallel-agents`), one per branch, scoped to its spec + the shared
  note. Orchestrator stays gate-keeper, collects reports + reviews. Chunk a huge
  batch.
- **Concurrent same-repo test runs** need a real install per worktree, not the
  symlink (build caches race).
- **One blocked story never blocks the batch** — held aside; actionable ships;
  blocked surface as questions.
- **Mixed forges/trackers in one batch** fine — resolved per repo / per issue
  (Phase 0).
