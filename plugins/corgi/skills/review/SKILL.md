---
name: review
description: "Use when the user wants a code review of EXISTING pull/merge requests: \"review this PR/MR\", \"code review <link>\", \"check the api + web MRs for ABC-123\", a bare link with \"thoughts?\". Also to address feedback on your OWN PR: \"fix the comments on this MR\", \"address the feedback for ABC-123\". NOT for creating PRs (stories) or the local diff (/code-review)."
---

# Corgi review

**Done when:** someone else's PR/MR → the review is posted (summary + inline) and its link is in your reply; your own → the valid comments are applied, tests pass, the branch is pushed, and every thread has a reply (resolved where the forge allows).

Review one or more existing remote PR/MR(s) on GitHub or GitLab against each repo's own standards (CLAUDE.md/AGENTS.md, lint and format config) plus the intent from any linked Linear or Jira tracker ticket, then post a human-readable summary comment and inline line-level suggestions back onto each PR/MR. Someone else's PR is posted to without asking; your own is fixed and pushed instead. Services, dirs, and forges resolve from `corgi-compose.yml`.

Read `../_shared/conventions.md` first.

## Two modes — route from the verb

- **A · Give review** (default) — *review / look over / check* a PR, a pasted PR/MR
  link, "thoughts?". Post a summary + inline suggestions; **comments only, never
  touches the branch.** Phases 0–6 below.
- **B · Address review** — *fix / address / answer / respond to* the comments on
  **your** PR ("fix the comments on this MR", "answer the review", "address the
  feedback for story ABC-123"). Read the incoming threads, apply the valid ones, reply
  + resolve, **push the fixes.** See *Mode B* near the end.

Ambiguous ("check my PR for story X") → ask which. A story-id with no link → resolve it
to its PR the way `tracker` does. **Bare "fix this PR" / "fix them"** (no mention of
comments) → default **Mode B** (address feedback); but if the PR has **no open human
threads and CI is red**, the problem is the build, not comments → hand to `debug`
(Step 5), not a comment fix.

---

**Mode A — give review. Phases 0–6:**

## Phase 0 — Resolve target(s)

Input = `$ARGUMENTS` (command) or the pasted message (skill): one or more PR/MR
references, each a **URL** or a **bare number**.

**URL parse:**
- `github.com/<org>/<repo>/pull/<n>` → GitHub, tool `gh`.
- `<host>/<group>/<proj>/-/merge_requests/<n>` → GitLab, tool `glab` (incl.
  self-hosted hosts, not just gitlab.com).

**Bare number** → infer forge + repo from cwd
`git remote get-url origin` (same detection as stories P0: `*github.com*` → `gh`,
`*gitlab*` → `glab`). cwd not a repo / ambiguous → ask. **Bare numbers all resolve
to the cwd repo** — fine for a same-repo batch; for a multi-repo set, require URLs
or confirm the inferred repo per number.

**Verify tooling:** check the matched CLI is installed + authed
(`gh auth status` / `glab auth status`). For a **self-hosted GitLab** ref, check
auth against *that host* (`glab auth status --hostname <host>`) — a healthy
gitlab.com token doesn't mean the internal instance is configured. Missing → stop
with a host-specific install/auth hint; don't guess.

A set may **span forges** (e.g. api MR on GitLab, web PR on GitHub) — resolve each
ref independently. Both forges are first-class.

**Group by repo/service up front.** Build a repo → [PRs] map. Multiple PRs to the
same repo share one standards note (P2) and one area sweep (P3) — the core token
saving.

**Auto-detect siblings (optional).** Given a single PR/MR, read its branch name; if
a `corgi-compose.yml` is in cwd, resolve each other (non-`manualRun`) service's
repo (the P2 mapping) and enumerate same-branch PRs/MRs there:
`gh pr list --head <branch> --repo <o>/<r>` / `glab mr list --source-branch <branch> -R <repo>`
(commands in `../_shared/forge-commands.md` §1). stories opens sibling PRs
with the same branch name, so this finds the rest of a story's set. Found → confirm
the expanded set with the user before reviewing. **Never auto-expand silently.**

## Phase 1 — Fetch (no checkout)

Per PR/MR, fetch **without checking out the branch** (non-destructive — never touch
the user's working tree). Exact commands live in `../_shared/forge-commands.md` §1; pick
the `gh` or `glab` column that matches the ref's forge.

Fetch per PR/MR — **metadata + anchoring SHAs + commits in one call**, diff
separately (don't make a second call just for a SHA):
- GitHub: `gh pr view <n> --json title,body,author,baseRefName,headRefName,state,isDraft,files,url,headRefOid,baseRefOid,commits,statusCheckRollup`.
- GitLab: `glab mr view <n> -R <repo> -F json` — its JSON already includes
  `diff_refs` (`base_sha`/`head_sha`/`start_sha`), `state`, `draft`, `commits`, and
  the head `pipeline` status.
- Unified diff (raw, §0) — `gh pr diff` / `glab mr diff`.
- Inline anchoring later (P5): GitHub uses `path`+`line`+`side`; GitLab uses native
  `--file`/`--line`/`--old-line` (no `diff_refs` needed for the primary post path).
- **CI status** — read it on fetch (GitHub `statusCheckRollup`; GitLab the head
  `pipeline.status`, or `glab ci status -R <repo>`). It's the cross-check for P3.6:
  a **green** pipeline contradicts any "this fails to build/test" finding, so verify
  before posting. And a pipeline that reads green **only because a failed job is
  `allow_failure`** ("passed with warnings") hides a real red job — list the jobs
  (`../_shared/forge-commands.md` §1) and see which finding it confirms.
  **Read the red, not the colour.** A red check that died before the tests ran (a
  version gate, an install, a lockfile check) means the PR has **no test or coverage
  signal at all** — a blocking line on its own, naming the step that killed it, not a
  footnote. And `git merge-tree --write-tree origin/<base> <head>` from the evidence
  worktree: a conflict — usually a version field both sides bumped — blocks the merge
  whatever the checks say.

Fetch the **reviewable diff raw** — an output filter on `gh`/`glab` (rtk or similar)
truncates it and degrades the review; `../_shared/forge-commands.md` §0 has the bypass.

**State.** Read the PR/MR state on fetch.
- **Merged or closed → warn and ask** before reviewing either (usually pasted by
  mistake); skip by default if declined. Unattended (`../_shared/conventions.md`):
  skip with one line, no question.
- Draft → review normally (reviewing drafts is the common case).

**Stacked PRs.** `gh pr diff` / `glab mr diff` diff each PR against its own base.
If PR B's base is PR A's branch, B's diff is already just its own delta — review
as-is. If both target trunk and B's `commits` (from the P1 fetch) contain A's,
isolate B's own changes with the **compare API — no checkout**
(`gh api repos/<o>/<r>/compare/<A-head>...<B-head>`; never `git diff A..B` on a
tree you didn't fetch). Can't isolate cleanly → review the full diff and note the
double-review. State the stacking in the report.

**Existing discussion.** List the PR/MR's current review threads/comments on fetch
(`../_shared/forge-commands.md` §5 read commands work for Mode A too — read
only, no reply). You need them twice: to **dedup** (P5 marker skip) and, more
importantly, to **stay relevant** — a point a human already raised, the author
already answered, or anything on a **resolved** thread is not a fresh finding. Carry
this thread list into P3.6's prune pass. Don't re-litigate settled threads.

**Evidence worktree (read-only).** The diff is where findings anchor; the evidence
lives in the tree around it. Put the head SHA in a throwaway detached worktree — never
the user's checkout, never a branch. A detached worktree has **no installed
dependencies**, so it cannot run a test or a probe as-is. Decide once per repo whether
the hunt needs to run anything; if it does, install into **one** copy per repo up front
rather than letting each subagent rebuild its own — otherwise half the set verifies by
running and half falls back to trusting CI. **Never borrow another checkout's installed
deps when the PR touches the manifest or lockfile** — you would test a dependency tree
the PR just changed, and a version bump is often the thing under review. Can't install →
say so in the report and let CI carry the test signal; a probe on the wrong deps is worse
than no probe:
```bash
git -C <dir> fetch origin pull/<n>/head            # GitLab: merge-requests/<iid>/head
git -C <dir> worktree add --detach /tmp/corgi-review/<repo>-<n> FETCH_HEAD
```
Grep callers, open a dependency's source, run the coverage gate or a probe there;
`git worktree remove` it in P6. Remote-only repo → a shallow clone of the head into the
same path, same rule. Without it the review stays inside the hunk and misses what the
human reviewer next to you finds (P3, evidence sweep).

**Noise filter.** Drop generated/vendored/binary paths from the review surface:
lockfiles (`*.lock`, `package-lock.json`, `go.sum`, …), `vendor/`, `node_modules/`,
codegen output, images/binaries, and anything matching the repo's
`linguist-generated` / `.gitattributes` markers. Also catch **unmarked** codegen:
common globs (`*.pb.go`, `*_gen.go`, `*.generated.*`, OpenAPI/GraphQL client dirs)
+ a header-sentinel scan (`@generated` / `DO NOT EDIT` in the first lines). Never
comment on these; note the count skipped. **But** if a lockfile changed, add one
report line — "N dependency changes in `<lockfile>` — not line-reviewed" — so a
risky transitive bump isn't invisible.

## Phase 1.5 — Tracker enrichment (intent)

Scan the **input**, **each PR/MR body**, AND the **branch name**
(`headRefName`/`source_branch` — e.g. `feature/ABC-123/...` carries `ABC-123`)
for ticket references: `linear.app/…`, `atlassian.net/…`, or a bare `ABC-123` key.
**Dedupe keys across
the whole set first** — a shared ticket (the common api+web case) is fetched
**once** and its intent note reused by every PR that references it, same as P2's
per-repo note. Never re-fetch the same key per PR.

- **Linear** → `mcp__linear-server__get_issue` (+ comments); view screenshots by
  `curl`-ing the signed `uploads.linear.app` URLs (they expire ~5 min — re-fetch
  the issue for fresh URLs) then read.
- **Jira** → `mcp__atlassian__getJiraIssue` (+ comments). It returns attachment
  metadata only and no MCP tool returns the bytes (`../_shared/tracker-mcp.md`):
  with `$JIRA_EMAIL`/`$JIRA_API_TOKEN` in the env, `curl -u
  "$JIRA_EMAIL:$JIRA_API_TOKEN" -L -o <file> "<content-url>"` then read; otherwise
  ask the user to download and share the file. `getAccessibleAtlassianResources`
  for the site if needed.

**Extract the whole intent, not just acceptance criteria** — tickets carry the why:
- Description + acceptance criteria.
- Design rationale — why the approach was chosen, decisions made, trade-offs.
- Constraints — deadlines, compat requirements, "must not touch X", perf/security
  bars.
- Discussion/comments — later clarifications that override the original ask.
- Linked specs/docs + sub-tasks — follow one hop for design context.
- **Design screenshots / Figma links on a UI ticket = the visual acceptance
  bar.** View them and check the **diff** against them — layout values, spacing,
  colour, icons and copy in the code are what you compare. Screenshots the PR
  **does** carry are evidence: ones that visibly diverge from the design are a
  **finding**. A PR with **no** screenshots is **not** a finding, **not** a reason
  to withhold or condition an approval, and **not** a line in the summary —
  screenshots are not owed in this flow; review the code against the design, and
  ask for a screen when the diff genuinely can't settle a visual question (name
  which one). **No design on the ticket, or it explicitly waives one → not a
  finding:** review the UI on standards alone; don't demand a design that was never promised. On **your
  own** PR (the Phase 4 fix path) screenshots are yours to add when they help or
  the user asked — the **`before-after`** skill (base built, same screen captured
  twice) is the tool for a restyle, and it puts them where the user said: the PR
  when they named it, else the ticket. A body whose image links
  are `raw.githubusercontent.com` on a private repo, or local paths, renders empty
  boxes — a `nit` with the fix (`../_shared/forge-commands.md` §6a: assets branch +
  blob `?raw=true`), and not visual proof until they render.

Distill into a compact **intent note** the review uses two ways in P3:
1. **Check** — does the diff do what the ticket asked?
2. **Temper** — a choice the ticket explicitly justifies (deliberate hack, scoped
   approach, known debt with a follow-up) is not a bug; drop the finding or
   downgrade to a soft note citing the ticket's reasoning.

No ticket linked → skip; review on repo standards alone.

Ticket resolves but is **clearly unrelated** to the diff (stale/recycled key in
the branch or title) → treat as no-ticket: review on standards alone, and add a
one-line summary note about the mislink — tracker automation will bind the PR
to the wrong story until the key is fixed.

## Phase 2 — Standards note (once per repo)

Build **one compact, distilled** standards note **per repo** — same-repo PRs
share it, never rebuild it. The note is the orchestrator's cache (stories model):
a handful of bullets the reviewer reads, **not** raw files dumped into context.

**The orchestrator writes this note, and it is the ONLY standards input a subagent
gets. A subagent never opens `CLAUDE.md`, `AGENTS.md` or a lint config itself** —
that is the whole saving, and N same-repo PRs otherwise re-read the same file N
times. Skipping P2 and putting "read the repo's CLAUDE.md" in each prompt is the
failure this phase exists to prevent.

Read the canonical file (`CLAUDE.md`/`AGENTS.md`) plus the one lint config matching
the diff's languages — nothing else, headings only above ~400 lines. No CLAUDE.md →
the note is "lint config X + conventions observed in the diff", which is fine; don't
manufacture standards nobody wrote down.

**On-disk service (repo is a local corgi service)** — map repo → service via
`corgi-compose.yml` (`path:`/`cloneFrom:`), read from the service dir, reuse its
README one-liner for "what this service does". **Never map or read `manualRun`
services** — reference-only, same as stories.

**Remote-only repo (not on disk)** → best-effort API fetch of just `CLAUDE.md`/
`AGENTS.md` + the one relevant lint config (`gh api repos/<o>/<r>/contents/<f>` /
`glab api`). **No full clone.** A `404` = genuinely absent → skip. A `403`/auth
error ≠ absent → note "standards skipped (no access)" so a private repo doesn't
silently get a weaker review.

**Monorepo with >1 service** → still read each canonical file once, but **scope the
note per service area** (keyed by the compose path prefix, e.g. `api/` vs `web/`)
so a `web` convention isn't applied to `api` code.

Distilled note = naming conventions, test patterns, forbidden patterns, code-style
rules the repo **explicitly** spells out. Target a short note (~a dozen bullets),
reused unchanged across every PR in that repo.

## Phase 3 — Review each PR (subagent, scoped)

Hand a review subagent: that PR's diff + title/body + the repo's standards note
+ (if any) the intent note from P1.5 + the evidence worktree path (P1). Findings
**anchor on the diff**; the **evidence comes from the whole tree** — every symbol,
key and test the diff touches has callers, twins and consumers outside the hunk,
and that is where the bugs a good human reviewer finds live. Untouched code is not
reviewed for its own sake; it is read to judge the touched code.

**Surface first, then the diff.** Before a line of implementation, read the
changed surface — the PR body's `## Changed surface` section when the author
left one, else make it: `corgi surface --base <base>` from the checkout (or
`corgi_diff` with `surface: true` over the stack). It lists the exported
symbols, routes, contracts, migrations and config that changed, removals and
signature changes marked breaking. Every breaking line is a question the
review must answer — who calls it, is the caller in this PR or another, does
the ticket ask for it — before the diff is opened. A change whose surface is
"nothing public changed" is reviewed for behaviour and tests only. Say in the
summary comment what the surface was; a reviewer who reads only that line
should know whether to look closer.

**Hunt for:**
- Correctness bugs.
- Missing or weak tests.
- Security issues.
- **Comments the code does not need** — see the rule below.
- **Overengineering** — abstraction with one caller, a config knob nobody asked
  for, an interface introduced for a single implementation, a layer that only
  forwards. Flag it and name the simpler shape. The other four shapes of too much
  code, each a `nit` with the replacement in `suggestedReplacement` and its line
  count in the title: **hand-rolled what the language ships** (a date/URL/sort
  helper the stdlib has), **hand-rolled what the platform ships** (a picker, a
  modal, validation, a constraint the browser, framework or database provides), **a
  new dependency for what an installed one covers** (check the lockfile), and
  **dead flexibility** (a parameter, branch or option nothing in the diff uses).
  The ladder the `stories` skill builds by is `stories/references/smallest-change.md`;
  a PR body's `Deferred` list says which shortcuts were deliberate — a deferred item
  with no trigger is a finding, a deliberate one with a trigger is not.
- **Complexity regressions** — run the `complexity` skill in `report` mode on the
  diff. A touched function whose cyclomatic complexity rose, or a new function over
  the repo's threshold (its own linter config, else 10), is a finding: `nit` by
  default, `blocking` over 15 or in a hot path (the cost rule below). The finding
  carries the number before and after and the tactic that brings it down (guard
  clauses, an extracted function with a what-not-how name, a lookup table, a named
  predicate). A suppression added to pass a linter's complexity rule is a finding on
  its own.
- **Risk score** — run the `risk` skill in `assess` mode on the diff (its Phase 1
  evidence table reuses this PR's fetch; no second checkout). The first line of its
  card — `Risk N/10 — <tier> · <what the reviewer does>` — becomes the summary's
  headline (P5), and `auto-approve: yes|no — <reason>` its last line. The card's
  **Check** items are the reading order for the rest of this hunt: a floor it hit
  (auth, payment, migration, secret, native module, CI) is where the blocking
  findings will be. Offer `stamp` (write the card into the description) at the P4
  gate; never stamp without it.
- **Performance footguns in a hot path** — work repeated per item that could be
  done once, an unbounded read or scan, a per-request shell-out or file walk, a
  network call with no timeout, a goroutine nothing stops.
- **Leaked secrets** — always `severity: blocking`; flag the file + line + that a secret is present; never echo the value into a finding or comment.
- Repo-standard / convention violations (names, patterns, style).
- Scope creep (changes outside the ticket's stated scope).
- **User-facing copy that names internal tech** — a string/label exposing an
  engine, library, vendor, or infra detail to end users (`nit`); suggest selling
  the user benefit, not the mechanism. Skip if the ticket is about that copy.
- Ticket-intent mismatch (diff doesn't do what the ticket asked).

### Evidence sweep — follow every change out of the diff

Run this before any style point, per changed symbol, key, route, flag, schema field
and test. Shapes, greps and failure scenarios: `references/evidence-sweep.md`. An
item is a finding only when the grep or the read shows it; none is a checklist to
recite back.

1. **Twins and callers.** Grep the tree for the changed symbol. A read path that
   gained a field, a context or a filter while its write / enforce / validate twin did
   not (or the reverse) is `blocking`; so is a policy applied where a row is written
   but never where it is used, when a second path into the same gate re-checks it.
2. **Config delivery.** A new env var, secret or flag: name where each deployed
   environment gets it — deploy workflow, task definition, chart, secrets list — with
   an anchored grep (`(^|[^A-Z_])KEY`; a substring match hides absence). Declared only
   in `.env.example` / the CI env = unreachable in production = the fallback default is
   what ships; say what that default does there. `blocking`.
3. **Library premise.** When correctness rests on how a dependency behaves (a version
   comparator, a safe-area provider, an error wrapper, an ORM's null semantics, a
   reconciler), open that dependency's source at the locked version in the worktree,
   or run a probe. The PR body's claim about a library is a hypothesis.
4. **What the tests assert.** Per test in the diff: does it exercise the behaviour the
   criterion names, or a literal — decorator metadata, a mock's call args, the module
   under test mocked away? Would it stay green with the bug put back? Run the repo's
   coverage gate on the head worktree: the component the fix rests on at 0% functions
   is `blocking`.
5. **Deployed contract.** A consumer reading a new field: does the *deployed*
   producer have it (the base branch's schema file, a staging introspection)? The
   real order is deploy mechanics — an auto-published OTA or CD-on-merge versus a
   manual dispatch — not the merge order the body states.
6. **Claims the diff makes false.** A doc, `.env.example` line, code comment or
   CLAUDE.md sentence that now states the wrong count, default or behaviour
   (`corgi docs check` names the file). `nit`, or `blocking` when it describes a
   safety property ("refuses to boot without X" when it only warns).
7. **Edges the happy path hides.** A timestamp that may lie in the future, `??` on a
   value that can be `""`, an optional relation with a live fallback, a transient
   error treated as absence, a hand-copied list nothing keeps in sync, an input that
   fans out exponentially — measure the last one and put the number in the finding.
8. **Failure domain and structure.** A non-critical component now wrapping a critical
   one; a wrapper whose element type flips between branches (the subtree remounts);
   a shared document or query two features now depend on together.

### Comments: the bar is high

A comment beside code is a cost — it goes stale, it repeats what the code
says, and it is the most common thing a generated diff adds too much of.
**The default is no comment.** In practice this is rare enough that a PR
adding several is already a finding.

Flag as `nit` (with the deletion as `suggestedReplacement`) any comment that:
- restates the code (`// loop over users` above a loop over users),
- narrates a step (`// now save it`), or names what the function already names,
- documents a parameter or return the signature already states,
- is a section banner, a TODO with no owner, or commented-out code.

Leave alone — these earn their place:
- **why**, not what: a non-obvious constraint, a rejected alternative, a bug
  this shape exists to prevent,
- a real gotcha the next reader would otherwise reintroduce,
- an exported identifier's doc comment where the language expects one,
- a link to a spec, ticket, or upstream issue that explains the shape.

Judge the same way in Mode B: when addressing feedback, do not add explanatory
comments to justify a change — the change should read for itself.

### Simplicity and cost

Two questions on every non-trivial diff, before any style point:

1. **Is the simplest thing that works being done?** More types, layers or
   options than the change needs is a finding, not neutral. Name the smaller
   version in the explanation.
2. **What does this cost when it runs?** Look at where the code actually sits —
   a per-request handler, a poll loop, a per-item body. Repeated work, an
   unbounded scan, a missing timeout, or a leaked goroutine there is
   `blocking`; the same in a one-shot command is a `nit`.

Both are only findings when they are real. Do not invent an abstraction
objection to have something to say, and do not call a hot path slow without
naming what runs and how often.

**Temper with the intent note** (P1.5, point 2) before emitting any finding.

**Finding shape** (exact — used by P4/P5):
```
{ pr, file, line, side, anchorText, severity: blocking|nit, title, explanation, suggestedReplacement? }
```
`side` = `RIGHT` for an added/context line, `LEFT` for a removed line.
`anchorText` = the **verbatim source line** the finding sits on — this, not the
number, is the real anchor.
**Do not trust a subagent's `line`.** A subagent counting forward from
`@@ … +start,count @@` returns a *diff offset*, not the new-file line number, and
posting that anchors the comment to the wrong code. Before the P4 gate the
orchestrator **resolves every `line` by matching `anchorText` against the fetched
diff hunks** (new-file number for a `RIGHT` line, old-file number for `LEFT`) — so
the preview already shows the line that will actually be posted, not a guess. A
finding whose `anchorText` matches no diffed line has no anchor → goes in the
summary (P5). See `../_shared/forge-commands.md` §2.

Plus a **2–4 sentence human summary per PR** written above the findings list. When
the ticket enumerates acceptance criteria, say where they stand in **one plain
sentence** — "All four acceptance criteria met." — and name only the ones that are
**unmet or not verifiable**, each as one short line with the finding it points to.
Never a table, never a row per criterion that is met: the author wants to know what
stands between the PR and done, not to read a checklist of things that are fine.

**Token discipline (stories model):**
- **Spend the budget on evidence, not on standards.** Twenty neighbour files opened
  to settle a real question is the review; twenty opened to learn the code style is
  not.
- **Name the overlap before dispatching.** Two PRs in a set touching the same files
  (a stacked pair, or siblings in one repo) each re-derive the same facts unless the
  prompt says which files are shared and what the other reviewer covers. One line per
  prompt; the orchestrator holds the ledger, subagents cite it.
- Big **set** → dispatch per-PR reviews to parallel subagents, each scoped to
  its diff + the shared note.
- Big **single PR** (large diff / many files) → split that one PR's diff by
  file/dir group across parallel subagents (cap group size so each fits context),
  then merge + dedupe, plus **one cross-slice reconcile pass** so a
  caller-in-slice-A / definition-in-slice-B issue isn't missed between slices.
- Same-repo sibling PRs touching the same code → add a short **interaction
  note** (do they conflict or overlap?).

## Phase 3.5 — Cross-service contract pass

Triggers on a **service boundary being crossed** — not a repo boundary. Map
each changed file to its service via `corgi-compose.yml` paths. The boundary is
crossed by two PRs in different repos **or** by a single monorepo PR editing
two services' dirs (e.g. `api/` + `web/`). A set touching only one service
skips P3.5.

**No compose on disk (remote-only / cross-forge set)** — don't silently skip the
pass. Fall back: infer the service boundary from **changed-file path prefixes**
(distinct top-level dirs / repos = distinct services), and where compose can't
give the dependency direction, infer producer/consumer from the diffs themselves
(below).

One reviewer sees **all boundary-crossing diffs together** plus the dependency
direction from `corgi-compose.yml` (`depends_on_services`, `exports`,
`${producer.VAR}`) **when present**.

**Checks across the boundary:**
- Request/response shape, field names + types, nullability.
- New/removed endpoints, enum values, error codes.
- GraphQL schema / OpenAPI / protobuf / shared types.

**Flags:**
- Producer changed a field the consumer still reads the old way; or consumer
  expects a field the producer didn't add.
- Type, enum, or nullability mismatch across the boundary.
- Producer change with no matching consumer update (or an orphan consumer
  change with no producer change).
- **Deployed producer, not merged producer** (sweep item 5) — the consumer's field
  must exist on the producer that is *running* when the consumer ships; name the
  mechanism that ships each side (CD on merge, OTA on tag, manual dispatch) and the
  window where the consumer is live against the old producer.
- **Merge order** (producer PR first) — state it explicitly in the output. If
  compose doesn't encode the dependency (an HTTP response-field contract usually
  isn't a `depends_on` edge), infer it from the diffs: the PR that **adds/changes
  the field** is the producer → merge it first.
- **Shared-registry collision** — two+ PRs in the set each *append in parallel* to
  the same shared list (an enum / string-literal union, a generated client-types
  file, a locale bundle, a snapshot fixture), not a producer/consumer edge. Additive
  in meaning, but they **conflict textually once the first lands**. Call it out with
  a land order so only the last sibling rebases — keeping **all** entries and
  **regenerating** generated artifacts (snapshots, codegen output) rather than
  hand-merging them.

A contract finding spans two sides, so it carries a **per-side anchor list** —
`anchors: [{ pr, file, line, side }]`, one entry per affected PR (the producer's
file/line and the consumer's are different, and GitHub vs GitLab anchor
differently). P5 posts each side against **its own** anchor and cross-links; a
side with no valid anchor folds into that PR's summary. (Plain single-side
findings keep the normal `{pr,file,line,side}` shape.)

## Phase 3.6 — Verify + prune (before the gate)

Two passes over the findings before anything reaches the preview.

**Verify the blockers.** A wrong `blocking` finding posted publicly is worse than a
missed nit. Re-check every `blocking` finding against the **actual source** — re-read
the cited lines (and the symbol it calls/asserts) from the diff or file, not from the
subagent's summary. Drop or downgrade any that don't hold. When a blocker hinges on
**framework/library runtime behavior** rather than logic visible in the diff (an ORM's
undefined-vs-null update semantics, a validation/transform decorator that fires only on
present keys, a serializer's default coercion), re-reading source can't settle it — run a
quick **empirical probe** (a throwaway test in the repo's own runner, or a REPL one-liner)
and let the observed result, not the assumed behavior, decide. A finding that
**contradicts CI** is the loudest tell: claims a spec/build fails but the pipeline is
green → one of them is wrong, verify before posting. Conversely a "passed with
warnings" pipeline (a failed `allow_failure` job) often hides the real red job a
finding points at — pull that job's log (`../_shared/forge-commands.md` §1)
to confirm. State how each contradiction resolved in the report. A claim about
cost or amplification carries the measured number (input size → time or calls),
never an adjective; a test finding (sweep item 4) quotes the coverage report or
the test run from the evidence worktree.

**Prune against existing discussion.** Drop any finding a human already raised, the
author already answered, or that sits on a **resolved** thread (the existing-thread
list from P1). Re-reviewing a settled point is noise — the fastest way to get the
whole review muted. If the existing answer looks wrong, that's a *reply on the
existing thread* (out of Mode A's scope — note it in the report), not a fresh
duplicate comment.

Nits skip the blocker-verify pass (lower stakes) but still get pruned against
existing discussion.

## Phase 4 — Preview, then act

**Authorship decides what happens; you never ask "post or not".** Resolve the
PR/MR author against the current forge user (`gh api user -q .login` /
`glab api user -q .username`; author from P1's `gh pr view --json author` /
`glab mr view`) and act:

| whose PR | what you do |
|---|---|
| someone else's | print the preview, then **post** it. No question, no "shall I?", no waiting. |
| your own | apply the valid findings on its branch and push (Fix path below). No comment on your own work. |
| a mix in one batch | post on theirs, fix-and-push yours. Still no per-PR prompt. |
| the current user cannot be resolved, or the intent is unclear | one prompt for the whole set: post / fix / edit / cancel |

The preview is what you print on the way to acting, not a yes/no question. The
only overrides are the user's own words in this conversation: "don't post",
"review only", "just comment on mine". A user who pasted someone else's PR and
asked for a review has already asked for the review to be posted.

Print to terminal, per PR in the set:

```
[<repo>#<n>] <PR title>
<2–4 sentence summary>

  <file>:<line> · blocking · <problem> · <fix>
  <file>:<line> · nit      · <problem> · <fix>
  …
```

If P3.5 ran, append a **Contract** section after all per-PR blocks listing
cross-service findings and the stated merge order (producer first). Many findings
→ paginate the preview (don't print an unbounded wall before the gate).

**Plain lines, never tables.** One finding per line in the shape above — no
box-drawing/ASCII tables. Wide `┌─┬─┐` grids wrap and smear in a narrow terminal
(columns collide, text corrupts) right when the user has to decide what posts. Keep
each line short; truncate a long `<problem>`/`<fix>` rather than wrap a cell.

**Default lean and human, not exhaustive.** What posts inline = the blockers + the
few nits that genuinely help. Fold low-value nits and style quibbles into the summary
as one brief "minor:" line, not a wall of inline comments — a review with 3 sharp
comments gets read; one with 15 gets muted. Each comment is a terse, kind, human
one-liner (problem + fix), not a robot paragraph; match the repo's comment density.
The *edit* option still lets the user trim more, but the default should already be
this lean — they shouldn't have to ask.

Severity (`blocking|nit`) is triage metadata for the preview and the report —
never print the label in a posted comment body. State the fact and the fix
options ("this is what's failing the pipeline: …") and let the team assign
severity; a public "blocking" tag from a reviewer-bot reads as a verdict, not
a review.

*Edit* is something the user can ask for after seeing the preview, never
something you offer as a question first: present each finding, they keep, drop
or rewrite it, re-preview, then post. When a PR already has a corgi summary from
a prior run, **update the existing summary** rather than post a new one. Zero
findings → post the clean summary (or, on your own PR, say so and stop).

*Fix path* (own PR — you can push): take Mode B's apply path on the **in-hand** findings
(checkout the PR's own branch, minimum-diff fix each valid one, re-gate, push). Don't
post-then-address; don't re-read threads — findings already in hand. Pushed-back finding
→ skip, note why.

`--yes` skips printing the preview too. Nothing else changes: the action was
never waiting on an answer.

## Phase 5 — Post

Exact commands live in `../_shared/forge-commands.md` §2–4; use the forge from P0.

**GitHub** — one review call (`event=COMMENT`): `body` = the PR's human summary
(tagged `<!-- corgi-review -->`), `comments[]` = all inline findings, each
suggestion a ` ```suggestion ` block for one-click apply. Summary + all inline
in **one** call.

**GitLab** — summary piped to `glab mr note create … --unique` (the body on stdin,
**not** `-m -`; tagged `<!-- corgi-review -->`); each inline finding via the native
`glab mr note create --file <path> --line <n>` (or `--old-line` for a removed line),
suggested change as a ` ```suggestion:-0+0 ` block (Apply button). (Those flags are
experimental and **absent from many `glab` builds** — probe once (§3a) and, if
missing or erroring, post via the raw `discussions` + `position` API in §3b.) On the
§3b path the position **must** be one `--input` JSON object — never `-F 'position[…]'`
bracket fields, which post unanchored with a misleading 201 — then **verify each
inline note anchored** (`position != null`) and delete+repost any that didn't (§3b/§4).

**Human voice.** The posted summary and every inline comment read as a human reviewer:
plain, kind, first-person where natural, matching the repo's comment density. Attribution
rule (and the one allowed `<!-- corgi-review -->` marker): `../_shared/conventions.md`.

**Applicable suggestions are the useful part — supply them on both forges.** A
finding with a concrete fix (a changed line or a small range) posts as a suggestion
block — GitHub ` ```suggestion `, GitLab ` ```suggestion:-0+0 ` — so the author gets
a one-click **Apply**, not prose to retype. GitLab suggestion blocks render through
**both** the native `--file/--line` path (§3a) *and* the raw discussions API (§3b):
if §3a's flags are missing and you fall back to §3b, **keep the fenced block** —
don't downgrade to a plain comment. The small fixes are exactly the ones Apply saves
time on. Reserve plain prose only for findings with no single right fix (scenario 5).

**Finding that cannot be inlined** (line not in the diff) → fold into that PR's summary;
note it in the report. Never silently drop.

**Cross-service contract finding** → post to **both** sides, each against **its own
per-side anchor** (P3.5 `anchors[]`), each cross-linked (`see <other-PR-link>`), so
each reviewer sees the full picture. A side lacking a valid anchor → that side folds
into its PR's summary; the other still posts.

**Idempotency (summary + inline) — dedup on a deterministic marker, never on the
LLM-generated title:**
- Summary tagged `<!-- corgi-review -->`; every inline comment starts with
  `<!-- corgi-review:<file>:<line> -->`.
- **Order matters: re-anchor first (scenario 3), THEN dedup** — compute each
  marker from the *re-anchored* (current-head) line, then list existing comments
  (§4) and **skip any whose body already carries that marker**. Doing it in this
  order means a finding that shifted lines between runs still matches its prior
  comment instead of posting a duplicate. GitLab `--unique` is an extra backstop.
- Re-run summary → **update vs new** (decided at the P4 gate): GitHub
  `PUT …/reviews/<id>` (a review body can't be PATCHed via a `comments` route);
  GitLab edit/replace the tagged note.
- **One rule for a finding whose line no longer matches head: fold it into the
  summary** (so the user still sees it). Reserve "skip" only for threads the forge
  has already marked outdated/resolved — never silently drop a live finding.
- **User rejects findings after posting** → delete those notes by id and edit
  the tagged summary so it matches the trimmed set (keep the markers). Never
  leave the summary advertising findings that no longer exist below it.

**Posting scenarios:**

1. **Clean PR (no findings)** — post one short "Reviewed — no blocking issues"
   summary (+ a line of what was checked / any praise). No inline. Don't go
   silent; don't spam. If the user authorized approving (Mode A guardrail
   exception), the approval **is** the clean signal: empty or one-line body,
   no "what I verified" essay — a wall of green text reads as noise, and it's
   the correction you'll be asked to unwind via the review-edit API.
2. **Nits only, no blockers** — inline the nits; summary headline "No blockers,
   N nits" so it doesn't read as alarming. If approving was authorized, the
   approval goes **with** them — a nit never holds it back.
3. **Head moved during the gate** — re-fetch metadata (head SHA) right before
   posting. If it changed: warn, re-fetch the new diff, and **relocate each
   finding by its anchored source-line text + surrounding hunk context** → take
   the new line/side. A finding whose exact line content is absent from every
   new-head hunk = vanished → fold into the summary. **Material change** = any kept
   finding's anchor line changed or vanished vs this run's P1 head; on a material
   change, re-print the affected lines and re-confirm (or offer re-review) before
   posting. **Never post inline against a stale SHA.**
4. **Partial post failure** (inline rejected — line outside diff `422`, rate
   limit, transient `5xx`) — retry transient errors with backoff; fold
   still-failing findings into the summary; report posted-vs-failed counts. No
   silent half-post. **GitLab silent-unanchored:** a malformed §3b position posts
   with a misleading `201` (not a `422`) as a general comment — so exit code alone
   isn't proof. After posting, re-fetch and confirm every inline note's
   `position != null`; delete + repost any that didn't anchor (§4). Don't count an
   unanchored note as posted.
5. **Suggestion can't apply** (pure deletion, non-contiguous range, lines don't
   line up) — fall back to a normal inline comment with the proposed code in a
   **plain fenced block** (not ` ```suggestion `). Avoids a broken Apply button.
6. **No comment permission** — **pre-flight probe before the gate**: check write
   access (`gh api repos/<o>/<r> -q .permissions` / GitLab member check). No write
   → tell the user up front and switch the gate to **print-locally-only** (don't
   offer "post all" for an action that can't succeed). Also catch a `403` reactively
   at post time as a backstop. Either way print the full review; don't lose the work.
7. **Body/size limits** (GitHub body ~65k chars; many findings) — cap inline
   count per PR, truncate/split the summary, push overflow into a follow-up
   comment, and say so in the report. No silent dropping.
8. **Cross-service dual-post ordering** — resolve the `see <other-PR-link>`
   cross-link only after both PR IDs exist; post both. If one side fails, still
   post the other with a one-sided link and flag the gap in the report.
9. **Throughput** — GitHub posts summary + all inline in one review call. GitLab
   is one call per discussion → throttle with backoff across many findings / many
   PRs to avoid rate limits.

## Phase 6 — Grouped report

**Single PR** → one line `[<repo>] <summary headline>` + link, then the `risk`
skill's summary line (`risk N/10 <tier> · auto-approve: yes|no — <reason>`) on its
own line, then counts.

**Multi-PR** → group by **related change**, not one flat list:
- PRs of the **same change/story** (same issue key or branch across repos) → one
  header `[<issue-key>] <change headline>`, then one `<repo>: <bare link> — <counts
  or top finding>` line per repo, then one risk summary line for the set (the
  maximum across its PRs, contract counted once).
- **Unrelated targets in one batch never share a header** — each gets its own
  block, blank line between.

**Contract** section — whenever P3.5 ran (multi-PR set **or** a single monorepo
PR crossing a service boundary) — lists cross-service findings and merge order
(producer first).

**Totals line:**
```
N findings: B blocking, K nits;  P posted inline, S folded into summary.
```
Zero findings → `0 findings — clean.` — keep that form exact, and always print
the totals line's `B blocking` count verbatim: a caller looping review → fix →
re-review (`stories` P5.5) stops on `0 findings — clean.` **or** a totals line
with `0 blocking` (nits alone don't keep its loop alive). Wholly
permission-blocked (scenario 6) → `0 posted — printed locally (no write access).`.

**Skipped** line — list every ref that was NOT reviewed and why (merged/closed and
declined, blocked) so a skipped target isn't lost between P1 and the report.

List anything that couldn't be inlined explicitly (file, line, reason) — no
silent drops. Then remove the evidence worktree(s):
`git -C <dir> worktree remove /tmp/corgi-review/<repo>-<n>`.

**No ceremony footer.** Don't append "review only — does not approve/merge" to every
report; it reads like a bot covering itself, and the user knows what a review is. The
*behaviour* stays hard-enforced (Guardrails) — just don't narrate it each time. Say
it in plain words **only** if it's actually in question (e.g. someone asks "so did
you block it?").

Example:

```
[ABC-200] Add address field to user
api: https://github.com/<org>/api/pull/42 — no blockers, 2 nits
web: https://github.com/<org>/web/pull/37 — 1 blocking: missing null-check on user.address
risk 7/10 high · auto-approve: no — cross-service contract

[api] Fix pagination cursor on empty page
https://github.com/<org>/api/pull/45 — no blockers
risk 2/10 trivial · auto-approve: yes

Contract
  api#42 + web#37: api adds address?: string | null; web reads .address without null guard (blocking, posted to both)
  Merge order: api first, then web.

3 findings: 1 blocking, 2 nits;  3 posted inline, 0 folded into summary.
```

---

## Mode B — Address review feedback on your PR/MR

Apply reviewer feedback on **your own** PR, reply per thread, push. **Writes the
branch** — see Mode B guardrails.

**Target — one PR or a whole set:**
- a **link** / **bare number** → that one PR.
- a **story-id** (`ABC-123`) → correlate like `tracker` (issue git links / dev-panel,
  else `gh pr list --search <KEY>` / `glab mr list --search <KEY>` across the service
  repos). A multi-repo story = **several PRs, one per service, same branch** → address
  the **whole set**. Genuinely ambiguous (two competing PRs in one repo) → ask; none →
  stop.

Run steps 1–5 **per PR in the set** — cluster threads by repo, one checkout per repo
(`stories` token model). A thread asking for a **contract change** that spans services
(producer field + consumer read) → fix **producer first, consumer after** (`stories`
P4 order) and cross-link the two replies. Then one combined report (6).

1. **Read threads** (§5) — keep the **unresolved, human** ones; skip your own
   `<!-- corgi-review -->` bots + resolved. Group by file; read each thread's full
   back-and-forth (a later reply can change the ask).
2. **Judge — apply or push back.** `superpowers:receiving-code-review` if installed,
   else inline. **Never blind-apply.** Fix in the same shape as the surrounding
   code: no comment explaining the change, no new abstraction to hold it, and
   nothing that adds per-request work. Valid + in scope → fix. Wrong /
   out-of-scope / regresses → **reply why, don't apply** (push-back is a real
   answer). Needs an owner
   call → ask.
3. **Checkout the PR's OWN branch → fix → gate.** Clean tree → `gh pr checkout <n>` /
   `glab mr checkout <n>` (its head — **not** a new branch off base). Dirty tree →
   `git worktree add` off the fetched head so the user's work is untouched (`stories`
   P3 worktree rules). **Conflicts first**: `gh pr view <n> --json mergeable,mergeStateStatus`
   (`glab mr view <n>` → `has_conflicts`) — `CONFLICTING` / `DIRTY` means the branch
   cannot land whatever the threads say. `git merge origin/<base>` on the PR's own
   branch (a merge, not a rebase: the reviewer's comment anchors survive), resolve
   keeping both sides' intent, run the gate, commit `Merge <base> into <branch>`;
   only then the threads. A conflict you cannot resolve with confidence — two
   sides changed the same logic differently — is a **needs you** in the report, and
   the threads still get addressed on top of the unmerged branch.
   Gate: `corgi test --service` / `corgi exec` + scoped self-review
   (`stories` P3.5). **Minimum diff — only what the threads ask.**
4. **Reply + resolve per thread** (§5) — what changed (commit/line), or why you pushed
   back. **Reply INSIDE the reviewer's thread** — GitHub `in_reply_to`, GitLab
   `POST …/discussions/<id>/notes`; never `gh pr comment` / `glab mr note create`,
   which post a standalone PR-level note detached from the thread (reads as ignoring
   the reviewer, and resolving leaves their question visually unanswered).
   **Resolve only what you addressed**; a pushed-back thread stays **open**.
   **Durable convention → memory (confirm first).** If a resolved thread settles a
   lasting convention/decision for the stack and `.corgi/memory/` exists, draft a
   `decision` fact and list it in the report as a proposed fact — written only on the
   user's OK (`corgi memory add --type decision …`, then `corgi memory index`; see the
   `memory` skill). It never holds the push. Absent → skip. **No secrets.**
5. **Preview → push.** Print the fixes + replies for the whole set once (P4 — printed
   on the way, not a question; `--yes` skips printing it) → commit (repo style, issue
   key, no AI trailer) → `git push` each branch → post the thread replies. **Draft stays draft; no force-push, no merge, no approve.** Fork PR / no
   push access → post replies only, say so.
6. **Report** — grouped by PR: per thread **applied** (commit/line) / **pushed back**
   (reason) / **needs you** (question); + each PR's push result + link. Multi-repo →
   state the producer-first push order.

## Guardrails (non-negotiable)

**Both modes:** **no secret values** echoed into comments/replies — flag location +
that a secret is present, never paste it. **Human voice** — terse, kind, specific
(problem + fix); each comment and suggestion is one or two lines, not a paragraph; no
AI-attribution trailer, no walls, match the repo's density. Post only what genuinely
helps — a few sharp comments beat many; drop low-value nits rather than pad the count.
A posted **summary body** is plain prose — a few short sentences, at most a couple of
bullets, the way a person types into the PR box. Not a structured document: no `##`
section headers, no long numbered-question lists, no pasted spec / code-map dumps.
That report shape belongs in the terminal output (P6), never in the comment. No
tables in a comment, acceptance criteria included (P3): met criteria are one
sentence, an unmet one is one line.
**Never suggest adding a comment to the code.** The reviewer's job is to remove
the ones that do not earn their place, not to plant more — if a line needs
explaining, the fix in the suggestion is a clearer name or a smaller function.

**Mode A (give review):**
- **Comments only.** Never set a formal approve / request-changes state, never merge,
  never push, never modify the branch. **Sole exception — the user explicitly says to
  approve** ("approve if good", "approve these", or the unattended prompt from a
  workspace with `--approve` on). Then: clean PR → plain approval,
  empty or one-line body; PR with findings → post the findings (summary + inline),
  approving alongside only when none are blocking **and** the risk card's last line is
  `auto-approve: yes`. A `no` there means findings only, never an approval, whatever the
  prompt said.
- **Only a `blocking` finding you actually filed holds back the approval.** Absent
  author evidence is not a finding and not a blocker: no screenshot, no before/after,
  no test asserting a copy or layout change, no detail in the description — none of
  these withhold an approval or earn a condition on one. Everything you found is a
  nit → approve and leave the nits as notes. **Never post a conditional approval**
  ("not approving yet, only because …", "add X and it's an easy yes") — that is a
  request-for-changes wearing a friendly face, over something you had no standing to
  ask for. A test that is genuinely owed (behaviour the diff changes, nothing covers
  it) is a `blocking` finding on its own merits or it is nothing.
- Never pair an approval with a
  verification write-up — the per-PR "what I checked" report belongs in the terminal
  (P6), not the approve body.
- **Read-only on the repo.** Never touch the user's checkout or the PR branch; the
  detached evidence worktree (P1) is the only tree you open, for reading and probes,
  and it is removed in P6. (Exception: a PR that is **your own** routes to the Fix path —
  Phase 4 / Mode B — which does write its branch.)
- **Preview before posting** unless `--yes` — show what will post; posting is
  outward-facing. But the **post-vs-fix decision is authorship** (Phase 4), not a
  recurring "should I post?" prompt: your own PR → fix + push (no post), someone else's →
  post.

**Mode B (address review):**
- **Explicit target only — never infer-and-push.** The PR/MR must come from a link, a
  bare number, or a story-id the user gave; it must be **your own** branch (you can
  push). Someone else's PR / a producer you don't own → stay in Mode A (comment), never
  write. One target ambiguous between several PRs → ask, don't pick.
- **Writes the branch — bounded.** Edit + push the PR's **own** branch only; **draft
  stays draft, never force-push, never merge, never approve.**
- **Pushing is the point.** Addressing feedback on your own PR already asked for the
  push: print the preview and push, no "shall I push?". Only the user's own words in
  this conversation hold it ("don't push", "just show me the fixes").
- **Don't blind-apply.** A wrong / out-of-scope suggestion gets a reasoned reply, not
  a commit. **Resolve only threads you addressed**; leave pushed-back ones open.
  **Minimum diff.**
