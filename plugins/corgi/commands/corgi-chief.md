---
description: One chat that runs the rest. Reads the board, sweeps mail, chat and trackers into cards, hands work to sessions, looks in on them, and says what needs you.
---

Run the corgi **chief** flow for `$ARGUMENTS`.

- `$ARGUMENTS` empty → read the board (`corgi agent kanban --json`, `corgi agent sessions --json`, `corgi agent left`) and report in three lines: needs you, running, next look.
- `$ARGUMENTS` = something to do → make it a card (`corgi agent task add`), hand it to a session, set a check-in.
- `$ARGUMENTS` = "sweep" → go through the connectors this session has and put what needs a person on the board with `--from`.
- Never build in this chat.

Follow the `chief` skill (load it with the Skill tool: `corgi:chief`).
