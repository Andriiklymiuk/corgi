---
name: tracker
description: Use for the tracker side of a corgi workspace — Linear or Jira. Three jobs: (1) status / standup — "where are we", "what's blocked", "generate standup", "is the sprint on track"; (2) triage — "triage the inbox", "label these", "any duplicates"; (3) decompose — "break this epic into tickets", "turn this feature into stories". Its edge over the tracker's own UI: it ties each ticket to its REAL code state — branch, draft/open/merged PR/MR, CI — across every service in corgi-compose.yml (GitHub or GitLab), so drift like "In Progress but no branch" or "Todo but the PR already merged" surfaces. Read-only until a single confirm gate guards any tracker write. Manager / lead / founder lens. NOT for implementing tickets (stories), improvement ideas (suggest), reviewing PRs (review), running/diagnosing the stack (run/debug), or authoring corgi-compose.yml (corgi). Hands its tickets to stories.
---

# Corgi tracker

Read the issue tracker (Linear or Jira) **and** the `corgi-compose.yml` workspace,
**tie each ticket to its real code state**, and answer three kinds of question:
**status**, **triage**, **decompose**. It feeds **`stories`** (build) the same way
**`review`** feeds the back of the loop. Reports and plans — it never writes code.

**The one thing a tracker UI can't do:** it doesn't know your service→repo map, so it
can't tell you a ticket marked *In Progress* has no branch, or a *Todo* whose PR
already merged. This skill does — that correlation is its whole reason to exist. When
the compose is on disk, never report tracker status without it.

## Guardrails

- **Read-only until the gate.** Pulling tracker + forge state is free and silent. Any
  **write** (create / move / label / set priority / comment) is batched behind **one
  confirm**. `--yes` skips the asking.
- **Correlate, don't assume.** A ticket's true state = tracker status **+** its PR/CI
  state. Report the mismatch; never paper over it. Never invent an estimate, status,
  or assignee the tracker doesn't have.
- **Plan, don't build.** Output is a report or tracker tickets. Code → `stories`;
  ideas → `suggest`. Hand off; don't cross the line.
- **Never touch `manualRun` services** when mapping work to repos.

## Phase 0 — Workspace + tracker + forge

- **Workspace** — `ls corgi-compose.yml *.corgi-compose.yml`. Present → read the
  **service → dir** map + dependency order (`path`/`cloneFrom`, `depends_on_services`;
  exclude `manualRun`; schema `../corgi/references/yml-schema.md`). Absent → degrade:
  tracker-only, skip Phase 1, and **say** the code column wasn't checked.
- **Forge per repo** — `git -C <dir> remote get-url origin` → `gh`/`glab` (a workspace
  may span both). Needed to read PR/CI state.
- **Tracker** — Linear (`linear.app` / key shape) → `mcp__linear-server__*`; Jira
  (`atlassian.net` / project key) → `mcp__atlassian__*`. Both connected + a bare key →
  ask. **Neither connected** → name what to connect + offer a git-only digest (open
  PRs + recent commits). Tool names + the cycle/board/search calls per tracker:
  `references/tracker-and-forge.md`.

## Phase 1 — Correlate ticket ↔ code (the superpower)

For each in-scope ticket (skip only when no compose on disk):

1. **Find its PRs.** Prefer the tracker's **own git links** (Linear attachments /
   Jira dev-panel). Fallback: list PRs/MRs whose head branch **contains the key**
   across each non-`manualRun` repo (`stories` uses `feature/<KEY>/<slug>`, same name
   per repo). Record per ticket: **none / draft / open / merged / closed** + the link
   + **CI** (`gh pr checks` / `glab ci status`). Read-only; never check out a branch.
2. **Flag the drift** — the part the tracker can't see:

   | Tracker | Code | Report as |
   |---------|------|-----------|
   | In Progress | no branch/PR | **not actually started** |
   | In Progress | open PR, CI red | **blocked on CI** → `/corgi-debug` |
   | In Review | no PR | drift — nothing to review |
   | Todo / Backlog | PR merged | **stale — close it** (gate) |
   | Done | PR open | **premature done** |
   | any | open PR, no review | needs a reviewer → `/corgi-review` |

Hold this in one cache; the jobs below read it, never re-query.

## Job 1 — Status / standup

Group the correlated tickets, lead with **blockers + drift** (that's what standup is
for), each line carrying its PR + CI:

```
Cycle 24 · day 6/10 · 14 issues
🔴 Blocked / drift
   ABC-122 api  Webhook retries   PR #255 open, CI ✗      → /corgi-debug
   ABC-130 web  New onboarding    In Progress 4d, no branch  ⚠
🟢 On track
   ABC-118 api  Add phone field   PR #251 draft, CI ✓     → ready to land
✅ Done (5) · 🗒 Todo (3) · ⏳ Stale (1: ABC-077, 23d)
```

End with the **burn read** (can the open points land in the days left? name the
reason if not) and next actions, each routed to a skill. **No cycle/sprint** (Jira
Kanban, Linear without cycles) → group by board column / status instead — same
correlation, no burn line. Read-only by default; "plan next sprint" → propose a set
that fits capacity (tracker's velocity if it exposes one, else ask — never invent
one), carry-over first, producer-before-consumer order; offer to move it in (gate).

## Job 2 — Triage

For each untriaged issue propose — don't apply yet — **label/area** (map the text to
a service via compose names + READMEs), **priority** (from real signals, else
`needs-info` + the question), **assignee** (by area ownership if known, else leave
it), **duplicate** (link a candidate, never auto-merge). Present a table → gate →
batch the writes. Ambiguous → leave for the human, flag it.

## Job 3 — Decompose epic → tickets

Turn a feature/epic/roadmap into **buildable, ordered tickets** (the input `stories`
wants):

1. Scope it (read the epic + existing children to not dup; free-text → settle the
   boundary with the user first).
2. **One ticket per coherent unit of work per service** (the grain `stories` branches
   from). Cross-service feature → a **producer** ticket (contract owner) + **consumer**
   ticket(s), with blocks-links encoding the order.
3. Each ticket: title, intent, the service(s), acceptance criteria, a labelled
   T-shirt estimate (never a false-precise number). Multi-service → a one-line
   contract note.
4. Preview the set + the order → **gate** → create in the tracker (parented, linked).
5. Offer "build these now?" → hand the **keys** to **`stories`** (don't let it
   re-create the issues).

## The write gate

One preview, one confirm, for the whole batch (never per-issue):
```
Write to <Linear|Jira>:  create 3 under EPIC-9 (api×1, web×2) + links · move ABC-118 to Cycle 25 · set GHI-4 High
Proceed?  apply / edit / cancel
```
Gate on by default; `--yes` skips it. **Preflight the MCP is connected** before
promising a write — not connected → keep the plan + a paste-ready body + the new-issue
URL and stop. Idempotent: match on title/key, update instead of double-creating.

## Hand-offs & degrade

- Build a ticket → **`stories`** · need ideas → **`suggest`** · open unreviewed PR →
  **`review`** · blocked on a failing/deployed thing → **`debug`** · draft + green,
  ready to land → the ship/land flow.
- **No tracker MCP** → git/forge-only digest. **No compose** → tracker-only, code
  column omitted (say so). **Mixed forges / both trackers** → resolve per repo/issue.
- Big cycle (>~30 issues) → one `Explore` per service-area for the Phase 1
  correlation in parallel, merge into the one cache. Daily cadence → wrap in `/loop`,
  don't build a scheduler here.
