---
name: suggest-proactive
description: Use when corgi should push ideas on its own instead of being asked: "suggest things automatically", "run suggest every week", "put the proactive bot on", "be a proactive engineer / push me work", "why did the proactive bot propose X", or when a daemon routine or a scheduled job invokes /corgi-suggest-proactive. NOT for on-demand ideas (suggest) or implementing anything (stories).
---

# Corgi proactive suggest — the Proactive bot

Once a week the Proactive bot reads the stack the way `suggest` does, picks
**one** thing worth building, and puts it on the board as a task with its
evidence — where **Work on it** on the phone, the bar or the editor hands it to
`stories`. It never writes into the repository, never files on the tracker
unless the workspace opted in, never builds.

Two ways it runs:

- **On the clock** (the default) — a daemon routine under the bot's soul:
  ```
  corgi agent bot add proactive --template proactive --workspace <id>
  corgi agent routine add suggest --bot proactive --workspace <id>   # weekly Mon 09:30; --schedule to change
  corgi agent restart
  ```
  The watch's caps, quiet hours and budget apply. The run is filed under the
  bot (`corgi agent bot show proactive`), its headline is one inbox row, the
  task is on the board. `corgi agent routine run suggest` runs it now.
- **By hand or from another scheduler** — `/corgi-suggest-proactive
  [/abs/workspace]`: the same flow, with a person maybe present.
  `references/schedule-config.md` covers `/schedule` and `CronCreate`.

## Guardrails (non-negotiable)

- **One idea per run**, at most `maxPerWeek` (default 1, hard ceiling 3) —
  `corgi suggest-history check` says when the week is spent.
- **Ranking is `suggest`'s** (its Phases 0–3): consume the shortlist, never
  re-invent the lenses or the evidence rule.
- **Dedupe before anything:** the board (`corgi agent kanban --json`: open
  tasks and tickets by title), the history (`filed` always; `proposed` and
  `dismissed` within the 30-day cooldown), workspace memory (a `decision` or
  a "won't build"), open pull requests.
- **The output is a task, not a file.** `corgi agent task add`, nothing in
  `docs/`. A tracker ticket only with `suggest.autoFileDrafts` on, and then
  draft-only: no assignee, never past backlog, never a build, never a PR.
- **Unattended never asks.** A routine run (the `corgi watch ·` trail, or no
  one to answer) takes the recommended choice, records `proposed`, and ends on
  the headline.
- **Idempotent.** Same slug → skip, never a duplicate.
- **Say the mode first:** `proactive · <workspace> · mode=propose · cap=1/week`.
- Read `../_shared/conventions.md` first.

## Phase 0 — Workspace, mode, state

1. **Workspace.** `$ARGUMENTS` (absolute path) or cwd; a routine already runs
   in the workspace directory. Preflight per conventions; none → stop with
   the "open the workspace folder" line.
2. **Mode.** `corgi suggest-history config --json` → `{autoFileDrafts,
   maxPerWeek}`; absent → propose, 1. Print the mode line.
3. **State.** `corgi suggest-history list --json`, `corgi agent kanban
   --json`. If `check --slug _` answers `rate-limit`, the week is spent:
   `record --status skipped`, headline "nothing this week: cap reached", stop.

## Phase 1 — Rank, magic first

Run `suggest` Phases 0–3 in this session: one pass, three lenses, about ten
cited signals, cards, ranked. At equal effort prefer **magic** (a user would
feel it) over friction over risk — unless the risk is a path that loses data,
which always wins.

## Phase 2 — Dedupe, top down

`slug` = the title in kebab-case (lowercase, non-alphanumerics → `-`, repeats
collapsed, trimmed; the same rule `corgi suggest-history` uses). For each card
from the top:

- `corgi suggest-history check --slug <slug> --json` → skip on
  `filed | dismissed | proposed | rate-limit`.
- Title match against the board's open cards and the open pull requests →
  skip as `open`.
- Memory says rejected → skip as `dismissed`.

The first survivor wins. None → `record --slug <top> --status skipped`,
headline "nothing new: N ideas already on the board or proposed", stop.

## Phase 3 — Put it on the board

```
corgi agent task add "<title>" --body - <<'EOF'
why now: <one line>
evidence: <file:line · README promise · gap>
change: <three lines, by service>
done when: <the check>
effort: S/M/L · risk: <one line>
EOF
corgi suggest-history record --slug <slug> --status proposed --title "<title>" --lens <magic|friction|risk>
```

- A person is present and wants the tracker → the `tracker` write gate
  (`../_shared/tracker-mcp.md`, MCP preflight; draft, label `corgi-suggest`,
  no assignee, the task body as the description) → `record --status filed
  --ticket <KEY>`.
- `autoFileDrafts` on → the same, unattended, one per run; not connected →
  the task stands, record `proposed`.
- "Not this" → `record --status dismissed`; the cooldown keeps it away.

## Phase 4 — Report

The first line is the headline the inbox row shows: `<title> — <why, in ten
words>`. Then the task ref, the evidence, and what was skipped and why. No
shortlist, no essay.

## Scenarios

- **First time** → add the bot and the routine (above); a trial is `corgi
  agent routine run suggest` right away.
- **No daemon or the workspace is not watched** → routines cannot run: `corgi
  agent watch enable --workspace <id>`, then `routine add suggest`, or run by
  hand.
- **The bot is missing** → `routine add suggest --bot proactive` refuses and
  prints the add line; without `--bot` the routine runs plain (same prompt,
  no soul).
- **Cap hit or everything deduped** → a clean no-op with a `skipped` entry, so
  `suggest-history list` shows it ran and why nothing landed.
- **Another scheduler** → `references/schedule-config.md`; the daemon routine
  stays the default.
