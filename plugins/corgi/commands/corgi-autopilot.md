---
description: "Run one iteration of the supervised autopilot loop: build-ready tickets into draft PRs, with one spec gate per batch."
---

Run the corgi **autopilot** skill for `$ARGUMENTS` (one supervised iteration).

- `$ARGUMENTS` = the **scope** passed straight to the `tracker` pickup (Job 4): empty
  → the **`agent` queue**; or `in ready` / `from backlog` / `most impactful` / `bugs`
  / explicit `ABC-1 ABC-2` keys. Same scopes as `/corgi-queue`.
- Optional flags the skill reads: `--max-batch N` (default 3), `--review-after`
  (chain the `review` skill on the new draft PRs - off by default), `--until <when>`
  / `--max-iterations N` (stop conditions).
- **One spec sign-off gate per batch is preserved** (from `stories`). **Draft PRs
  only - never merges.** Honors `corgi autopilot pause`/`stop` at the iteration
  boundary; writes a heartbeat each iteration.
- **It does not schedule itself.** To keep draining:
  - `/loop 1h /corgi-autopilot` - recurring while the session is open.
  - `/schedule` a routine running `/corgi-autopilot` on a cron for unattended draining
    (unattended iterations stop at the spec gate and wait).
- Kill switch / status: `corgi autopilot stop` · `pause` · `resume` · `status --json`.

Follow the `autopilot` skill (load it with the Skill tool: `corgi:autopilot`).
