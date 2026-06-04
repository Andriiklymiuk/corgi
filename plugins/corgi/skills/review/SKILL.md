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

## Phase 3 — Review each PR (subagent, scoped)

## Phase 3.5 — Cross-service contract pass

## Phase 4 — Preview + confirm (the gate)

## Phase 5 — Post

## Phase 6 — Grouped report

## Guardrails (non-negotiable)
