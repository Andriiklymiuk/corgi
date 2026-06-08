---
description: Run one iteration of the SUPERVISED autopilot loop — drain the build-ready ticket queue into DRAFT PRs, with one spec sign-off gate per batch, a heartbeat, and a kill switch. Pass a scope in plain words (nothing = the `agent` queue; "in ready", "from backlog", "most impactful", "bugs", or explicit ABC-123 keys). It delegates pickup to the tracker skill and building to stories — never auto-merges. Schedule it with /loop or /schedule to keep draining; stop it with `corgi autopilot stop`.
---

Run the corgi **autopilot** skill for `$ARGUMENTS` (one supervised iteration).

- `$ARGUMENTS` = the **scope** passed straight to the `tracker` pickup (Job 4): empty
  → the **`agent` queue**; or `in ready` / `from backlog` / `most impactful` / `bugs`
  / explicit `ABC-1 ABC-2` keys. Same scopes as `/corgi-queue`.
- Optional flags the skill reads: `--max-batch N` (default 3), `--review-after`
  (chain the `review` skill on the new draft PRs — off by default), `--until <when>`
  / `--max-iterations N` (stop conditions).
- **One spec sign-off gate per batch is preserved** (from `stories`). **Draft PRs
  only — never merges.** Honors `corgi autopilot pause`/`stop` at the iteration
  boundary; writes a heartbeat each iteration.
- **It does not schedule itself.** To keep draining:
  - `/loop 1h /corgi-autopilot` — recurring while the session is open.
  - `/schedule` a routine running `/corgi-autopilot` on a cron for unattended draining
    (unattended iterations stop at the spec gate and wait).
- Kill switch / status: `corgi autopilot stop` · `pause` · `resume` · `status --json`.

Follow `skills/autopilot/SKILL.md`.
