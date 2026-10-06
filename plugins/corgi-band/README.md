# corgi-band

A Claude Code mod for people who run corgi.

- **Band above the prompt.** Sessions elsewhere that stopped on a question or a permission, each with a **Focus** button (`corgi agent focus`). Under them, what this turn said to other people - Slack posts, ticket moves, comments, PR verbs, pushes - each with a tick, or a cross and the error (a revoked Slack token shows here, not in a report you have to read). **Hide** puts it away until something changes.
- **Toast** the moment one of those fails.
- **Status line** `corgi · 2 waiting on you · 5h 91%` - only when something waits or the 5-hour window is past 80%.
- **`/corgi`** opens a pane: every live session (waiting first, with Focus), the tickets that want a person (Blocked, Review, Inbox, with **Work on it** → `corgi agent watch work`), the stack of this folder (`corgi status`), and the budget while its window is current.

It reads `corgi --json agent sessions`, `corgi --json agent kanban` and `corgi --json status` every 20 seconds; without corgi on PATH it stays silent.

## Install

```
/plugin install corgi-band --marketplace Andriiklymiuk/corgi
```

Answer `y` to add the marketplace, then pick the user scope.
