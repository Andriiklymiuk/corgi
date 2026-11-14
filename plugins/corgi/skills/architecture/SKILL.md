---
name: architecture
description: "Use to improve how an app or a workspace is put together: \"improve the architecture\", \"this is a big ball of mud\", \"where are the boundaries\", \"untangle these modules\", \"should this be its own service\", \"why does every change touch five places\". Maps what is there with evidence, finds the seams that hurt, moves them in small green steps. NOT one function (complexity), defects (quality) or a PR (review)."
---

# Improve the architecture

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** the map as it is was drawn from the code, every problem names its evidence and the change
it makes slow or risky, the moves were ranked, each move done was proven with the tests unchanged, and
the rest is written down as decisions with their cost.

## What this is for

Architecture is what makes a common change cheap or expensive. A diagram with nicer boxes that leaves
the same five files changing together fixed nothing. Start from a change people make often and work
back to the boundary that makes it hard.

```
the change that hurts ──▶ map as it is ──▶ measure ──▶ problems as scenarios ──▶ target
                                                                                   │
                      report ◀── record decisions ◀── move one seam, prove it ◀────┘
```

## 1. The change that hurts

Ask once, or find it: what do people change most, and what makes it slow? "Adding a payment method
touches the API, two workers and the admin", "a field rename broke the mobile app". No change named →
use the history:

```bash
git log --since=6.months --format= --name-only | awk -F/ 'NF>1{print $1"/"$2}' | sort | uniq -c | sort -rn | head -15
```

Read the repo's CLAUDE.md, AGENTS.md, ADRs (`docs/adr`, `docs/decisions`) and any lint rule that already
fences imports. A decision someone wrote down is not reopened without a reason that is new.

## 2. Map it as it is

From the code, never from the README. ASCII, one box per module or service, arrows for "depends on".

- **Inside one repo:** the import graph. Go: `go list -f '{{.ImportPath}} {{join .Imports " "}}' ./...`.
  TS/JS: `npx madge --circular --extensions ts,tsx src` or `npx depcruise src --output-type dot`.
  Python: `pydeps` or `import-linter`. Swift: the targets in Package.swift and who imports whom.
- **A corgi workspace:** the services are already a graph. `corgi-compose.yml` names each service's
  `depends_on_services` and `depends_on_db`; `corgi run --dry-run` and `corgi why <service>` show it
  resolved. Add what compose cannot see: queues, webhooks, a shared bucket, a cron that writes another
  service's table. Grep the env files for other services' hosts.
- **Who owns the data:** for each table or collection, which module or service writes it. Two writers
  of one table is a boundary that is not there.

## 3. Measure, so the problems are not opinions

| signal | how | what it means |
|---|---|---|
| cycles | madge `--circular`, `go vet` refuses them, depcruise `no-circular` | two modules that are one module |
| hubs | fan-in and fan-out per module from the graph | a hub with high fan-out changes with everything |
| change coupling | pairs of folders in the same commits (below) | a boundary on paper that history ignores |
| layer leaks | domain code importing http, db, ui or a vendor SDK | logic that cannot be tested or moved alone |
| shared database | two services in `depends_on_db` of one db writing the same tables | a deploy of one breaks the other |
| call chains | A calls B calls C on one user request | latency and failure multiply |
| copies | the same rule in two services (pricing, permissions, validation) | they drift; one is wrong today |

```bash
git log --since=6.months --format=tformat:COMMIT --name-only | awk -F/ '
  /^COMMIT$/ { for (a in d) for (b in d) if (a < b) print a, b; delete d; next }
  NF > 1 { d[$1"/"$2] = 1 }' | sort | uniq -c | sort -rn | head -15
```

High counts between folders that are meant to be apart are the strongest evidence there is.

## 4. Problems as scenarios

Each one: the change, what it touches, what broke or slowed, the numbers. "Too coupled" is not a
problem. "Adding a field to Order changed 11 files in 4 packages in each of the last 6 times
(commits …); 2 of them broke the worker" is.

Rank by how often the change happens times what it costs, divided by the cost of the move.

## 5. The target: the smallest shape that fixes the top problem

- **Boundaries in place before new places.** A package with one public entry and private insides
  fixes more than a new service. Split a service only for a different deploy cadence, scale or team;
  never for tidiness. One team, one database, one deploy: a modular monolith is the answer.
- **Dependencies point inward.** Domain code knows nothing of transport, storage or vendors; those
  depend on it through an interface the domain owns. Only where the leak caused a problem in step 4.
- **One owner per table.** Another service reads through the owner's API or an event, not the table.
- **No layers for their own sake.** No repository over an ORM with one caller, no interface with one
  implementation and no test double. Name the problem each abstraction removes.

Draw the target next to the map, same boxes, and mark only what moves.

## 6. Move one seam at a time

A move is a chain of small refactors, each with the tests green and no test edited:

1. **Fence first.** Add the rule that will hold the boundary, set to warn or scoped to the new code:
   `depguard` / `go-arch-lint`, dependency-cruiser `forbidden`, `import-linter` contracts, an eslint
   `no-restricted-imports`. It fails on the violations you are about to remove, so it proves the move.
2. **Strangle, do not rewrite.** New code goes behind the new boundary; old callers move over one
   at a time; the old path is deleted when nothing calls it.
3. **Move, then change.** A file moved and a behaviour changed in one commit cannot be reviewed.
4. **Data last.** Split reads first (an API or a view), then writes, then the table, each released
   on its own. A migration that cannot be rolled back is a plan for a person, not a step for you.
5. **One move, one PR.** `corgi:prep-pr` for it; a move across several repos is a story: `corgi:stories`.

Over a day's work, a public interface, or a data migration: do not start it. Write it down.

## 7. Write the decisions down

Each decision: the context (the scenario and its numbers), the decision, what it costs, what was
rejected and why. In the repo's ADR folder when it has one, else `docs/adr/NNNN-title.md`. To share
it for comments: `corgi idea new --type hld --from docs/adr/NNNN-title.md`. The fence rule from step 6
is the part of the decision that enforces itself.

## 8. Report

```
map      7 packages, 2 cycles (billing ↔ orders, api ↔ auth), orders fan-out 9
problem  a new payment method touches 11 files in 4 packages (6 of 6 times since May)
moved    billing behind billing.Service; cycle billing ↔ orders gone; depguard rule added   tests 412 = 412
left     orders and invoices share the invoices table          a migration, ADR 0007 written
none     co-change billing/orders: 23 → 4 in the commits since
```

Say what you did not look at: runtime traffic, production load, other teams' consumers.

## How the pass goes wrong

- A rewrite proposed where a boundary in place would do; a new service for a team of one.
- A map drawn from the README or folder names; the imports say something else.
- Patterns applied by name (hexagonal, clean, DDD) with no problem from step 4 behind them.
- A move that renames and reformats as it goes, so the diff hides the change.
- A boundary added with no rule to hold it; it is gone in a month.
- Decisions left in chat; the next session reopens them.

## See also

- **[`complexity`](../complexity/SKILL.md)** for the one function that is a jungle, **[`quality`](../quality/SKILL.md)** for defects.
- **[`idea`](../idea/SKILL.md)** to share a design for comments, **[`stories`](../stories/SKILL.md)** for a move across repos.
- **[`corgi`](../corgi/SKILL.md)** when the answer is a service boundary: compose `depends_on_services`, `runner`.
