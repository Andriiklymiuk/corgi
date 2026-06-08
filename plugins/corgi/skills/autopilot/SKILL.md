---
name: autopilot
description: Use when the user wants corgi to run a SUPERVISED background loop that continuously drains the build-ready ticket queue into draft PRs — "autopilot", "keep shipping the agent queue", "run the loop", "drain the backlog overnight", "babysit the queue and open PRs". Each batch still passes ONE spec sign-off gate (supervised, not zero-touch) and opens DRAFT PRs only — never merges. It composes the existing tracker pickup (/corgi-queue), stories, and review skills and is scheduled by the host's /loop or /schedule — it does not reinvent scheduling. Has a kill switch (corgi autopilot stop/pause) and a heartbeat. NOT for a single batch you'd run once with /corgi-queue, NOT for auto-merging anything, and NOT for authoring/running corgi-compose itself.
---

# Corgi autopilot

A supervised loop: one iteration = one `/corgi-queue` pickup → `stories` (spec gate → branch per service → tests → review → draft PR) → emit a heartbeat → back off → repeat on the host scheduler. Inherits corgi's one-spec-gate-per-batch guarantee and draft-PR-only rule; adds visible progress and a clean kill switch. **Orchestration only — it calls the queue/stories/review skills, never re-implements them.**

## What it is NOT

- **NOT zero-touch.** Each batch passes `stories`' one spec sign-off gate. Autopilot stages and waits; it never auto-approves a spec.
- **NOT a merge bot.** Draft PRs/MRs only — never `gh pr ready`, never `glab mr update --ready`, never merge, never force-push.
- **NOT a scheduler.** It runs one iteration per invocation; the host's `/loop` / `/schedule` repeats it. No daemon, no inner busy-loop.
- **NOT a re-implementation.** Pickup logic lives in `tracker` (Job 4); build + gate + draft PR live in `stories`; PR review lives in `review`. This skill calls them.

## Guardrails (non-negotiable)

- **One spec gate per batch, preserved.** Bounded by `maxBatch` (default 3) so one gate covers a sane set.
- **Draft only. Never auto-merge.**
- **Honor the kill switch at the boundary, never mid-build.** Read `corgi autopilot status --json` at the start of every iteration.
- **No new auto-writes.** Creating tracker issues, pushing, and the review post all keep their existing confirmation gates.
- **Never touch `manualRun` services** (inherited from `stories`/`tracker`).
- **Degrade, don't crash.** No tracker MCP / no compose → report and stop the loop, don't guess.

## Phase 0 — Preflight (mode + workspace + tracker)

1. **Mode check first.** `corgi autopilot status --json`. Branch on `mode`:
   - `mode: uninitialized` (no state file yet — a genuine **first run**, NOT a stop) → `corgi autopilot resume --json` to initialize `running`, then continue.
   - `mode: running` → continue.
   - `mode: stopped` (kill switch) or `mode: paused` → emit a heartbeat noting why and **end the iteration** (no pickup).
   The `uninitialized` sentinel is what tells a first run apart from an explicit `stop` — both used to look like `stopped`; don't mistake the first run for the kill switch.
2. **Workspace + tracker** — exactly as `tracker` Phase 0: `ls corgi-compose.yml *.corgi-compose.yml`; detect Linear/Jira MCP. No compose or no tracker MCP → `corgi autopilot pause` with a note, surface what to connect, stop. Don't guess a layout.

## Phase 1 — Resolve a batch (delegate to tracker pickup)

- **Run `tracker` Job 4 (pickup)** with the configured **scope** (from `$ARGUMENTS`; default the `agent` queue). Do **not** re-implement the scope/drift logic — call the skill. It already filters not-In-Progress/not-Done/not-blocked and drift-skips merged/in-flight tickets.
- **Cap to `maxBatch`** (default 3): take the top N ready picks for this iteration; the rest wait for the next one.
- **Empty / all-drift** → record `idle`, jump to Phase 3 (heartbeat) then Phase 4 (backoff). Not an error.

## Phase 2 — Spec gate + build (delegate to stories)

- Hand the picked **keys** to `stories`. Its **Phase 2 one spec sign-off** runs for the whole batch — **do not bypass, do not auto-approve.**
- **Unattended run** (no human present, e.g. under `/schedule`): stage the batch, stop at the gate, record `awaiting_spec_signoff` + the staged keys via `corgi autopilot heartbeat`, and end the iteration. Resume builds them when a human approves.
- **Attended run:** human approves the spec(s) → `stories` branches per service, tests, self-reviews (its Phase 3.5), opens **draft** PRs/MRs, moves tickets In-Progress → Code Review. That in-progress move is what de-dupes the next iteration — rely on it.
- **Optional `review` chain** (config `reviewAfterBuild`, default off): after draft PRs open, run `review` on them. Keep off by default — `review` posts outward-facing comments behind its own gate.
- **Build failure** (Stop rule in `stories`, dirty-tree overlap, gate failed twice) → record `error` + reason, `corgi autopilot pause`, surface to the human. Don't retry-spam.

## Phase 3 — Heartbeat + progress

After each iteration (built, idle, awaiting-gate, or error), call:
`corgi autopilot heartbeat --json --built <N> --skipped <M> --awaiting <K> --phase <built|idle|awaiting_spec_signoff|error> --note "<short>"`
Then print one human line: `autopilot · iter <i> · built N · skipped M · awaiting K · <note>` so it shows up in the harness/mission-control transcript.

## Phase 4 — Backoff, stop conditions, repeat

- **The scheduler interval IS the backoff.** Never sleep/poll inside one invocation — end and let `/loop`/`/schedule` re-invoke.
- **Stop conditions:** `mode: stopped` (kill switch); a configured `maxIterations`/`until` reached; a hard error that paused the loop. On any, end without scheduling more (the human or the harness owns the schedule lifecycle).
- **Idle/all-drift** → just end this iteration; the next scheduled run retries. Optionally widen scope only if the user configured a fallback; default stays on `agent`.

## Scheduling — /loop and /schedule

Autopilot does not schedule itself. Use the host harness:
- **Recurring (foreground-ish):** `/loop 1h /corgi-autopilot` — re-runs one iteration every hour while the session is open.
- **Cron (remote routine):** `/schedule` a routine that runs `/corgi-autopilot` on a cron (e.g. nightly) so it drains the queue unattended; unattended iterations stop at the spec gate (Phase 2) and wait.
Each scheduled run is a fresh agent session — the durable `corgi autopilot` state file (mode + heartbeat) is how iterations coordinate.

## Kill switch / pause / state

- `corgi autopilot stop` — kill switch; next iteration sees `stopped` and no-ops. (Cancelling the `/loop`/`/schedule` job also works; state stays consistent.)
- `corgi autopilot pause` / `resume` — toggle without losing config.
- `corgi autopilot status [--json]` — `mode` (`uninitialized` first run · `running` · `paused` · `stopped`), `lastHeartbeat` + age, last iteration summary. Heartbeat age > interval ⇒ a stalled loop a supervisor can flag.
State lives in `corgi_services/.autopilot.json` (gitignored, per project). No daemon.
