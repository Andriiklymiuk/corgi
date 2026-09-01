---
description: Set up or manage phone-startable Claude Code sessions on this machine — "make this repo phone-startable", "set up remote control", "pair my phone", "start a session from my phone", "give the launcher a permanent URL", "run this workspace under my work account", "stop the remote setup". Works in a corgi stack OR any git repository; no args = set up the current directory.
---

Run the agent-mode remote setup for the request in `$ARGUMENTS`.

- `$ARGUMENTS` = plain words: what to set up or change (pair a new phone, a
  permanent URL via a named tunnel, a per-workspace Claude account/profile,
  stop everything). Empty → make the current directory phone-startable.
- Works in a corgi stack **or any git repository**. Neither → tell the user to
  open the project they want phone-startable.

Follow the `agent` skill (`plugins/corgi/skills/agent/SKILL.md`) — the
"Setting it up from a session on the laptop" section is the core flow:

1. `corgi agent up` (detached; `--json` for structured output). Print the QR /
   pairing URL for the user to scan. Port already held by a healthy corgi MCP →
   it reports as already up; a NEW pairing window needs `corgi agent up --fresh`.
2. Reboot survival: offer `corgi agent up --at-login` (daemon + endpoint + tunnel at login; `corgi agent install` is the daemon alone).
3. Permanent URL: offer the named tunnel (`--tunnel-name <name> --tunnel-hostname <host>`,
   one-time `cloudflared tunnel create` + `route dns` — docs/agent.md carries the
   steps). Same origin every restart, so the phone never re-pairs.
4. Separate Claude accounts: `corgi agent init --config-dir <dir>` per
   workspace, or `corgi agent profile add <name> --config-dir <dir>` to pick at
   start time. Per-workspace open target (Claude app vs browser vs Chrome) is
   set on the phone launcher itself, not in config.
5. Tear-down: `corgi agent down` (daemon + MCP + tunnel).

Guardrails from the skill hold: never enable `--dangerously-skip-permissions`
on your own — it is the user's explicit, trusted-config choice; a `sensitive`
workspace is never exposed through a public tunnel; registration is per
directory the user deliberately chose.
