---
name: review
description: Use when the user wants a code review of one or more EXISTING pull/merge requests — any phrasing of "review this PR/MR" alongside GitHub PR or GitLab MR links/numbers (e.g. "review these MRs <link> <link>", "look over this PR <link>", "code review <link>", "check the api + web MRs for ABC-123", or a bare PR/MR link with "thoughts?"). Reviews against the repo's own standards (CLAUDE.md/AGENTS.md, lint config), pulls intent from any linked Linear/Jira ticket, runs a cross-service contract check when the set spans services, then posts a human summary comment + inline suggestions behind a preview gate. ALSO use to **address review feedback on your OWN PR/MR** — "fix the comments on this MR", "answer the review and reply", "address the feedback for story ABC-123": it reads the incoming reviewer threads, applies the valid ones (pushing back on the wrong ones), replies to + resolves the threads, and pushes the fixes to the PR branch — resolve the target from a link, a bare number, or a tracker story-id. NOT for creating PRs from issues/feature text (that is the stories skill) or reviewing the local uncommitted diff (that is the built-in /code-review).
---

# Corgi review

Review one or more existing remote PR/MR(s) on GitHub or GitLab against each repo's own standards (CLAUDE.md/AGENTS.md, lint and format config) plus the intent from any linked Linear or Jira tracker ticket, then post a human-readable summary comment and inline line-level suggestions back onto each PR/MR — all behind a preview gate before anything goes public. It is the direct counterpart to the `stories` skill: stories **creates** draft PRs/MRs from issues or feature text; review **consumes** existing ones. It reuses stories' workspace model (services, dirs, and forges resolved from `corgi-compose.yml`) and stories' token-efficiency model (cluster by service+area, investigate once, orchestrator-as-cache, reuse ledger).

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
(commands in `references/github-gitlab-commands.md` §1). stories opens sibling PRs
with the same branch name, so this finds the rest of a story's set. Found → confirm
the expanded set with the user before reviewing. **Never auto-expand silently.**

## Phase 1 — Fetch (no checkout)

Per PR/MR, fetch **without checking out the branch** (non-destructive — never touch
the user's working tree). Exact commands live in `references/github-gitlab-commands.md` §1; pick
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
  (`references/github-gitlab-commands.md` §1) and see which finding it confirms.

**rtk:** metadata, status, and list calls go through rtk automatically (the Claude
Code hook rewrites `git`/`gh`/`glab`). Fetch the **reviewable diff raw** to avoid
truncation degrading review quality:
```
rtk proxy gh pr diff <n> --repo <owner>/<repo> --patch
rtk proxy glab mr diff <n> --repo <host>/<group>/<proj> --color=never
```
See `references/github-gitlab-commands.md` §0 for the rule of thumb: rtk-filtered for
everything except the diff content (and any file body read in full).

**State.** Read the PR/MR state on fetch.
- **Merged or closed → warn and ask** before reviewing either (usually pasted by
  mistake); skip by default if declined.
- Draft → review normally (reviewing drafts is the common case).

**Stacked PRs.** `gh pr diff` / `glab mr diff` diff each PR against its own base.
If PR B's base is PR A's branch, B's diff is already just its own delta — review
as-is. If both target trunk and B's `commits` (from the P1 fetch) contain A's,
isolate B's own changes with the **compare API — no checkout**
(`gh api repos/<o>/<r>/compare/<A-head>...<B-head>`; never `git diff A..B` on a
tree you didn't fetch). Can't isolate cleanly → review the full diff and note the
double-review. State the stacking in the report.

**Existing discussion.** List the PR/MR's current review threads/comments on fetch
(`references/github-gitlab-commands.md` §5 read commands work for Mode A too — read
only, no reply). You need them twice: to **dedup** (P5 marker skip) and, more
importantly, to **stay relevant** — a point a human already raised, the author
already answered, or anything on a **resolved** thread is not a fresh finding. Carry
this thread list into P3.6's prune pass. Don't re-litigate settled threads.

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
(`headRefName`/`source_branch` — e.g. `feature/HUM-1063/...` carries `HUM-1063`)
for ticket references: `linear.app/…`, `atlassian.net/…`, or a bare `ABC-123` key.
**Dedupe keys across
the whole set first** — a shared ticket (the common api+web case) is fetched
**once** and its intent note reused by every PR that references it, same as P2's
per-repo note. Never re-fetch the same key per PR.

- **Linear** → `mcp__linear-server__get_issue` (+ comments); view screenshots by
  `curl`-ing the signed `uploads.linear.app` URLs (they expire ~5 min — re-fetch
  the issue for fresh URLs) then read.
- **Jira** → `mcp__atlassian__getJiraIssue` (+ comments); fetch attachment bytes
  via `mcp__atlassian__fetch` then read (getJiraIssue returns attachment metadata,
  not image bytes). Use `getAccessibleAtlassianResources` for the site if needed.

**Extract the whole intent, not just acceptance criteria** — tickets carry the why:
- Description + acceptance criteria.
- Design rationale — why the approach was chosen, decisions made, trade-offs.
- Constraints — deadlines, compat requirements, "must not touch X", perf/security
  bars.
- Discussion/comments — later clarifications that override the original ask.
- Linked specs/docs + sub-tasks — follow one hop for design context.

Distill into a compact **intent note** the review uses two ways in P3:
1. **Check** — does the diff do what the ticket asked?
2. **Temper** — a choice the ticket explicitly justifies (deliberate hack, scoped
   approach, known debt with a follow-up) is not a bug; drop the finding or
   downgrade to a soft note citing the ticket's reasoning.

No ticket linked → skip; review on repo standards alone.

## Phase 2 — Standards note (once per repo)

Build **one compact, distilled** standards note **per repo** — same-repo PRs
share it, never rebuild it. The note is the orchestrator's cache (stories model):
a handful of bullets the reviewer reads, **not** raw files dumped into context.

**Keep it cheap — this is the token-sensitive phase.** Don't read everything.
1. **Canonical first, stop early.** Read `CLAUDE.md` (or `AGENTS.md`) — usually
   small and authoritative. If it covers conventions well, that + the lint config
   is the whole note. `GEMINI.md`/`.cursorrules`/`CONTRIBUTING.md` overlap heavily
   — read one only if the canonical file is absent. Don't read all five.
2. **One lint/format config, relevance-gated by the diff's languages.** Diff is
   all TS → read the JS/TS config (`biome.json` / `.eslintrc*` / `.prettierrc`),
   skip `.golangci.yml`/`ruff.toml`. Don't read configs for languages not in the
   diff.
3. **Size cap.** Skip any standards file over ~400 lines / 40 KB — sample its
   headings instead of reading whole. The manifest is read only for the test/lint
   *scripts*, not in full.
4. **Neighbor files are lazy, not upfront.** Don't pre-read source files. The diff
   already carries surrounding context; only open **1–2** neighbor files **if** a
   specific convention question comes up mid-review (e.g. "is this the repo's error
   pattern?"), scoped to the diff's area.
5. **Most repos have no CLAUDE.md** → the note is just "lint config X + conventions
   observed in the diff." Near-zero cost. Don't manufacture standards that aren't
   written down.

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
+ (if any) the intent note from P1.5. Scope **strictly to the diff** — don't
review untouched code.

**Hunt for:**
- Correctness bugs.
- Missing or weak tests.
- Security issues.
- **Leaked secrets** — always `severity: blocking`; flag the file + line + that a secret is present; never echo the value into a finding or comment.
- Repo-standard / convention violations (names, patterns, style).
- Scope creep (changes outside the ticket's stated scope).
- Perf footguns.
- **User-facing copy that names internal tech** — a string/label exposing an
  engine, library, vendor, or infra detail to end users (`nit`); suggest selling
  the user benefit, not the mechanism. Skip if the ticket is about that copy.
- Ticket-intent mismatch (diff doesn't do what the ticket asked).

**Temper with the intent note** — before emitting any finding, check it against
the ticket's rationale, constraints, and discussion. A choice the ticket
explicitly justifies (deliberate hack, scoped approach, known debt with a
follow-up) is not a bug; drop it or downgrade to a soft note citing the
ticket's reasoning.

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
summary (P5). See `references/github-gitlab-commands.md` §2.

Plus a **2–4 sentence human summary per PR** written above the findings list.

**Token discipline (stories model):**
- **One Explore sweep per service+area, not per PR.** Orchestrator holds the
  investigation note (the cache); subagents reference it, never re-explore the
  same files.
- **Reuse ledger** — shared components/contracts recorded once; each review
  cites, doesn't re-derive.
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
- **Merge order** (producer PR first) — state it explicitly in the output. If
  compose doesn't encode the dependency (an HTTP response-field contract usually
  isn't a `depends_on` edge), infer it from the diffs: the PR that **adds/changes
  the field** is the producer → merge it first.

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
subagent's summary. Drop or downgrade any that don't hold. A finding that
**contradicts CI** is the loudest tell: claims a spec/build fails but the pipeline is
green → one of them is wrong, verify before posting. Conversely a "passed with
warnings" pipeline (a failed `allow_failure` job) often hides the real red job a
finding points at — pull that job's log (`references/github-gitlab-commands.md` §1)
to confirm. State how each contradiction resolved in the report.

**Prune against existing discussion.** Drop any finding a human already raised, the
author already answered, or that sits on a **resolved** thread (the existing-thread
list from P1). Re-reviewing a settled point is noise — the fastest way to get the
whole review muted. If the existing answer looks wrong, that's a *reply on the
existing thread* (out of Mode A's scope — note it in the report), not a fresh
duplicate comment.

Nits skip the blocker-verify pass (lower stakes) but still get pruned against
existing discussion.

## Phase 4 — Preview + confirm (the gate)

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

Then ask (one prompt for the whole set):

> **post** all / **edit** (drop or keep individual findings, tweak wording) /
> **cancel**

*Edit* = interactive pruning — present each finding; user keeps, drops, or
rewrites it; re-preview before proceeding. Not every finding has to go up. When a
PR already has a corgi summary from a prior run, the gate also asks **update the
existing summary vs post new** (default, and under `--yes`: **update**). Zero
findings → no *edit* option (nothing to prune); just post the clean summary or
cancel.

- Gate **on** by default — posting is outward-facing.
- `--yes` skips the gate and posts immediately.

## Phase 5 — Post

Exact commands live in `references/github-gitlab-commands.md` §2–4; use the forge from P0.

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

**Posting scenarios:**

1. **Clean PR (no findings)** — post one short "Reviewed — no blocking issues"
   summary (+ a line of what was checked / any praise). No inline. Don't go
   silent; don't spam.
2. **Nits only, no blockers** — inline the nits; summary headline "No blockers,
   N nits" so it doesn't read as alarming.
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

**Single PR** → one line `[<repo>] <summary headline>` + link, then counts.

**Multi-PR** → per-PR line + link (same `[<repo>] <summary headline>` form,
one per PR).

**Contract** section — whenever P3.5 ran (multi-PR set **or** a single monorepo
PR crossing a service boundary) — lists cross-service findings and merge order
(producer first).

**Totals line:**
```
N findings: B blocking, K nits;  P posted inline, S folded into summary.
```
Zero findings → `0 findings — clean.`. Wholly permission-blocked (scenario 6) →
`0 posted — printed locally (no write access).`.

**Skipped** line — list every ref that was NOT reviewed and why (merged/closed and
declined, blocked) so a skipped target isn't lost between P1 and the report.

List anything that couldn't be inlined explicitly (file, line, reason) — no
silent drops.

**No ceremony footer.** Don't append "review only — does not approve/merge" to every
report; it reads like a bot covering itself, and the user knows what a review is. The
*behaviour* stays hard-enforced (Guardrails) — just don't narrate it each time. Say
it in plain words **only** if it's actually in question (e.g. someone asks "so did
you block it?").

Example:

```
[api] No blockers, 2 nits
https://github.com/<org>/api/pull/42

[web] 1 blocking: missing null-check on user.address
https://github.com/<org>/web/pull/37

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
   else inline. **Never blind-apply.** Valid + in scope → fix. Wrong / out-of-scope /
   regresses → **reply why, don't apply** (push-back is a real answer). Needs an owner
   call → ask.
3. **Checkout the PR's OWN branch → fix → gate.** Clean tree → `gh pr checkout <n>` /
   `glab mr checkout <n>` (its head — **not** a new branch off base). Dirty tree →
   `git worktree add` off the fetched head so the user's work is untouched (`stories`
   P3 worktree rules). Gate: `corgi test --service` / `corgi exec` + scoped self-review
   (`stories` P3.5). **Minimum diff — only what the threads ask.**
4. **Reply + resolve per thread** (§5) — what changed (commit/line), or why you pushed
   back. **Reply INSIDE the reviewer's thread** — GitHub `in_reply_to`, GitLab
   `POST …/discussions/<id>/notes`; never `gh pr comment` / `glab mr note create`,
   which post a standalone PR-level note detached from the thread (reads as ignoring
   the reviewer, and resolving leaves their question visually unanswered).
   **Resolve only what you addressed**; a pushed-back thread stays **open**.
   **Durable convention → memory (confirm first).** If a resolved thread settles a
   lasting convention/decision for the stack and `.corgi/memory/` exists, draft a
   `decision` fact, show it, and write it on OK (`corgi memory add --type decision …`,
   then `corgi memory index`; see the `memory` skill). Absent → skip. **No secrets.**
5. **Gate → push.** Preview fixes + replies for the whole set in **one** gate (P4;
   `--yes` skips) → commit (repo style, issue key, no AI trailer) → `git push` each
   branch. **Draft stays draft; no force-push, no merge, no approve.** Fork PR / no
   push access → post replies only, say so.
6. **Report** — grouped by PR: per thread **applied** (commit/line) / **pushed back**
   (reason) / **needs you** (question); + each PR's push result + link. Multi-repo →
   state the producer-first push order.

## Guardrails (non-negotiable)

**Both modes:** **no secret values** echoed into comments/replies — flag location +
that a secret is present, never paste it. **Human voice** — terse, kind, specific
(problem + fix); no AI-attribution trailer, no walls, match the repo's density.
**Never touch `manualRun` services** when mapping via `corgi-compose.yml`.

**Mode A (give review):**
- **Comments only.** Never set a formal approve / request-changes state, never merge,
  never push, never modify the branch.
- **Read-only on the repo.** Never check out / write the PR branch; review from the
  fetched diff only.
- **Gate before posting** unless `--yes`. Posting is outward-facing.

**Mode B (address review):**
- **Explicit target only — never infer-and-push.** The PR/MR must come from a link, a
  bare number, or a story-id the user gave; it must be **your own** branch (you can
  push). Someone else's PR / a producer you don't own → stay in Mode A (comment), never
  write. One target ambiguous between several PRs → ask, don't pick.
- **Writes the branch — bounded.** Edit + push the PR's **own** branch only; **draft
  stays draft, never force-push, never merge, never approve.**
- **Gate before pushing** unless `--yes` — preview the fixes + replies first.
- **Don't blind-apply.** A wrong / out-of-scope suggestion gets a reasoned reply, not
  a commit. **Resolve only threads you addressed**; leave pushed-back ones open.
  **Minimum diff.**
