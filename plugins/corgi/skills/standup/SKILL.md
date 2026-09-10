---
name: standup
description: Use when the user asks "what did I do today", "what did I do yesterday", "write my standup", "what landed this week". Runs `corgi agent standup`, reads it back, offers to post. NOT for "what's blocked" (tracker).
---

# Standup

Read `../_shared/conventions.md` first.

`corgi agent standup` already knows what every account's Claude Code was asked and
what landed in git under each registered workspace. This skill runs it, reads it, and
turns it into words a person would say.

## Run

- Default window: `corgi agent standup` (last 24h). "Yesterday" on a Monday, "the
  weekend", "this week" → `--since 48h`, `--since 72h`, `--since 168h`.
- The user asked for prose ("write it", "summarize", "three sentences") → add
  `--write`. That hands the raw list to `claude -p` and prints a few sentences; without
  it nothing leaves the machine, so say which mode you used.
- Empty output → tracking may be off: point at `corgi agent track enable`, then fall
  back to `git log --since=<window> --oneline` per repo of the current workspace.

## Read it back

Group by workspace as the command does. Commits are facts (keep the subject, drop the
hash); prompts are headlines (what was asked, not the wording). Turn both into:

```
<workspace>
  done     <what landed, one line per PR or commit cluster>
  doing    <what the last prompts were about, not yet committed>
  next     <what the open branches imply>
  blocked  <only when sessions or brief show a wait or a limit>
```

Three to six lines per workspace; only the cwd workspace for a team standup, all of
them for "everything".

Cross-check the tracker only when ticket keys appear in the commits: pull those keys'
status through the `tracker` skill and mark drift ("ticket still In Progress, PR
merged").

## Offer to post

Ask once: "post this?" with the destinations that exist: the tracker (a comment on
the cycle or the standup issue, through the `tracker` skill's write gate), the chat
the daemon already notifies (`corgi agent digest --send` sends today's digest there),
or a paste-ready block for Slack. Never post without the answer, never add
attribution, never make the facts sound bigger than the diff.

## Do not

- Redo the git archaeology yourself when the command returned data.
- Read prompt history beyond what the command prints.
