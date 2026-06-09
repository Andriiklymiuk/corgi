---
name: suggest
description: Use when the user wants feature or improvement ideas for a corgi workspace — "suggest features", "what should we build next", "ideas to improve X", "how do we make this faster/safer/cheaper", "what's missing", "any new business cases". Scans the stack + its business domain + existing features and proposes RANKED, evidence-backed suggestions across two lenses — product/business (new features, new business cases, UX) and engineering (performance, reliability, security, cost, tech-debt, even a language rewrite when the ROI is high) — each tied to a measurable outcome. Specs the chosen one and offers to create a tracker story (asks where). NOT for implementing it (use the stories skill) or authoring corgi-compose.yml (use the corgi skill).
---

# Corgi suggest

Propose **real, measurable** improvements for a corgi workspace — the way a product
manager and a senior engineer would after actually reading the code and the
business. Two lenses, every idea grounded in evidence and tied to an outcome, then
**spec the chosen one** and **offer a story** (ask where). Suggests + specs; does not
implement (that's `stories`).

## Guardrails (non-negotiable)

- **Evidence or it doesn't ship.** Every suggestion cites a real signal — a
  `file:line`, a dependency, a metric, a concrete domain gap. No generic "add dark
  mode / add AI / add notifications" without a reason rooted in *this* stack. Slop is
  the failure mode; kill it.
- **Measurable outcome required.** Name the metric, a rough magnitude, how you'd
  measure it (p95 latency, signup completion, error rate, bundle size, infra $/mo,
  test coverage, MTTR). Can't tie it to an outcome → drop it.
- **Suggest, don't build.** Output = ranked shortlist → one spec → optional story
  handoff. No code changes here.
- **A rewrite is a claim, not a vibe.** "Rewrite service X to Go/Rust" only with a
  feasibility + ROI case: a hot path, a large measured/estimated gain, a migration
  path. Never for novelty.
- **Metrics/analytics on demand only.** Any perf/cost claim needing runtime data →
  reuse the `debug` skill's provider detection; ask before querying, scoped,
  read-only.
- **Honest effort + ranking.** Rank by impact/effort; don't bury a big lift as a
  "quick win".

## Phase 0 — Map the stack + the business

cwd must hold `corgi-compose.yml` (else tell the user to open the workspace folder).
Read once:
- **Stack** — `services`/`db_services`, `depends_on`, ports, each service's
  **language/runtime** (from `package.json`/`go.mod`/`Cargo.toml`/`pyproject`/
  Gemfile…), `manualRun` (reference-only). Schema:
  `../corgi/references/yml-schema.md`.
- **Business + existing features** — workspace README + per-service READMEs: what the
  product does, **who the users are, the business model**, what already exists.
  Product suggestions anchor to *this* domain — don't invent a generic SaaS.
- **Workspace memory** — if `.corgi/memory/` exists, run `corgi memory list --json`
  (or read `index.md`) and open the matching facts (see the `memory` skill). Don't
  propose what a `decision` rejected; ground a product idea in a `domain` fact; cite a
  past `incident` as evidence. Absent → skip silently.

## Phase 1 — Gather evidence (investigate once, parallel lenses)

Dispatch **one `Explore`/`Task` per lens** (not per idea; scan each area once,
orchestrator holds the evidence ledger). Each returns **cited** signals, not opinions:

- **Product / business** — table-stakes features missing for this domain;
  activation/retention/revenue levers; UX friction; per-service capability gaps vs
  domain norms. Cite the README/route/screen.
- **Performance** — hot/heavy paths, N+1, missing indexes/caching, big bundles,
  sync-where-async, chatty cross-service calls. Cite `file:line`. Need runtime
  numbers → `debug` provider (on demand).
- **Reliability / safety / security** — missing healthchecks, tests, structured
  logging/observability, error handling, retries/timeouts; authz gaps, secret
  handling, vulnerable/deprecated deps, data-loss risks. Cite the gap.
- **Cost / DX / tech-debt** — outdated deps, oversized files, duplication, slow CI, a
  hot service on a runtime costing latency/$$ (rewrite candidate). Cite it.

## Phase 2 — Turn each signal into a REAL suggestion

One card per candidate; any hand-wavy field → **cut the card**:

- **Title** + **lens** (product | eng).
- **Evidence** — the signal (`file:line` / dep / metric / domain gap).
- **Change** — concrete, which service(s).
- **Measurable outcome** — metric + rough magnitude + how measured. e.g. *"p95 of
  /search 1200ms → <400ms (add the missing index + a 60s cache)"*, *"signup
  completion +X% (remove the dead address step)"*, *"JS bundle 2.1MB → <900KB"*,
  *"MTTR ↓ via structured logs + one dashboard"*, *"infra $/mo ↓ moving the cron
  worker off the always-on dyno"*.
- **Effort** — S/M/L + the main risk.
- **Why now** — the business/engineering reason it matters for this stack.

## Phase 3 — Rank + present the shortlist

Adversarial pass on every card: *"real, measurable improvement, or generic slop?"*
Drop the slop. Rank by **impact/effort**. Present a tight shortlist (~5–8),
**mixing product + engineering**, a rewrite candidate only if it earns its place:

```
[eng] Add composite index on orders(creator_id, created_at)
   evidence: api/queries/orders.rb:88 — full scan; logs show /orders p95 ~1.2s
   outcome:  /orders p95 1.2s → <300ms; ~0 risk; effort S

[product] Email-digest of weekly activity
   evidence: notification-service exists but only sends transactional; README
             says retention is the Q3 goal
   outcome:  W2 retention lever — measure via cohort open→return; effort M
```

Offer the user to pick one (or a few).

## Phase 4 — Spec the chosen suggestion

Write `docs/suggestions/<slug>.md` — **reuse the `stories` spec shape** so it drops
straight into implementation: problem + evidence (`file:line`), change **grouped by
service**, the measurable outcome **and how to verify it**, effort/risk, rollout.
Multi-service → add `## Contract` + producer→consumer order.

## Phase 5 — Offer a story (ask where)

Ask: **"Create a story for this?"** If yes, ask **where**:
- **Tracker** — detect Linear vs Jira from the workspace (tracker URLs in
  README/compose, git remote); confirm. Namespace: Linear → `mcp__linear-server__*`,
  Jira → `mcp__atlassian__*`.
- **Project / service** — which tracker project, which service(s) the work lands in
  (paths from `corgi-compose.yml`).

**Preflight: confirm the matching tracker MCP is actually connected** before creating
anything. Missing → don't silently create in the wrong tracker: keep
`docs/suggestions/<slug>.md` as the deliverable + a paste-ready issue body + the
tracker's new-issue URL, and stop.

Connected → create the issue from the spec (Linear `mcp__linear-server__save_issue`
with `title`+`team` and no `id` / Jira `mcp__atlassian__createJiraIssue`) at the chosen location, link the spec,
report the key/link. Then offer **"Implement it now?"** → hand the approved spec
**and the created issue key** to the **`stories`** skill (it branches per service off
that key — don't let it re-create the issue). Declined → leave the spec; done.

## Scenarios & scaling

- **Big workspace** → lens agents per service-cluster in parallel; orchestrator
  merges + dedups the ledger before carding.
- **Perf/cost claims needing real numbers** → `debug` provider, on demand, ask first;
  else mark the magnitude an estimate and say so.
- **One spec → one story** at a time; batch more later via `stories`.
