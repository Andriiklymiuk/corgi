---
name: review
description: Use when the user wants a code review of one or more EXISTING pull/merge requests — any phrasing of "review this PR/MR" alongside GitHub PR or GitLab MR links/numbers (e.g. "review these MRs <link> <link>", "look over this PR <link>", "code review <link>", "check the api + web MRs for ABC-123", or a bare PR/MR link with "thoughts?"). Reviews against the repo's own standards (CLAUDE.md/AGENTS.md, lint config), pulls intent from any linked Linear/Jira ticket, runs a cross-service contract check when the set spans services, then posts a human summary comment + inline suggestions behind a preview gate. NOT for creating PRs from issues/feature text (that is the stories skill) or reviewing the local uncommitted diff (that is the built-in /code-review).
---

# Corgi review

Review one or more existing remote PR/MR(s) on GitHub or GitLab against each repo's own standards (CLAUDE.md/AGENTS.md, lint and format config) plus the intent from any linked Linear or Jira tracker ticket, then post a human-readable summary comment and inline line-level suggestions back onto each PR/MR — all behind a preview gate before anything goes public. It is the direct counterpart to the `stories` skill: stories **creates** draft PRs/MRs from issues or feature text; review **consumes** existing ones. It reuses stories' workspace model (services, dirs, and forges resolved from `corgi-compose.yml`) and stories' token-efficiency model (cluster by service+area, investigate once, orchestrator-as-cache, reuse ledger).

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
(`gh auth status` / `glab auth status`). Missing → stop with an install/auth hint;
don't guess.

A set may **span forges** (e.g. api MR on GitLab, web PR on GitHub) — resolve each
ref independently. Both forges are first-class.

**Group by repo/service up front.** Build a repo → [PRs] map. Multiple PRs to the
same repo share one standards note (P2) and one area sweep (P3) — the core token
saving.

**Auto-detect siblings (optional).** Given a single PR/MR, read its branch name; if
a `corgi-compose.yml` is in cwd, look for PRs/MRs on the **same branch** in the
workspace's other services' repos (stories opens sibling PRs with the same branch
name). Found → confirm the expanded set with the user before reviewing. **Never
auto-expand silently.**

## Phase 1 — Fetch (no checkout)

Per PR/MR, fetch **without checking out the branch** (non-destructive — never touch
the user's working tree). Exact commands live in `references/forge-api.md` §1; pick
the `gh` or `glab` column that matches the ref's forge.

Fetch per PR/MR:
- Title, body, author, base branch, head branch, changed-file list, URL, state,
  isDraft — metadata via `gh pr view … --json …` / `glab mr view …`.
- Unified diff — via `gh pr diff` / `glab mr diff`.
- Anchoring SHAs for inline comments: GitHub uses `path`+`line`+`side` on the
  latest head commit; GitLab needs `diff_refs` (`base_sha`/`head_sha`/`start_sha`).

**rtk:** metadata, status, and list calls go through rtk automatically (the Claude
Code hook rewrites `git`/`gh`/`glab`). Fetch the **reviewable diff raw** to avoid
truncation degrading review quality:
```
rtk proxy gh pr diff <n> --repo <owner>/<repo> --patch
rtk proxy glab mr diff <n> --repo <host>/<group>/<proj> --color=never
```
See `references/forge-api.md` §0 for the rule of thumb: rtk-filtered for
everything except the diff content (and any file body read in full).

**State.** Read the PR/MR state on fetch.
- Merged/closed → warn and skip by default; ask before reviewing a closed one (usually a mistake).
- Draft → review normally (reviewing drafts is the common case).

**Stacked PRs.** `gh pr diff` / `glab mr diff` diff each PR against its own base.
If PR B's base is PR A's branch, B's diff is already just its own delta — review
as-is. If both target trunk and B contains A's commits, detect the shared commit
range and review B only against the non-shared commits, so no lines are reviewed
twice. State the stacking in the report.

**Noise filter.** Drop generated/vendored/binary paths from the review surface:
lockfiles (`*.lock`, `package-lock.json`, `go.sum`, …), `vendor/`, `node_modules/`,
codegen output, images/binaries, and anything matching the repo's
`linguist-generated` / `.gitattributes` markers. Never comment on these; note the
count of paths skipped.

## Phase 1.5 — Tracker enrichment (intent)

Scan the **input** and **each PR/MR body** for ticket references:
`linear.app/…`, `atlassian.net/…`, or a bare `ABC-123` key.

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
`glab api`). **No full clone.** Missing → skip, don't chase.

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
- **Leaked secrets** — flag the file + line + that a secret is present; never echo the value into a finding or comment.
- Repo-standard / convention violations (names, patterns, style).
- Scope creep (changes outside the ticket's stated scope).
- Perf footguns.
- Ticket-intent mismatch (diff doesn't do what the ticket asked).

**Temper with the intent note** — before emitting any finding, check it against
the ticket's rationale, constraints, and discussion. A choice the ticket
explicitly justifies (deliberate hack, scoped approach, known debt with a
follow-up) is not a bug; drop it or downgrade to a soft note citing the
ticket's reasoning.

**Finding shape** (exact — used by P4/P5):
```
{ pr, file, line, side, severity: blocking|nit, title, explanation, suggestedReplacement? }
```
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
  file/dir group across parallel subagents, then merge + dedupe findings.
- Same-repo sibling PRs touching the same code → add a short **interaction
  note** (do they conflict or overlap?).

## Phase 3.5 — Cross-service contract pass

Triggers on a **service boundary being crossed** — not a repo boundary. Map
each changed file to its service via `corgi-compose.yml` paths. The boundary is
crossed by two PRs in different repos **or** by a single monorepo PR editing
two services' dirs (e.g. `api/` + `web/`). A set touching only one service
skips P3.5.

One reviewer sees **all boundary-crossing diffs together** plus the dependency
direction from `corgi-compose.yml`: `depends_on_services`, `exports`,
`${producer.VAR}` substitutions.

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
- **Merge order** (producer PR first) — state it explicitly in the output.

Contract findings are structured the same as P3 findings (same shape); tag
each with both affected PRs so P5 can post to both sides.

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
cross-service findings and the stated merge order (producer first).

Then ask (one prompt for the whole set):

> **post** all / **edit** (drop or keep individual findings, tweak wording) /
> **cancel**

*Edit* = interactive pruning — present each finding; user keeps, drops, or
rewrites it; re-preview before proceeding. Not every finding has to go up.

- Gate **on** by default — posting is outward-facing.
- `--yes` skips the gate and posts immediately.

## Phase 5 — Post

Exact commands live in `references/forge-api.md` §2–4; use the forge from P0.

**GitHub** — one review call (`event=COMMENT`): `body` = the PR's human summary
(tagged `<!-- corgi-review -->`), `comments[]` = all inline findings, each
suggestion a ` ```suggestion ` block for one-click apply. Summary + all inline
in **one** call.

**GitLab** — summary via `glab mr note create` (tagged `<!-- corgi-review -->`);
each inline finding as a separate `discussions` call with a `position` object;
suggested change as a ` ```suggestion:-0+0 ` block (Apply button).

**Finding that cannot be inlined** (line not in the diff) → fold into that PR's summary;
note it in the report. Never silently drop.

**Cross-service contract finding** → post to **both** sides, each cross-linked
(`see <other-PR-link>`), so each reviewer sees the full picture.

**Idempotency (summary + inline):**
- Every corgi-posted comment is tagged `<!-- corgi-review -->`.
- Re-run → detect the prior corgi summary → offer **update vs new** (patch the
  existing comment rather than stacking a duplicate).
- Inline deduped by `(file, line, title)` — same finding never posted twice.
- Findings whose line the author already changed are skipped (forge marks them
  outdated).

**Posting scenarios:**

1. **Clean PR (no findings)** — post one short "Reviewed — no blocking issues"
   summary (+ a line of what was checked / any praise). No inline. Don't go
   silent; don't spam.
2. **Nits only, no blockers** — inline the nits; summary headline "No blockers,
   N nits" so it doesn't read as alarming.
3. **Head moved during the gate** — re-fetch the head SHA right before posting.
   If it changed: warn, re-anchor inline findings against the new diff, drop
   any whose line vanished (fold into summary). If the diff changed materially,
   offer a re-review instead of posting stale comments. **Never post inline
   against a stale SHA.**
4. **Partial post failure** (inline rejected — line outside diff `422`, rate
   limit, transient `5xx`) — retry transient errors with backoff; fold
   still-failing findings into the summary; report posted-vs-failed counts. No
   silent half-post.
5. **Suggestion can't apply** (pure deletion, non-contiguous range, lines don't
   line up) — fall back to a normal inline comment with the proposed code in a
   **plain fenced block** (not ` ```suggestion `). Avoids a broken Apply button.
6. **No comment permission** (`403`, not a collaborator) — print the full review
   locally and tell the user to post manually or get access. Detect before
   attempting any posts; don't lose the work.
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

List anything that couldn't be inlined explicitly (file, line, reason) — no
silent drops.

**Footer line** (always last):
> review only — does not approve, request changes, or merge.

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

review only — does not approve, request changes, or merge.
```

## Guardrails (non-negotiable)

- **Comments only.** Never set a formal approve / request-changes state, never
  merge, never push, never modify the branch.
- **Gate before posting** unless `--yes`. Posting is outward-facing; the gate
  is not optional by default.
- **Read-only on the repo.** Never check out / write the PR branch; review from
  the fetched diff only.
- **No secret values** echoed into comments — flag location + that a secret is
  present; never paste the value.
- **Human voice.** Comments terse, kind, specific: problem + fix. No
  AI-attribution trailer. No walls of text. Match the repo's comment density.
- **Never touch `manualRun` services** when mapping via `corgi-compose.yml` —
  reference-only, same as stories.
