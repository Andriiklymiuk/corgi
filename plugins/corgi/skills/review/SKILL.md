---
name: review
description: Use when the user wants a code review of one or more EXISTING pull/merge requests — any phrasing of "review this PR/MR" alongside GitHub PR or GitLab MR links/numbers (e.g. "review these MRs <link> <link>", "look over this PR <link>", "code review <link>", "check the api + web MRs for ABC-123", or a bare PR/MR link with "thoughts?"). Reviews against the repo's own standards (CLAUDE.md/AGENTS.md, lint config), pulls intent from any linked Linear/Jira ticket, runs a cross-service contract check when the set spans services, then posts a human summary comment + inline suggestions behind a preview gate. NOT for creating PRs from issues/feature text (that is the stories skill) or reviewing the local uncommitted diff (that is the built-in /code-review).
---

# Corgi review

Review one or more existing remote PR/MR(s) on GitHub or GitLab against each repo's own standards (CLAUDE.md/AGENTS.md, lint and format config) plus the intent from any linked Linear or Jira tracker ticket, then post a human-readable summary comment and inline line-level suggestions back onto each PR/MR — all behind a preview gate before anything goes public. It is the direct counterpart to the `stories` skill: stories **creates** draft PRs/MRs from issues or feature text; review **consumes** existing ones. It reuses stories' workspace model (services, dirs, and forges resolved from `corgi-compose.yml`) and stories' token-efficiency model (cluster by service+area, investigate once, orchestrator-as-cache, reuse ledger).

## Phase 0 — Resolve target(s)

## Phase 1 — Fetch (no checkout)

## Phase 1.5 — Tracker enrichment (intent)

## Phase 2 — Standards note (once per repo)

## Phase 3 — Review each PR (subagent, scoped)

## Phase 3.5 — Cross-service contract pass

## Phase 4 — Preview + confirm (the gate)

## Phase 5 — Post

## Phase 6 — Grouped report

## Guardrails (non-negotiable)
