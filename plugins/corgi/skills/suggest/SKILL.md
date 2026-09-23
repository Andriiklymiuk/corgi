---
name: suggest
description: "Use when the user wants feature or improvement ideas for a corgi workspace: \"suggest features\", \"what should we build next\", \"what would make this magic\", \"ideas to improve X\", \"how do we make this faster/safer/cheaper\", \"what's missing\", \"any new business cases\". NOT for implementing it (stories), running it on a clock (suggest-proactive), or authoring corgi-compose.yml (corgi)."
---

# Corgi suggest

Find the few things worth building next in **this** stack - the way a product
engineer who read the code and used the product would - and turn the chosen one
into work: a story-shaped spec, a task on the board or a tracker ticket, and a
handoff to `stories`. Suggests and specs; never implements.

## What a good suggestion is

- **A user would feel it.** Magic is the thing the product almost does: the
  promise in the README the code does not keep, the data it stores and never
  shows, the flow that dead-ends, the three steps that could be one, the wait
  that could be a word.
- **You can point at it.** A `file:line`, a README line, a route with no test, a
  step the README says to do by hand, a TODO older than the last month of `git
  log`. Not a feeling about the domain.
- **You know when it worked.** The check that passes after, in one line. A
  number only when you have one - "p95 1.2s → <300ms" with a measurement, or
  "an estimate" said out loud.
- **Not** "add dark mode / add AI / add notifications / rewrite in Rust" unless
  the evidence in this stack says so. Slop is the failure mode.

## Guardrails (non-negotiable)

- **One pass, in this session.** Read the stack once, keep the evidence in
  your head, then write cards. No agent per lens, no fan-out: a second reader
  costs more than it finds.
- **Evidence or cut.** Every card cites something in the repo, the memory, the
  board or the product. A card with a hand-wavy field is cut, not softened.
- **Suggest, don't build.** Output is a shortlist, one spec, one place for it
  to live. No code changes here.
- **Honour what is known.** A memory `decision` that rejected it → don't
  propose it. What is on `corgi agent kanban`, in `corgi suggest-history
  list`, or in an open pull request → already taken.
- **A rewrite is a claim, not a vibe.** "Move service X to Go/Rust" only with a
  hot path, a measured or estimated gain and a migration path.
- **Metrics on demand only.** A claim that needs runtime numbers → the `debug`
  skill's provider, read-only, after asking; else say "estimate".
- Read `../_shared/conventions.md` first (attribution, `manualRun`, preflight).

## Phase 0 - Read the stack, once

Preflight per `../_shared/conventions.md`. Then, in this order:

- **Shape** - `corgi context --json`: services, languages, ports, branches,
  what is dirty. Schema in `../corgi/references/yml-schema.md`.
- **Promise** - the workspace README and each service's: what the product
  says it does, for whom, how money moves. The routes, screens or commands are
  the truth to check it against.
- **Memory** - `.corgi/memory/` if present (`corgi memory list --json`, see
  the `memory` skill): `decision` rules out, `domain` grounds, `incident` is
  evidence. Absent → skip silently.
- **Taken** - `corgi agent kanban --json` (the board), `corgi suggest-history
  list --json` (proposed before), open pull requests, `git log --since=4.weeks
  --oneline` (what the team is on).
- **Focus** - `$ARGUMENTS` narrows to a lens, a service or a goal
  ("performance", "the api", "retention"). Empty → all three lenses.

## Phase 1 - Three lenses, one walk

Walk the services once and note signals under three headings, cited as you
go. Stop around ten: enough for a shortlist, not a survey.

- **Magic** (what a user would feel): a promise not kept, a dead-end flow, an
  empty state that could act, data held and never shown, a manual step the
  product could take, a wait the user is not told about.
- **Friction** (what a developer here trips on): the README step done by
  hand, no test on the hot path, the script everyone copies, the service
  restarted by hand, the flaky job, the setting kept in three places.
- **Risk** (what will bite): no healthcheck, timeout or retry on a
  cross-service call, an unbounded query, a secret handled loosely, a
  deprecated dependency with an advisory, a path that loses data.

## Phase 2 - Cards; cut what is not real

One card per signal that survives:

- **Title** + **lens** (magic | friction | risk).
- **Evidence** - the citation.
- **Change** - concrete, by service.
- **Worked when** - the check that passes after; a number only if you have one.
- **Effort** - S/M/L and the main risk.
- **Why now** - the reason for this stack this month.

Any field you cannot fill honestly → cut the card.

## Phase 3 - Shortlist

Rank by what it changes for a user against what it costs. Three to six cards,
magic first when one earned it, a rewrite only with its ROI case:

```
★ [magic] Search by tag
   evidence: README "filter items by tag" - no route in api/routes.go; tags are stored (db/schema.sql:44)
   change:   api: GET /items?tag= · web: a tag chip on the list
   worked:   a tagged item shows under its tag; one test on the route
   effort:   S · risk: none · why now: tags exist and nobody can use them

  [friction] Seed the local database from one command
   evidence: README "run these 6 psql lines" - done by hand on every clone
   change:   api: `make seed` wrapping the six lines; corgi-compose afterStart runs it
   worked:   a fresh clone has data after corgi run
   effort:   S · risk: none · why now: two new people onboarded this month
```

Ask which one (or a few). Do not spec the rest.

## Phase 4 - Spec the chosen one

Write it in the `stories` spec shape so it drops into implementation as is:
problem and evidence (`file:line`), the change **by service**, `## Contract`
plus producer→consumer order when it spans services, **done when** (the
check), effort and risk, rollout. Show it in the answer; write
`docs/suggestions/<slug>.md` only if the user wants a file in the repo - the
spec otherwise lives on the task or the ticket.

## Phase 5 - Give it a home, then offer the build

Ask **"put it on the board?"** and where:

- **Task** - `corgi agent task add "<title>" --body -` with the spec on
  stdin. On the board, the phone and the editor now, no tracker needed;
  **Work on it** hands it to `stories`. The default when no tracker MCP is
  connected.
- **Tracker** - read `../_shared/tracker-mcp.md`; confirm the MCP is
  connected, else fall back to the task plus a paste-ready body and the
  tracker's new-issue URL. Create in draft/backlog, label `corgi-suggest`,
  link or paste the spec. One terse comment at most.
- **Just the spec** - leave the file; nothing filed.

Then record it so the proactive run never proposes it again:
`corgi suggest-history record --slug <slug> --status filed|proposed --ticket
<KEY or TASK-N> --title "<title>" --lens <magic|friction|risk>`.

Then **"implement it now?"** → the `stories` skill with the ticket key or the
task ref: it branches off that; don't let it re-create the issue. Declined →
done.

## Scenarios & scaling

- **A focus was given** → one lens or one service, still cited, still ranked.
- **No memory, no board, no history** → say so in one line and go on.
- **Numbers wanted** → `debug` provider, ask first, read-only; else "estimate".
- **Big stack** → walk service by service in the same pass; still ten signals.
- **Every idea is taken** → say what is on the board already and what you
  would have added; no card for its own sake.
