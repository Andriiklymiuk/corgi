---
name: review
description: "Use to review EXISTING pull/merge requests: \"review this PR/MR\", \"code review <link>\", a bare PR link. Also to address feedback on your OWN: \"fix the comments on this MR\". Works from a saved PR when the forge is down. NOT creating PRs (stories) or the local diff (/code-review)."
---

# Corgi review

**Done when:** someone else's PR/MR → review posted (summary + inline), approved when nothing blocks (Approve rule in Guardrails), link in reply; your own → valid comments applied, tests pass, branch pushed, every thread has reply (resolved where forge allows). Either way, when corgi tracks PR/MR or its ticket, end with `corgi agent watch done <link or KEY>` so inbox row closes this side (review posted through `corgi agent watch pr` does it itself).

Review one or more existing remote PR/MR(s) on GitHub or GitLab against each repo's standards (CLAUDE.md/AGENTS.md, lint and format config) plus intent from linked Linear or Jira ticket, then post human-readable summary comment and inline line-level suggestions on each PR/MR. Someone else's PR: post without asking. Your own: fix and push instead. Services, dirs, forges resolve from `corgi-compose.yml`.

Read `../_shared/conventions.md` first.

## Two modes - route from the verb

- **A · Give review** (default) - *review / look over / check* a PR, pasted PR/MR link, "thoughts?". Post summary + inline suggestions; **comments only, never touches branch.** Phases 0-6 below.
- **B · Address review** - *fix / address / answer / respond to* comments on **your** PR ("fix the comments on this MR", "answer the review", "address the feedback for story ABC-123"). Read incoming threads, apply valid ones, reply + resolve, **push fixes.** See *Mode B* near end.

Ambiguous ("check my PR for story X") → ask which. Story-id with no link → resolve to its PR like `tracker`. **Every review in session routes from scratch**: second "review these" after Mode B run on your own PRs is Mode A on someone else's - re-resolve author (P4), re-read this routing, never carry last run's mode. **Bare "fix this PR" / "fix them"** (no mention of comments) → default **Mode B**; but if PR has **no open human threads and CI red**, problem is build, not comments → hand to `debug` (Step 5), not comment fix.

---

**Mode A - give review. Phases 0-6:**

## Phase 0 - Resolve target(s)

Input = `$ARGUMENTS` (command) or pasted message (skill): one or more PR/MR references, each **URL** or **bare number**.

**URL parse:**
- `github.com/<org>/<repo>/pull/<n>` → GitHub, tool `gh`.
- `<host>/<group>/<proj>/-/merge_requests/<n>` → GitLab, tool `glab` (incl. self-hosted hosts, not just gitlab.com).

**Bare number** → infer forge + repo from cwd `git remote get-url origin` (same detection as stories P0: `*github.com*` → `gh`, `*gitlab*` → `glab`). cwd not repo / ambiguous → ask. **Bare numbers all resolve to cwd repo** - fine for same-repo batch; multi-repo set: require URLs or confirm inferred repo per number.

**Verify tooling:** check matched CLI installed + authed (`gh auth status` / `glab auth status`). **Self-hosted GitLab** ref: check auth against *that host* (`glab auth status --hostname <host>`) - healthy gitlab.com token doesn't mean internal instance configured. Missing → stop with host-specific install/auth hint; don't guess.

**Forge unreachable, change on disk.** Missing, unauthed or offline CLI is stop only when change nowhere local. When user says forge down and points at PR on disk - head as local branch or saved tree, `pr view` output, patch - review that: base and author from saved metadata (else `origin/HEAD`), diff from `git diff <base>...<head>`, head read with `git show <head>:<path>` or detached worktree, P2-P3.6 as written - they need tree, not forge. Print review in P4 shape under first line `not posted: <forge>
unreachable`, post nothing. Mode B same way: asks come from saved threads or user's own words, fixes go on PR's own branch, gate, commit, push when `origin` takes it, report gives each unposted reply with its text.

Set may **span forges** (e.g. api MR on GitLab, web PR on GitHub) - resolve each ref independently. Both forges first-class.

**Group by repo/service up front.** Build repo → [PRs] map. Multiple PRs to same repo share one standards note (P2) and one area sweep (P3) - core token saving.

**Auto-detect siblings (optional).** Given single PR/MR, read branch name; if `corgi-compose.yml` in cwd, resolve each other (non-`manualRun`) service's repo (P2 mapping) and enumerate same-branch PRs/MRs there: `gh pr list --head <branch> --repo <o>/<r>` / `glab mr list --source-branch <branch> -R <repo>` (commands in `../_shared/forge-commands.md` §1). stories opens sibling PRs with same branch name, so this finds rest of story's set. Found → confirm expanded set with user before reviewing. **Never auto-expand silently.**

## Phase 1 - Fetch (no checkout)

Per PR/MR, fetch **without checking out branch** (non-destructive - never touch user's working tree). Exact commands in `../_shared/forge-commands.md` §1; pick `gh` or `glab` column matching ref's forge.

Fetch per PR/MR - **metadata + anchoring SHAs + commits in one call**, diff separately (no second call just for SHA):
- GitHub: `gh pr view <n> --json title,body,author,baseRefName,headRefName,state,isDraft,files,url,headRefOid,baseRefOid,commits,statusCheckRollup`.
- GitLab: `glab mr view <n> -R <repo> -F json` - JSON already includes `diff_refs` (`base_sha`/`head_sha`/`start_sha`), `state`, `draft`, `commits`, head `pipeline` status.
- Unified diff (raw, §0) - `gh pr diff` / `glab mr diff`.
- Inline anchoring later (P5): GitHub uses `path`+`line`+`side`; GitLab uses native `--file`/`--line`/`--old-line` (no `diff_refs` needed for primary post path).
- **CI status** - read on fetch (GitHub `statusCheckRollup`; GitLab head `pipeline.status`, or `glab ci status -R <repo>`). Cross-check for P3.6: **green** pipeline contradicts any "this fails to build/test" finding, so verify before posting. Pipeline green **only because failed job is `allow_failure`** ("passed with warnings") hides real red job - list jobs (`../_shared/forge-commands.md` §1), see which finding it confirms. **Read the red, not the colour.** Red check that died before tests ran (version gate, install, lockfile check) means PR has **no test or coverage signal at all** - blocking line on its own, naming step that killed it, not footnote. And `git merge-tree --write-tree origin/<base> <head>` from evidence worktree: conflict - usually version field both sides bumped - blocks merge whatever checks say.

Fetch **reviewable diff raw** - output filter on `gh`/`glab` (rtk or similar) truncates and degrades review; `../_shared/forge-commands.md` §0 has bypass.

**State.** Read PR/MR state on fetch.
- **Merged or closed → warn and ask** before reviewing (usually pasted by mistake); skip by default if declined. Unattended (`../_shared/conventions.md`): skip with one line, no question.
- **Already approved by current user** (approvals list names them) → review only what landed after that approval (commits newer); nothing new → one line in report, no comment on PR. Unattended run skips outright.
- Draft → review normally (reviewing drafts is common case).

**Stacked PRs.** `gh pr diff` / `glab mr diff` diff each PR against own base. If PR B's base is PR A's branch, B's diff already just own delta - review as-is. If both target trunk and B's `commits` (from P1 fetch) contain A's, isolate B's changes with **compare API - no checkout** (`gh api repos/<o>/<r>/compare/<A-head>...<B-head>`; never `git diff A..B` on tree you didn't fetch). Can't isolate cleanly → review full diff, note double-review. State stacking in report.

**Existing discussion.** List PR/MR's current review threads/comments on fetch (`../_shared/forge-commands.md` §5 read commands work for Mode A too - read only, no reply). Needed twice: to **dedup** (P5 marker skip) and, more important, **stay relevant** - point human already raised, author already answered, or anything on **resolved** thread is not fresh finding. Carry thread list into P3.6 prune pass. Don't re-litigate settled threads.

**Evidence worktree (read-only).** Diff is where findings anchor; evidence lives in tree around it. Put head SHA in throwaway detached worktree - never user's checkout, never branch. Detached worktree has **no installed dependencies**, cannot run test or probe as-is. Decide once per repo whether hunt needs to run anything; if yes, install into **one** copy per repo up front, not each subagent rebuilding own - otherwise half the set verifies by running, half falls back to trusting CI. **Never borrow another checkout's installed deps when PR touches manifest or lockfile** - you'd test dependency tree PR just changed, and version bump often the thing under review. Can't install → say so in report, let CI carry test signal; probe on wrong deps worse than no probe:
```bash
git -C <dir> fetch origin pull/<n>/head            # GitLab: merge-requests/<iid>/head
W=$(corgi worktree add <service> --detach FETCH_HEAD)   # inside the workspace, so the repo's CLAUDE.md loads; same SHA → same dir
# repo not in corgi-compose.yml, or no local clone - /tmp, and read its CLAUDE.md / AGENTS.md yourself first:
W=/tmp/corgi-review/<repo>-<n>; [ -e "$W" ] && W="$W-$(date +%s)"   # leftover: new path, never rm -rf
git init -q "$W" && git -C "$W" fetch -q --depth 50 <clone-url> merge-requests/<iid>/head && git -C "$W" checkout -q --detach FETCH_HEAD
```
Grep callers, open dependency source, run coverage gate or probe there; `git worktree remove` it in P6 (no `--force`, no `rm -rf` - both refused; clone in `/tmp` left for OS). Remote-only repo → `git init` + fetch line above. Without it, review stays inside hunk and misses what human reviewer next to you finds (P3, evidence sweep).

**Noise filter.** Drop generated/vendored/binary paths from review surface: lockfiles (`*.lock`, `package-lock.json`, `go.sum`, …), `vendor/`, `node_modules/`, codegen output, images/binaries, anything matching repo's `linguist-generated` / `.gitattributes` markers. Also catch **unmarked** codegen: common globs (`*.pb.go`, `*_gen.go`, `*.generated.*`, OpenAPI/GraphQL client dirs) + header-sentinel scan (`@generated` / `DO NOT EDIT` in first lines). Never comment on these; note count skipped. **But** if lockfile changed, add one report line - "N dependency changes in `<lockfile>` - not line-reviewed" - so risky transitive bump isn't invisible.

## Phase 1.5 - Tracker enrichment (intent)

Scan **input**, **each PR/MR body**, AND **branch name** (`headRefName`/`source_branch` - e.g. `feature/ABC-123/...` carries `ABC-123`) for ticket references: `linear.app/…`, `atlassian.net/…`, or bare `ABC-123` key. **Dedupe keys across whole set first** - shared ticket (common api+web case) fetched **once**, intent note reused by every PR referencing it, same as P2's per-repo note. Never re-fetch same key per PR.

- **Linear** → `mcp__linear-server__get_issue` (+ comments); view screenshots by `curl`-ing signed `uploads.linear.app` URLs (expire ~5 min - re-fetch issue for fresh URLs) then read.
- **Jira** → `mcp__atlassian__getJiraIssue` (+ comments). Returns attachment metadata only, no MCP tool returns bytes (`../_shared/tracker-mcp.md`): with `$JIRA_EMAIL`/`$JIRA_API_TOKEN` in env, `curl -u
  "$JIRA_EMAIL:$JIRA_API_TOKEN" -L -o <file> "<content-url>"` then read; otherwise ask user to download and share file. `getAccessibleAtlassianResources` for site if needed.

**Extract whole intent, not just acceptance criteria** - tickets carry the why:
- Description + acceptance criteria.
- Design rationale - why approach chosen, decisions, trade-offs.
- Constraints - deadlines, compat requirements, "must not touch X", perf/security bars.
- Discussion/comments - later clarifications override original ask.
- Linked specs/docs + sub-tasks - follow one hop for design context.
- **Design screenshots / Figma links on UI ticket = visual acceptance bar.** View them, check **diff** against them - layout values, spacing, colour, icons, copy in code are what you compare. Screenshots PR **does** carry are evidence: ones visibly diverging from design are **finding**. PR with **no** screenshots is **not** finding, **not** reason to withhold or condition approval, **not** line in summary - screenshots not owed in this flow; review code against design, ask for screen when diff genuinely can't settle visual question (name which one). **No design on ticket, or it explicitly waives one → not finding:** review UI on standards alone; don't demand design never promised. On **your own** PR (Phase 4 fix path) screenshots yours to add when they help or user asked - **`before-after`** skill (base built, same screen captured twice) is tool for restyle, puts them where user said: PR when named, else ticket. Body whose image links are `raw.githubusercontent.com` on private repo, or local paths, renders empty boxes - `nit` with fix (`../_shared/forge-commands.md` §6a: assets branch + blob `?raw=true`), not visual proof until they render.

Distill into compact **intent note** review uses two ways in P3:
1. **Check** - does diff do what ticket asked?
2. **Temper** - choice ticket explicitly justifies (deliberate hack, scoped approach, known debt with follow-up) is not bug; drop finding or downgrade to soft note citing ticket's reasoning.

No ticket linked → skip; review on repo standards alone. Key found but **no tracker reachable** (no MCP loaded, no credentials, `corgi agent` not watching that project) → review on standards alone, say so in one report line ("ticket ABC-123 not read: no tracker access"); never let skipped intent note pass as read.

Ticket resolves but **clearly unrelated** to diff (stale/recycled key in branch or title) → treat as no-ticket: review on standards alone, add one-line summary note about mislink - tracker automation binds PR to wrong story until key fixed.

## Phase 2 - Standards note (once per repo)

Build **one compact, distilled** standards note **per repo** - same-repo PRs share it, never rebuild. Note is orchestrator's cache (stories model): handful of bullets reviewer reads, **not** raw files dumped into context.

**Orchestrator writes this note, and it is ONLY standards input the review of each PR gets. That review never reopens `CLAUDE.md`, `AGENTS.md` or lint config itself** - that is whole saving; N same-repo PRs otherwise re-read same file N times. Skipping P2 and putting "read the repo's CLAUDE.md" in each prompt is failure this phase prevents.

Read canonical file (`CLAUDE.md`/`AGENTS.md`) plus one lint config matching diff's languages - nothing else, headings only above ~400 lines. No CLAUDE.md → note is "lint config X + conventions observed in the diff", fine; don't manufacture standards nobody wrote.

**On-disk service (repo is local corgi service)** - map repo → service via `corgi-compose.yml` (`path:`/`cloneFrom:`), read from service dir, reuse README one-liner for "what this service does". **Never map or read `manualRun` services** - reference-only, same as stories.

**Remote-only repo (not on disk)** → best-effort API fetch of just `CLAUDE.md`/ `AGENTS.md` + one relevant lint config (`gh api repos/<o>/<r>/contents/<f>` / `glab api`). **No full clone.** `404` = genuinely absent → skip. `403`/auth error ≠ absent → note "standards skipped (no access)" so private repo doesn't silently get weaker review.

**Monorepo with >1 service** → still read each canonical file once, but **scope note per service area** (keyed by compose path prefix, e.g. `api/` vs `web/`) so `web` convention isn't applied to `api` code.

Distilled note = naming conventions, test patterns, forbidden patterns, code-style rules repo **explicitly** spells out. Team has an installed review or conventions skill for this repo (`../_shared/conventions.md` › Team skills) → read it once here, its rules go into same note. Target short note (~dozen bullets), reused unchanged across every PR in repo.

## Phase 3 - Review each PR (this session, one at a time)

Review in this session, one PR after another. **No subagents unless the person asked for them in this conversation**; an unattended run never fans out. Unasked parallel agents are what people stop this skill for.

Each review works from: PR's diff + title/body + repo's standards note + (if any) intent note from P1.5 + evidence worktree path (P1). Findings **anchor on diff**; **evidence comes from whole tree** - every symbol, key, test diff touches has callers, twins, consumers outside hunk, where bugs a good human reviewer finds live. Untouched code not reviewed for own sake; read to judge touched code.

**Surface first, then diff.** Before any implementation line, read changed surface - PR body's `## Changed surface` section when author left one, else make it: `corgi surface --base <base>` from checkout (or `corgi_diff` with `surface: true` over stack). Lists exported symbols, routes, contracts, migrations, config that changed, removals and signature changes marked breaking. Every breaking line is question review must answer - who calls it, is caller in this PR or another, does ticket ask for it - before opening diff. Change whose surface is "nothing public changed" reviewed for behaviour and tests only. Summary names surface - each changed symbol, route or key by name, `breaking` where it is - never "the changes listed in the body"; reviewer reading only that line should know whether to look closer.

**Hunt for:**
- Correctness bugs.
- Missing or weak tests.
- Security issues.
- **Comments the code does not need** - see rule below.
- **Overengineering** - abstraction with one caller, config knob nobody asked for, interface for single implementation, layer that only forwards. Flag, name simpler shape. Other four shapes of too much code, each `nit` with replacement in `suggestedReplacement` and line count in title: **hand-rolled what the language ships** (date/URL/sort helper stdlib has), **hand-rolled what the platform ships** (picker, modal, validation, constraint browser, framework or database provides), **new dependency for what an installed one covers** (check lockfile), **dead flexibility** (parameter, branch or option nothing in diff uses). Ladder `stories` skill builds by is `stories/references/smallest-change.md`; PR body's `Deferred` list says which shortcuts deliberate - deferred item with no trigger is finding, deliberate one with trigger is not.
- **Complexity regressions** - run `complexity` skill in `report` mode on diff. Touched function whose cyclomatic complexity rose, or new function over repo's threshold (own linter config, else 10), is finding: `nit` by default, `blocking` over 15 or in hot path (cost rule below). Finding carries number before and after and tactic that lowers it (guard clauses, extracted function with what-not-how name, lookup table, named predicate). Suppression added to pass linter's complexity rule is finding on its own.
- **Risk score** - run `risk` skill in `assess` mode on diff (Phase 1 evidence table reuses this PR's fetch; no second checkout). First line of card - `Risk N/10 - <tier> · <what the reviewer does>` - becomes summary's headline (P5), `auto-approve: yes|no - <reason>` its last line. Card's **Check** items are reading order for rest of hunt: floor it hit (auth, payment, migration, secret, native module, CI) is where blocking findings will be. Offer `stamp` (write card into description) at P4 gate; never stamp without it.
- **Performance footguns in hot path** - work repeated per item that could be done once, unbounded read or scan, per-request shell-out or file walk, network call with no timeout, goroutine nothing stops.
- **Leaked secrets** - always `severity: blocking`; flag file + line + that secret present; never echo value into finding or comment.
- Repo-standard / convention violations (names, patterns, style).
- Scope creep (changes outside ticket's stated scope).
- **User-facing copy naming internal tech** - string/label exposing engine, library, vendor, infra detail to end users (`nit`); suggest selling user benefit, not mechanism. Skip if ticket is about that copy.
- Ticket-intent mismatch (diff doesn't do what ticket asked).
- **Second-reviewer kinds** evidence sweep below doesn't cover - `../_shared/second-reviewer.md` §1 two steps that should be one, §2 old world still running, §5 personal data, §6 what person sees when it fails, §7 delete means all of it, §10 house rules and unasked product values. Each finding is scenario; cheap one proven with probe spec failing on head.

### Evidence sweep - follow every change out of the diff

Run before any style point, per changed symbol, key, route, flag, schema field, test. Shapes, greps, failure scenarios: `references/evidence-sweep.md`. Item is finding only when grep or read shows it; none is checklist to recite back.

1. **Twins and callers.** Grep tree for changed symbol. Read path that gained field, context or filter while write / enforce / validate twin did not (or reverse) is `blocking`; so is policy applied where row written but never where used, when second path into same gate re-checks it. **New guard has no callers to grep - its twins are siblings of code that got it.** Limit, check or permission added to one handler: list handlers built same way (same file, same first lines, same route group), say which did not get it. Left out with no reason in PR body is finding.
2. **Config delivery.** New env var, secret or flag: name where each deployed environment gets it - deploy workflow, task definition, chart, secrets list - with anchored grep (`(^|[^A-Z_])KEY`; substring match hides absence). Declared only in `.env.example` / CI env = unreachable in production = fallback default ships; say what default does there. `blocking`.
3. **Library premise.** When correctness rests on dependency behaviour (version comparator, safe-area provider, error wrapper, ORM's null semantics, reconciler), open dependency source at locked version in worktree, or run probe. PR body's claim about library is hypothesis.
4. **What the tests assert.** Per test in diff: exercises behaviour criterion names, or literal - decorator metadata, mock's call args, module under test mocked away? Stays green with bug put back? Run repo's coverage gate on head worktree: component fix rests on at 0% functions is `blocking`.
5. **Deployed contract.** Consumer reading new field: does *deployed* producer have it (base branch's schema file, staging introspection)? Real order is deploy mechanics - auto-published OTA or CD-on-merge versus manual dispatch - not merge order body states.
6. **Claims the diff makes false.** Doc, `.env.example` line, code comment or CLAUDE.md sentence now stating wrong count, default or behaviour (`corgi docs check` names file). `nit`, or `blocking` when it describes safety property ("refuses to boot without X" when it only warns).
7. **Edges the happy path hides.** Timestamp possibly in future, `??` on value that can be `""`, optional relation with live fallback, transient error treated as absence, hand-copied list nothing keeps in sync, input fanning out exponentially - measure last one, put number in finding.
8. **Failure domain and structure.** Non-critical component now wrapping critical one; wrapper whose element type flips between branches (subtree remounts); shared document or query two features now depend on together.

### Comments: the bar is high

Comment beside code is cost - goes stale, repeats code, most common thing generated diff adds too much of. **Default is no comment.** Rare enough that PR adding several is already finding.

Flag as `nit` (with deletion as `suggestedReplacement`) any comment that:
- restates code (`// loop over users` above loop over users),
- narrates step (`// now save it`), or names what function already names,
- documents parameter or return signature already states,
- is section banner, TODO with no owner, or commented-out code.

Leave alone - these earn place:
- **why**, not what: non-obvious constraint, rejected alternative, bug this shape prevents,
- real gotcha next reader would otherwise reintroduce,
- exported identifier's doc comment where language expects one,
- link to spec, ticket, or upstream issue explaining shape.

Judge same way in Mode B: when addressing feedback, don't add explanatory comments to justify change - change should read for itself.

### Simplicity and cost

Two questions on every non-trivial diff, before any style point:

1. **Is simplest thing that works being done?** More types, layers or options than change needs is finding, not neutral. Name smaller version in explanation.
2. **What does this cost when it runs?** Look where code actually sits - per-request handler, poll loop, per-item body. Repeated work, unbounded scan, missing timeout, leaked goroutine there is `blocking`; same in one-shot command is `nit`.

3. **Does it read without help?** Comment narrating code, name needing body to be understood, copy of helper existing two files away, option with one caller - each `nit` with smaller version named (`../_shared/conventions.md` › Code you write is the bar).

All three findings only when real. Don't invent abstraction objection to have something to say; don't call hot path slow without naming what runs and how often.

**Temper with intent note** (P1.5, point 2) before emitting any finding.

**Finding shape** (exact - used by P4/P5):
```
{ pr, file, line, side, anchorText, severity: blocking|nit, title, explanation, suggestedReplacement? }
```
`side` = `RIGHT` for added/context line, `LEFT` for removed line. `anchorText` = **verbatim source line** finding sits on - this, not number, is real anchor. **Do not trust subagent's `line`.** Subagent counting forward from `@@ … +start,count @@` returns *diff offset*, not new-file line number; posting that anchors comment to wrong code. Before P4 gate orchestrator **resolves every `line` by matching `anchorText` against fetched diff hunks** (new-file number for `RIGHT` line, old-file number for `LEFT`) - so preview shows line actually posted, not guess. Finding whose `anchorText` matches no diffed line has no anchor → goes in summary (P5). See `../_shared/forge-commands.md` §2.

Plus **2-4 sentence human summary per PR** above findings list. When ticket enumerates acceptance criteria, say where they stand in **one plain sentence** - "All four acceptance criteria met." - and name only **unmet or not verifiable** ones, each one short line with finding it points to. Never table, never row per met criterion: author wants what stands between PR and done, not checklist of things fine.

**Token discipline (stories model):**
- **Spend budget on evidence, not standards.** Twenty neighbour files opened to settle real question is the review; twenty opened to learn code style is not.
- **Name overlap before dispatching.** Two PRs in set touching same files (stacked pair, or siblings in one repo) each re-derive same facts unless prompt says which files shared and what other reviewer covers. One line per prompt; orchestrator holds ledger, subagents cite it.
- Big **set** → one PR at a time in this session; parallel subagents only when person asked for them here, each scoped to diff + shared note. **Mind the machine**: read load first (`uptime`, `nproc`/`sysctl -n hw.ncpu`); read-only reviewers can run together, but full test gates run one at a time per repo (two at most when load below core count) - five suites at once push load past 50, all crawl. Wait on subagents by notifications, never placeholder `sleep` task.
- Big **single PR** (large diff / many files) → review it in slices by file/dir group, one after another in this session (subagents only if person asked), then merge + dedupe, plus **one cross-slice reconcile pass** so caller-in-slice-A / definition-in-slice-B issue isn't missed.
- Same-repo sibling PRs touching same code → add short **interaction note** (conflict or overlap?).

## Phase 3.5 - Cross-service contract pass

Triggers on **service boundary crossed** - not repo boundary. Map each changed file to service via `corgi-compose.yml` paths. Boundary crossed by two PRs in different repos **or** single monorepo PR editing two services' dirs (e.g. `api/` + `web/`). Set touching only one service skips P3.5.

**No compose on disk (remote-only / cross-forge set)** - don't silently skip. Fall back: infer service boundary from **changed-file path prefixes** (distinct top-level dirs / repos = distinct services), and where compose can't give dependency direction, infer producer/consumer from diffs (below).

One reviewer sees **all boundary-crossing diffs together** plus dependency direction from `corgi-compose.yml` (`depends_on_services`, `exports`, `${producer.VAR}`) **when present**.

**Checks across boundary:**
- Request/response shape, field names + types, nullability.
- New/removed endpoints, enum values, error codes.
- GraphQL schema / OpenAPI / protobuf / shared types.

**Flags:**
- Producer changed field consumer still reads old way; or consumer expects field producer didn't add.
- Type, enum, or nullability mismatch across boundary.
- Producer change with no matching consumer update (or orphan consumer change with no producer change).
- **Deployed producer, not merged producer** (sweep item 5) - consumer's field must exist on producer *running* when consumer ships; name mechanism shipping each side (CD on merge, OTA on tag, manual dispatch) and window where consumer live against old producer.
- **Merge order** (producer PR first) - state explicitly in output. If compose doesn't encode dependency (HTTP response-field contract usually isn't `depends_on` edge), infer from diffs: PR that **adds/changes field** is producer → merge first.
- **Shared-registry collision** - two+ PRs in set each *append in parallel* to same shared list (enum / string-literal union, generated client-types file, locale bundle, snapshot fixture), not producer/consumer edge. Additive in meaning, but **conflict textually once first lands**. Call out with land order so only last sibling rebases - keeping **all** entries and **regenerating** generated artifacts (snapshots, codegen output), not hand-merging.

Contract finding spans two sides, carries **per-side anchor list** - `anchors: [{ pr, file, line, side }]`, one entry per affected PR (producer's file/line and consumer's differ; GitHub vs GitLab anchor differently). P5 posts each side against **its own** anchor and cross-links; side with no valid anchor folds into that PR's summary. (Plain single-side findings keep normal `{pr,file,line,side}` shape.)

## Phase 3.6 - Verify + prune (before the gate)

Two passes over findings before anything reaches preview.

**Verify the blockers.** Wrong `blocking` finding posted publicly worse than missed nit. Re-check every `blocking` finding against **actual source** - re-read cited lines (and symbol it calls/asserts) from diff or file, not subagent's summary. Drop or downgrade any that don't hold. When blocker hinges on **framework/library runtime behavior** not logic visible in diff (ORM's undefined-vs-null update semantics, validation/transform decorator firing only on present keys, serializer's default coercion), re-reading source can't settle it - run quick **empirical probe** (throwaway test in repo's own runner, or REPL one-liner); observed result, not assumed behavior, decides. Finding that **contradicts CI** is loudest tell: claims spec/build fails but pipeline green → one wrong, verify before posting. Conversely "passed with warnings" pipeline (failed `allow_failure` job) often hides real red job finding points at - pull job's log (`../_shared/forge-commands.md` §1) to confirm. State how each contradiction resolved in report. Claim about cost or amplification carries measured number (input size → time or calls), never adjective; test finding (sweep item 4) quotes coverage report or test run from evidence worktree.

**Check what people see, blocking or not.** Finding that says what person sees or what data holds → read code that writes that data, or real row. Render branch no writer reaches is no finding. Cannot prove it but worth raising → post as question ("can this be empty after signup?"), never statement. Not worth asking → drop.

**Prune against existing discussion.** Drop any finding human already raised, author already answered, or on **resolved** thread (existing-thread list from P1). Re-reviewing settled point is noise - fastest way to get whole review muted. If existing answer looks wrong, that's *reply on existing thread* (out of Mode A's scope - note in report), not fresh duplicate comment.

Nits skip blocker-verify pass (lower stakes) but still get pruned against existing discussion.

## Phase 4 - Preview, then act

**Authorship decides what happens; you never ask "post or not".** Resolve PR/MR author against current forge user (`gh api user -q .login` / `glab api user -q .username`; author from P1's `gh pr view --json author` / `glab mr view`) and act:

| whose PR | what you do |
|---|---|
| someone else's | print preview, then **post**. No question, no "shall I?", no waiting. |
| your own | apply valid findings on its branch and push (Fix path below). No comment on your own work. |
| a mix in one batch | post on theirs, fix-and-push yours. Still no per-PR prompt. |
| current user cannot be resolved, or intent unclear | one prompt for whole set: post / fix / edit / cancel |

Preview is what you print on way to acting, not yes/no question. Only overrides are user's own words in this conversation: "don't post", "review only", "just comment on mine". User who pasted someone else's PR and asked for review already asked for it posted.

Keep findings in conversation, not scratch file: `/tmp` payload written one turn gone by next when OS or cleanup empties it, post must be rebuilt from memory.

Print to terminal, per PR in set:

```
[<repo>#<n>] <PR title>
surface: <changed symbols / routes / keys by name, "breaking" where it is - or "nothing public changed">
<2-4 sentence summary>

  <file>:<line> · blocking · <problem> · <fix>
  <file>:<line> · nit      · <problem> · <fix>
  …
```

If P3.5 ran, append **Contract** section after all per-PR blocks listing cross-service findings and stated merge order (producer first). Many findings → paginate preview (no unbounded wall before gate).

**Plain lines, never tables.** One finding per line in shape above - no box-drawing/ASCII tables. Wide `┌─┬─┐` grids wrap and smear in narrow terminal (columns collide, text corrupts) right when user decides what posts. Keep each line short; truncate long `<problem>`/`<fix>` rather than wrap cell.

**Default lean and human, not exhaustive.** Inline posts = blockers + few nits genuinely helping. Fold low-value nits and style quibbles into summary as one brief "minor:" line, not wall of inline comments - review with 3 sharp comments gets read; 15 gets muted. Each comment terse, kind, human one-liner (problem + fix), not robot paragraph; match repo's comment density. *Edit* option still lets user trim more, but default already this lean - they shouldn't have to ask.

Severity (`blocking|nit`) is triage metadata for preview and report - never print label in posted comment body. State fact and fix options ("this is what's failing the pipeline: …"), let team assign severity; public "blocking" tag from reviewer-bot reads as verdict, not review. Reader still tells must-fix from optional, in words: "this breaks checkout for …" vs "small one, only if it's quick". Repo's reviewers already label comments (`nit:`, `issue:`) → use their labels.

*Edit* is something user can ask for after seeing preview, never offered as question first: present each finding, they keep, drop or rewrite, re-preview, then post. When PR already has corgi summary from prior run, **update existing summary**, don't post new. Zero findings → post clean summary (or, on your own PR, say so and stop).

*Fix path* (own PR - you can push): take Mode B's apply path on **in-hand** findings (checkout PR's own branch, minimum-diff fix each valid one, re-gate, push). Don't post-then-address; don't re-read threads - findings in hand. Pushed-back finding → skip, note why.

`--yes` skips printing preview too. Nothing else changes: action never waited on answer.

## Phase 5 - Post

Exact commands in `../_shared/forge-commands.md` §2-4; use forge from P0.

**GitHub** - one review call (`event=APPROVE` when Approve rule passes, else `event=COMMENT`): `body` = PR's human summary (tagged `<!-- corgi-review -->`), `comments[]` = all inline findings, each suggestion a ` ```suggestion ` block for one-click apply. Summary + all inline in **one** call.

**GitLab** - summary piped to `glab mr note create … --unique` (body on stdin, **not** `-m -`; tagged `<!-- corgi-review -->`); each inline finding via native `glab mr note create --file <path> --line <n>` (or `--old-line` for removed line), suggested change as ` ```suggestion:-0+0 ` block (Apply button). (Those flags experimental and **absent from many `glab` builds** - probe once (§3a); if missing or erroring, post via raw `discussions` + `position` API in §3b.) On §3b path position **must** be one `--input` JSON object - never `-F 'position[…]'` bracket fields, which post unanchored with misleading 201 - then **verify each inline note anchored** (`position != null`), delete+repost any that didn't (§3b/§4). Approval is own call after notes: `glab mr approve <n> -R <owner>/<repo>`. Verify it took: `glab api projects/<id>/merge_requests/<n>/approvals` lists you in `approved_by`.

**Human voice.** Posted summary and every inline comment read as human reviewer: plain, kind, first-person where natural, matching repo's comment density. Attribution rule (and one allowed `<!-- corgi-review -->` marker): `../_shared/conventions.md`.

**Applicable suggestions are the useful part - supply on both forges.** Finding with concrete fix (changed line or small range) posts as suggestion block - GitHub ` ```suggestion `, GitLab ` ```suggestion:-0+0 ` - so author gets one-click **Apply**, not prose to retype. GitLab suggestion blocks render through **both** native `--file/--line` path (§3a) *and* raw discussions API (§3b): if §3a's flags missing and you fall back to §3b, **keep fenced block** - don't downgrade to plain comment. Small fixes are exactly ones Apply saves time on. Reserve plain prose only for findings with no single right fix (scenario 5).

**Finding that cannot be inlined** (line not in diff) → fold into PR's summary; note in report. Never silently drop.

**Cross-service contract finding** → post to **both** sides, each against **its own per-side anchor** (P3.5 `anchors[]`), each cross-linked (`see <other-PR-link>`), so each reviewer sees full picture. Side lacking valid anchor → folds into its PR's summary; other still posts.

**Idempotency (summary + inline) - dedup on deterministic marker, never LLM-generated title:**
- Summary tagged `<!-- corgi-review -->`; every inline comment starts with `<!-- corgi-review:<file>:<line> -->`.
- **Order matters: re-anchor first (scenario 3), THEN dedup** - compute each marker from *re-anchored* (current-head) line, then list existing comments (§4), **skip any whose body already carries marker**. This order means finding shifted lines between runs still matches prior comment, no duplicate. GitLab `--unique` is extra backstop.
- Re-run summary → **update vs new** (decided at P4 gate): GitHub `PUT …/reviews/<id>` (review body can't be PATCHed via `comments` route); GitLab edit/replace tagged note.
- **One rule for finding whose line no longer matches head: fold into summary** (so user still sees it). Reserve "skip" only for threads forge already marked outdated/resolved - never silently drop live finding.
- **User rejects findings after posting** → delete those notes by id, edit tagged summary to match trimmed set (keep markers). Never leave summary advertising findings no longer below it.

**Posting scenarios:**

1. **Clean PR (no findings)** - post one short "Reviewed - no blocking issues" summary (+ line of what checked / any praise). No inline. Don't go silent; don't spam. When Approve rule passes (usual case here), approval **is** clean signal: empty or one-line body, no "what I verified" essay - wall of green text reads as noise, and it's correction you'll be asked to unwind via review-edit API.
2. **Nits only, no blockers** - inline nits; summary headline "No blockers, N nits" so not alarming. Approval goes **with** them - nit never holds it back.
3. **Head moved during gate** - re-fetch metadata (head SHA) right before posting; review call takes **full 40-character** `headRefOid` as `commit_id` - short SHA refused as invalid git object id. If changed: warn, re-fetch new diff, **relocate each finding by anchored source-line text + surrounding hunk context** → take new line/side. Finding whose exact line content absent from every new-head hunk = vanished → fold into summary. **Material change** = any kept finding's anchor line changed or vanished vs this run's P1 head; on material change, re-print affected lines and re-confirm (or offer re-review) before posting. **Never post inline against stale SHA.**
4. **Partial post failure** (inline rejected - line outside diff `422`, rate limit, transient `5xx`) - retry transient errors with backoff; fold still-failing findings into summary; report posted-vs-failed counts. No silent half-post. **GitLab silent-unanchored:** malformed §3b position posts with misleading `201` (not `422`) as general comment - exit code alone isn't proof. After posting, re-fetch, confirm every inline note's `position != null`; delete + repost any unanchored (§4). Don't count unanchored note as posted.
5. **Suggestion can't apply** (pure deletion, non-contiguous range, lines don't line up) - fall back to normal inline comment with proposed code in **plain fenced block** (not ` ```suggestion `). Avoids broken Apply button.
6. **No comment permission** - **pre-flight probe before gate**: check write access (`gh api repos/<o>/<r> -q .permissions` / GitLab member check). No write → tell user up front, switch gate to **print-locally-only** (don't offer "post all" for action that can't succeed). Also catch `403` reactively at post time as backstop. Either way print full review; don't lose work.
7. **Body/size limits** (GitHub body ~65k chars; many findings) - cap inline count per PR, truncate/split summary, push overflow into follow-up comment, say so in report. No silent dropping.
8. **Cross-service dual-post ordering** - resolve `see <other-PR-link>` cross-link only after both PR IDs exist; post both. If one side fails, still post other with one-sided link, flag gap in report.
9. **Throughput** - GitHub posts summary + all inline in one review call. GitLab is one call per discussion → throttle with backoff across many findings / many PRs to avoid rate limits.

## Phase 6 - Grouped report

**Single PR** → one line `[<repo>] <summary headline>` + link, then `risk` skill's summary line (`risk N/10 <tier> · auto-approve: yes|no - <reason>`) on own line, then counts.

**Multi-PR** → group by **related change**, not flat list:
- PRs of **same change/story** (same issue key or branch across repos) → one header `[<issue-key>] <change headline>`, then one `<repo>: <bare link> - <counts
  or top finding>` line per repo, then one risk summary line for set (maximum across PRs, contract counted once).
- **Unrelated targets in one batch never share header** - each gets own block, blank line between.

**Contract** section - whenever P3.5 ran (multi-PR set **or** single monorepo PR crossing service boundary) - lists cross-service findings and merge order (producer first).

**Totals line:**
```
N findings: B blocking, K nits;  P posted inline, S folded into summary.
```
Zero findings → `0 findings - clean.` - keep form exact, always print totals line's `B blocking` count verbatim: caller looping review → fix → re-review (`stories` P5.5) stops on `0 findings - clean.` **or** totals line with `0 blocking` (nits alone don't keep loop alive). Wholly permission-blocked (scenario 6) → `0 posted - printed locally (no write access).`.

**Skipped** line - list every ref NOT reviewed and why (merged/closed and declined, blocked) so skipped target isn't lost between P1 and report.

List anything that couldn't be inlined explicitly (file, line, reason) - no silent drops. Then remove evidence worktree(s): `git -C <dir> worktree remove "$W"`.

**No ceremony footer.** Don't append "review only - does not merge" to every report; reads like bot covering itself, user knows what review is. *Behaviour* stays hard-enforced (Guardrails) - just don't narrate each time. Say in plain words **only** if actually in question (e.g. someone asks "so did you block it?").

Example:

```
[ABC-200] Add address field to user
api: https://github.com/<org>/api/pull/42 - no blockers, 2 nits, approved
web: https://github.com/<org>/web/pull/37 - 1 blocking: missing null-check on user.address
risk 7/10 high · auto-approve: no - cross-service contract

[api] Fix pagination cursor on empty page
https://github.com/<org>/api/pull/45 - no blockers, approved
risk 2/10 trivial · auto-approve: yes

Contract
  api#42 + web#37: api adds address?: string | null; web reads .address without null guard (blocking, posted to both)
  Merge order: api first, then web.

3 findings: 1 blocking, 2 nits;  3 posted inline, 0 folded into summary.
```

---

## Mode B - Address review feedback on your PR/MR

Apply reviewer feedback on **your own** PR, reply per thread, push. **Writes branch** - see Mode B guardrails.

**Target - one PR or whole set:**
- **link** / **bare number** → that one PR.
- **story-id** (`ABC-123`) → correlate like `tracker` (issue git links / dev-panel, else `gh pr list --search <KEY>` / `glab mr list --search <KEY>` across service repos). Multi-repo story = **several PRs, one per service, same branch** → address **whole set**. Genuinely ambiguous (two competing PRs in one repo) → ask; none → stop.

Run steps 1-5 **per PR in set** - cluster threads by repo, one checkout per repo (`stories` token model). Thread asking for **contract change** spanning services (producer field + consumer read) → fix **producer first, consumer after** (`stories` P4 order), cross-link two replies. Then one combined report (6).

1. **Read threads** (§5, `corgi agent watch pr threads <url>...` for whole set) - keep **unresolved, human** ones; skip your own `<!-- corgi-review -->` bots + resolved. Review reviewer started but not submitted shows as pending with no threads: say so in one line, stop, don't hunt comments not there yet. Group by file; read each thread's full back-and-forth (later reply can change ask). **Someone else on it?** `git fetch` PR head first: commits newer than comments, not made by this run, mean another session answering same threads - read what it pushed, do only what it left, never redo (second set of same fixes ends in rejected push).
2. **Judge - apply or push back.** `superpowers:receiving-code-review` if installed, else inline. **Never blind-apply.** Comment naming bug is hypothesis: prove it first (writer of the data, not only reader; reproduce or probe). Cannot happen → reply with the evidence, no fix. Ask that changes what product does, nobody decided it → owner's call, even for one line: ask in thread, name who; never decide it and write it down. Fix in same shape as surrounding code: no comment explaining change, no new abstraction to hold it, nothing adding per-request work. Valid + in scope → fix. Wrong / out-of-scope / regresses → **reply why, don't apply** (push-back is real answer). Needs owner call → ask.
3. **Checkout PR's OWN branch → fix → gate.** Clean tree → `gh pr checkout <n>` / `glab mr checkout <n>` (its head - **not** new branch off base); note branch checked out before, return checkout to it when push done. Dirty tree → `git worktree add` off fetched head so user's work untouched (`stories` P3 worktree rules). **Conflicts first**: `gh pr view <n> --json mergeable,mergeStateStatus` (`glab mr view <n>` → `has_conflicts`) - `CONFLICTING` / `DIRTY` means branch cannot land whatever threads say. `git merge origin/<base>` on PR's own branch (merge, not rebase: reviewer's comment anchors survive), resolve keeping both sides' intent, run gate, commit `Merge <base> into <branch>`; only then threads. Conflict that is only version field, lockfile or generated file resolved mechanically (take higher version, regenerate file with repo's tool), typecheck runs before push - conflict resolved by hand and pushed untested is broken build that follows. Conflict you can't resolve with confidence - two sides changed same logic differently - is **needs you** in report, threads still addressed on top of unmerged branch. Gate: `corgi test --service` / `corgi exec` + scoped self-review (`stories` P3.5). Fix that changes how real input is read (parser, selection pattern, prompt) → run it on real samples when repo or user has them, compare before and after. No run → reply says "not run on real samples", never "Done". **Minimum diff - only what threads ask.**
4. **Reply + resolve per thread** (§5) - what changed (commit/line), or why pushed back. **Reply INSIDE reviewer's thread** - GitHub `in_reply_to`, GitLab `POST …/discussions/<id>/notes`; never `gh pr comment` / `glab mr note create`, which post standalone PR-level note detached from thread (reads as ignoring reviewer; resolving leaves question visually unanswered). **Resolve only what you addressed**; pushed-back thread stays **open**. **One lesson per kind, not per thread.** For every applied finding, name kind in `../_shared/second-reviewer.md` it belongs to and why self-review missed it. House rule (naming, UI convention, input shape) or repeating kind becomes one line for every later session: `corgi agent lesson add "<repo>: <the rule, in the words you would want to read before writing the code>"` - rule, never finding (`web: every clickable element gets
   cursor-pointer`, not `I14 missing cursor-pointer`). One already there in other words → skip. Unattended runs do this too. **Durable convention → memory (confirm first).** If resolved thread settles lasting convention/decision for stack and `.corgi/memory/` exists, draft `decision` fact, list in report as proposed fact - written only on user's OK (`corgi memory add --type decision …`, then `corgi memory index`; see `memory` skill). Never holds push. Absent → skip. **No secrets.**
5. **Preview → push.** Print fixes + replies for whole set once (P4 - printed on way, not question; `--yes` skips printing) → commit (repo style, issue key, no AI trailer) → `git push` each branch → post thread replies. **Draft stays draft; no force-push, no merge, no approve.** Fork PR / no push access → post replies only, say so.
6. **Report** - grouped by PR: per thread **applied** (commit/line) / **pushed back** (reason) / **needs you** (question); + each PR's push result + link. Multi-repo → state producer-first push order.

## Guardrails (non-negotiable)

**Both modes:** **no secret values** echoed into comments/replies - flag location + that secret present, never paste. **Human voice** - terse, kind, specific (problem + fix); each comment and suggestion one or two lines, not paragraph; no AI-attribution trailer, no walls, match repo's density. Post only what genuinely helps - few sharp comments beat many; drop low-value nits, don't pad count. Posted **summary body** is plain prose - few short sentences, at most couple bullets, way person types into PR box. Not structured document: no `##` section headers, no long numbered-question lists, no pasted spec / code-map dumps. Report shape belongs in terminal output (P6), never comment. No tables in comment, acceptance criteria included (P3): met criteria one sentence, unmet one line. **Never suggest adding comment to code.** Reviewer's job is removing ones that don't earn place, not planting more - if line needs explaining, fix in suggestion is clearer name or smaller function.

**Mode A (give review):**
- **Approve rule - review user asked for ends in approval when good.** Someone else's PR, user at desk asked for review → approve when **all** hold: no `blocking` finding filed, not draft, CI not red, no merge conflict. Clean PR → plain approval, empty or one-line body; nits → post and approve alongside. One fails → findings only, report says which held back. Risk card's `auto-approve` line doesn't gate this approval - says whether human may skip reading, and user did read through you; real merge-order or contract problem is `blocking` finding on its own. Never request changes (blocker is comment, not red stamp), never merge, never push, never modify branch.
- **When not to approve.** User said "don't approve", "comments only", "no approval", or "review only". **Unattended** run (prompt carries `corgi watch · …` trail) follows its prompt: "approve it on my behalf only when … the risk card says auto-approve: yes" keeps stricter gate; "do not approve it on my behalf" means no approval, whatever review found.
- **Only `blocking` finding you actually filed holds back approval.** Absent author evidence not finding, not blocker: no screenshot, no before/after, no test asserting copy or layout change, no detail in description - none withhold approval or earn condition on one. Everything found is nit → approve, leave nits as notes. **Never post conditional approval** ("not approving yet, only because …", "add X and it's an easy yes") - request-for-changes wearing friendly face, over something you had no standing to ask for. Test genuinely owed (behaviour diff changes, nothing covers it) is `blocking` finding on own merits or nothing.
- **Comment never talks about approval.** Stamp is whole signal: there or not. Never write "I'm not approving this", "not approving it myself", "leaving it unapproved", "a second reviewer should look", "approving on X's behalf" or any line about who approves or why not - posts under user's name, reads as them declining own review. Why approval withheld (blocking finding, risk gate, "don't approve") goes in terminal report (P6), never PR. Risk headline's tier phrase ("two reviewers, one owning api") not approval statement; keep it, keep nothing else.
- Never pair approval with verification write-up - per-PR "what I checked" report belongs in terminal (P6), not approve body.
- **Read-only on repo.** Never touch user's checkout or PR branch; detached evidence worktree (P1) is only tree you open, for reading and probes, removed in P6. (Exception: PR that is **your own** routes to Fix path - Phase 4 / Mode B - which writes its branch.)
- **Preview before posting** unless `--yes` - show what will post; posting is outward-facing. But **post-vs-fix decision is authorship** (Phase 4), not recurring "should I post?" prompt: your own PR → fix + push (no post), someone else's → post.

**Mode B (address review):**
- **Explicit target only - never infer-and-push.** PR/MR must come from link, bare number, or story-id user gave; must be **your own** branch (you can push). Someone else's PR / producer you don't own → stay in Mode A (comment), never write. One target ambiguous between several PRs → ask, don't pick.
- **Writes branch - bounded.** Edit + push PR's **own** branch only; **draft stays draft, never force-push, never merge, never approve.**
- **Pushing is the point.** Addressing feedback on own PR already asked for push: print preview and push, no "shall I push?". Only user's own words in this conversation hold it ("don't push", "just show me the fixes").
- **Don't blind-apply.** Wrong / out-of-scope suggestion gets reasoned reply, not commit. **Resolve only threads you addressed**; leave pushed-back ones open. **Minimum diff.**