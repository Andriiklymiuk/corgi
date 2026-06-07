---
name: tracker
description: Tracker-side work for a corgi workspace (Linear or Jira). Four jobs — status/standup ("where are we", "what's blocked", "generate standup", "is the sprint on track"); triage ("triage the inbox", "label these", "dupes"); decompose ("break this epic into tickets", "turn this feature into stories"); pickup ("/corgi-queue", "pick up the agent tickets", "what's ready to build" — build-ready tickets, drift-skipped, handed to the stories skill to build). Its edge over the tracker UI: it ties each ticket to its REAL code state — branch, draft/open/merged PR, CI — across every corgi-compose.yml service (GitHub or GitLab), so drift like "In Progress but no branch" or "Todo but already merged" surfaces. Read-only itself (it dispatches to stories, never writes code); one confirm gate guards any tracker write. NOT for implementing tickets (stories), ideas (suggest), reviewing PRs (review), running/diagnosing the stack (run/debug), or authoring corgi-compose.yml (corgi).
---

# Corgi tracker

Read the tracker (Linear or Jira) **and** `corgi-compose.yml`, **tie each ticket to
its real code state**, and do four jobs: **status**, **triage**, **decompose**,
**pickup**. Reports, plans, and dispatches to `stories` — never writes code itself.

**Why not just the tracker UI:** it doesn't know your service→repo map, so it can't
tell you an *In Progress* ticket has no branch or a *Todo* whose PR already merged.
That correlation is the point — when compose is on disk, never report status without
it.

**Exact tracker calls (Linear + Jira tools, JQL, forge queries) live in
`references/tracker-and-forge.md` — read it before calling the tracker; don't guess a
tool name.**

## Guardrails

- **Read-only until the gate.** Reading tracker + forge is free; any **write**
  (create/move/label/priority/comment) batches behind **one confirm** (`--yes` skips
  asking).
- **Correlate, don't assume.** True state = tracker status **+** PR/CI. Report
  mismatches; never invent an estimate/status/assignee the tracker lacks.
- **Plan, don't build** — code → `stories`, ideas → `suggest`. **Never touch
  `manualRun`.**

## Phase 0 — Workspace + tracker + forge

- **Workspace** — `ls corgi-compose.yml *.corgi-compose.yml`; read service→dir +
  dependency order (`path`/`cloneFrom`, `depends_on_services`, exclude `manualRun`;
  schema `../corgi/references/yml-schema.md`). Absent → tracker-only; skip Phase 1,
  say the code column wasn't checked.
- **Forge** — `git -C <dir> remote get-url origin` → `gh`/`glab` (may span both).
- **Tracker** — Linear (`linear.app`/key) → `mcp__linear-server__*`; Jira
  (`atlassian.net`/project key) → `mcp__atlassian__*`. Both + bare key → ask. Neither
  connected → name what to connect, offer a git-only digest.

## Phase 1 — Correlate ticket ↔ code (the superpower)

Per in-scope ticket (skip only if no compose): find its PRs — prefer the tracker's
own git links (Linear attachments / Jira dev-panel), else list PRs whose head branch
contains the key per repo. Record **none/draft/open/merged/closed** + link + CI.
Read-only; no checkout. Then flag drift:

| Tracker | Code | Report as |
|---------|------|-----------|
| In Progress | no branch/PR | **not started** |
| In Progress | open PR, CI red | **blocked on CI** → `/corgi-debug` |
| In Review | no PR | drift — nothing to review |
| Todo/Backlog | PR merged | **stale — close** (gate) |
| Done | PR open | **premature done** |
| any | open PR, no review | needs a reviewer → `/corgi-review` |

Hold one cache; the jobs below read it, never re-query.

## Job 1 — Status / standup

Group the cache, lead with **blockers + drift**, each line carrying PR + CI:
```
Cycle 24 · day 6/10 · 14 issues
🔴 ABC-122 api  Webhook retries  PR #255 open, CI ✗  → /corgi-debug
   ABC-130 web  New onboarding   In Progress 4d, no branch ⚠
🟢 ABC-118 api  Add phone field  PR #251 draft, CI ✓ → ready to land
✅ Done 5 · 🗒 Todo 3 · ⏳ Stale 1 (ABC-077, 23d)
```
End with the **burn read** (can the open points land in the days left? why not) +
next actions routed to skills. **No cycle** (Jira Kanban / Linear without cycles) →
group by status column, no burn line. "Plan next sprint" → propose a set within
capacity (tracker velocity if exposed, else ask), carry-over first,
producer-before-consumer; offer to move it in (gate).

## Job 2 — Triage

Per untriaged issue, propose (don't apply): **label/area** (map text → service via
compose + READMEs), **priority** (real signals, else `needs-info` + the question),
**assignee** (by ownership if known, else leave), **duplicate** (link a candidate).
Table → gate → batch-write. Ambiguous → leave it, flag.

## Job 3 — Decompose epic → tickets

Feature/epic → **buildable, ordered tickets** (what `stories` wants):
1. Scope it (read the epic + existing children to not dup; free-text → settle the
   boundary first).
2. **One ticket per unit of work per service.** Cross-service → a **producer** ticket
   + **consumer**(s), blocks-links encoding order.
3. Each: title, intent, service(s), acceptance criteria, T-shirt size (never
   false-precise). Multi-service → a one-line contract note.
4. Preview set + order → gate → create (parented, linked) → offer "build these now?"
   → hand **keys** to `stories` (don't let it re-create them).

## Job 4 — Pickup (build-ready tickets → stories)

Two ways in, same dispatch. **Read-only here — `stories` owns the build + its spec
gate.**
1. **Resolve:** explicit links/keys passed in → those; **none → auto-pick the agent
   queue** (label `agent`, not In Progress/Done, not blocked).
2. **Drop drift** (Phase 1): skip anything already merged or with an open PR — flag,
   don't rebuild. Both modes.
3. **Present + confirm** (one line each, size + service; auto-pick default = all
   ready).
4. **Hand picked keys to `stories`** — it builds and **moves each ticket to the
   team's in-progress state as its branch is created** (`stories` Phase 3). That move
   de-dupes a looping `/corgi-queue` (auto-pick takes only not-In-Progress). Loop
   `/loop 1h /corgi-queue` to drain unattended. Empty / all-drift → say so.

## The write gate

One confirm for the whole batch (never per-issue):
```
Write to <Linear|Jira>:  create 3 under EPIC-9 + links · move ABC-118 to Cycle 25 · set GHI-4 High
apply / edit / cancel
```
On by default (`--yes` skips). **Preflight the MCP is connected** — if not, keep the
plan + a paste-ready body + the new-issue URL, stop. Idempotent: match on title/key,
update not duplicate.

## Hand-offs & degrade

- Build → `stories` · ideas → `suggest` · unreviewed PR → `review` · blocked/deployed
  → `debug` · draft + green → land it (`gh pr merge` / `glab mr merge`).
- **No tracker MCP** → git-only digest. **No compose** → tracker-only (code column
  omitted, say so). **Mixed forges/trackers** → resolve per repo/issue.
- Big cycle (>~30) → one `Explore` per service-area for Phase 1, merge into the cache.
