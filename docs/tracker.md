# Tracker & queue — plan and pick up work

The `tracker` skill and the `/corgi-queue` command are the **front of the corgi
agent loop**: they read your issue tracker (Linear or Jira) **and** your
`corgi-compose.yml`, tie each ticket to its real code state, and turn that into a
plan, a triaged inbox, or build-ready work handed straight to the `stories` skill.

```
suggest  →  tracker / queue  →  stories  →  review  →  you land it
 (ideas)      (plan, pick)       (build →    (review)
                                  draft PRs)
```

The full loop: **suggest → plan → stories → review**, with `run`/`debug` operating
throughout. `suggest` is upstream — it proposes work before it's even a ticket.

`suggest` can also run **proactively on a schedule** (`/corgi-suggest-proactive` armed
via `/schedule`): each run reuses the suggest ranking, dedupes the top idea against open
tickets + recently-dismissed ones, and — behind the same tracker write gate — either
proposes it (default) or, only if you opt in, files **one** rate-limited **draft** ticket.
See the `suggest-proactive` skill.

## Setup

You need the tracker's MCP server connected to Claude Code:

- **Linear** — the Linear MCP (`mcp__linear-server__*`).
- **Jira** — the Atlassian MCP (`mcp__atlassian__*`).

corgi auto-detects which from your workspace (tracker URLs in the README/compose, or
the issue-key shape). If neither is connected, `tracker` degrades to a **git-only
digest** (open PRs + recent commits) and tells you what to connect.

## The four jobs

You don't need the commands or any jargon — say it however you'd say it. It matches
on intent.

| Job | Say something like | What you get |
|-----|--------------------|--------------|
| **Status / standup** | "where are we", "what's blocked", "are we on track", `/corgi-tracker standup` | the cycle/sprint reconciled against real PRs + CI, leading with blockers and drift, plus a burn read |
| **Triage** | "sort and prioritize the new bugs", "any duplicates" | per-issue label / priority / assignee / dup / needs-info proposals, applied behind one confirm |
| **Decompose** | "break EPIC-9 into tickets", "turn this feature into stories" | ordered, service-mapped tickets (producer → consumer) created under the epic |
| **Pickup** | "find me something to work on", `/corgi-queue` | build-ready tickets handed to `stories` → draft PRs |

## The superpower: ticket ↔ code correlation

A tracker UI shows you cards. `tracker` shows you cards **reconciled against your
actual repos** — because it knows your service→repo map from `corgi-compose.yml`. It
flags the drift the tracker can't see:

| Tracker says | Code says | It reports |
|--------------|-----------|------------|
| In Progress | no branch/PR | **not actually started** |
| In Progress | open PR, CI red | **blocked on CI** → `/corgi-debug` |
| Todo / Backlog | PR merged | **stale — close it** |
| Done | PR open | **premature done** |
| any | open PR, no review | **needs a reviewer** → `/corgi-review` |

## Pickup scopes

`/corgi-queue` resolves what to build from your words (default = the `agent` queue):

```
/corgi-queue                    # tickets labelled `agent` that are ready
/corgi-queue in ready           # your Ready status column
/corgi-queue from backlog       # the backlog
/corgi-queue most impactful     # highest-priority ready work first (= "most roi")
/corgi-queue bugs               # ready bug-type tickets (built bug-tier: red test first)
/corgi-queue ABC-140 ABC-141    # exactly these tickets
/loop 1h /corgi-queue           # keep draining on a schedule (you still approve each spec)
```

Every scope filters to **not In Progress / not Done / not blocked**, then
**drift-skips** anything already merged or in-flight (so nothing gets built twice).

The loop isn't zero-touch: each round's **batch** of picks passes through `stories`'
**one spec sign-off** before any branch — batch-level (all the round's specs in a
single gate, not one per ticket). The loop *stages* a batch for your approval, then
builds the approved ones. Convenient, not autonomous.

## What happens when you pick work

Picking one ticket quietly engages the rest of the loop:

1. **tracker** correlates + drift-skips, you confirm the picks.
2. **stories** builds: reads `corgi-compose.yml`, branches per service, runs tests,
   calls **`debug`** if it needs runtime/staging data, uses **`corgi run`** to stand a
   producer up so a consumer can verify, runs an internal review pass, and opens
   **draft** PRs.
3. As each branch is created, **stories moves the ticket to In-Progress and assigns it
   to you** (states resolved from the tracker, never hardcoded), then **to Code Review /
   In Review when its draft PR opens** (if your team has that state). The In-Progress
   move also stops a looping `/corgi-queue` from grabbing the same ticket twice.
4. It then points you to the next steps: **`/corgi-review`** to review against your
   standards + the ticket, **`/corgi-run --service-branch …`** to see the branch live,
   **`/corgi-debug`** if CI is red — then you land it (`gh pr merge` / `glab mr merge`).

## Guardrails

- **Read-only until a gate.** Reading the tracker + forge is silent; any tracker write
  (create / move / label / comment) batches behind **one confirm** (`--yes` skips it).
- **tracker never writes code** — it dispatches to `stories`, which owns the build and
  its own spec sign-off gate.
- **Never touches `manualRun` services.**
- **Degrades gracefully** — no tracker MCP → git-only digest; no compose on disk →
  tracker-only (and it says the code column wasn't checked).

## See also

- [`docs/agents.md`](agents.md) — driving corgi non-interactively (`--json`, exit codes).
- [`docs/mcp.md`](mcp.md) — corgi's own MCP server.
- The Claude Code plugin section of the [README](../README.md#ai-agents-mcp--claude-code).
