# corgi-band

A Claude Code mod for people who run corgi.

- **Band above the prompt.** Sessions elsewhere that stopped on a question or a permission, each with a **Focus** button (`corgi agent focus`). This branch, when it needs something: it conflicts with its base, its checks fail, changes are requested, the tests failed - each with **Fix it**, which asks this session to do it - or it is ready to merge, merged, or the context is past 70%. Anything this turn said to other people that failed (a Slack post, a ticket move, a PR verb, a push), with the error: a revoked Slack token shows here, not in a report you have to read. What went through says nothing. **Hide** puts it away until something changes.
- **Toast** the moment one of those fails.
- **Status line** `corgi · 2 waiting on you · 5h 91%` - only when something waits or the 5-hour window is past 80%.
- **`/corgi`** opens a pane: every live session (waiting first, with Focus), the tickets that want a person (Blocked, Review, Inbox, with **Work on it** → `corgi agent watch work`), the stack of this folder (`corgi status`), and the budget while its window is current.

It reads `corgi --json agent sessions`, `corgi --json agent kanban` and `corgi --json status` every 20 seconds; without corgi on PATH it stays silent.

## Install

```
/plugin install corgi-band --marketplace Andriiklymiuk/corgi
```

Answer `y` to add the marketplace, then pick the user scope.
