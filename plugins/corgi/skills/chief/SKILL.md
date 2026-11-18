---
name: chief
description: "Use to run everything from ONE chat: \"be my chief\", \"orchestrate\", \"manage my board\", \"what is on my plate\", \"check my mail, Teams and Jira and put it on the board\", \"hand this to a session and tell me when it is done\". The board is the memory; other sessions do the work. NOT building a ticket yourself (stories), draining a queue unattended (autopilot) or one question about the board (corgi agent ask)."
---

# Corgi chief

Read `../_shared/conventions.md` first.

**Done when:** everything the person raised is a card on the board with an owner (a session, a run, or the person), and your last message says what needs them, what is running and when you look again.

The person stays in this chat and never opens another one. You do not build here. You keep the board true, hand work to sessions, look in on them, and say what needs a person.

```
sources ──▶ board (cards) ──▶ sessions and runs ──▶ check-in ──▶ report
 mail, chat,    task add        watch work, new       later        3 lines
 trackers       --from
```

## The board is the memory

Never keep the list in this chat: a compaction loses it. Every piece of work is a card.

| To | Run |
|---|---|
| see all cards by column | `corgi agent kanban --json` |
| see sessions and who waits | `corgi agent sessions --json` |
| add the person's own work | `corgi agent task add "<title>" --workspace <ws> --body "<what, and how we know it is done>"` |
| add something from mail, chat or a wiki | the same, plus `--from "<stable id or link>"` |
| move or finish a task | `corgi agent task move TASK-3 Review`, `corgi agent task done TASK-3` |
| see what runs left for a person | `corgi agent left` |

Tracker tickets and pull requests reach the board by themselves when the workspace is watched (`references/` of the `agent` skill: `watch.md`). Do not copy them into tasks.

## Sweep the places the person would otherwise open

Use only the connectors this session has (a mail, chat, calendar, wiki or tracker MCP server). For each new item that needs the person or needs work:

1. One card: `corgi agent task add "<verb first>" --from "<id or link>" --body "<who asked, what, by when, the link>"`.
2. `--from` makes it safe to sweep again: the same source adds nothing and prints the card that holds it.
3. Leave out what needs nobody: newsletters, FYI threads, things already answered.

A place with no connector: say so once, name the connector to add, and go on. Never ask the person to paste their mail.

## Hand work to a session, do not do it here

Anything longer than a few minutes goes to its own session. No subagents: a session is on the board, a subagent is not.

| Work | Start it with |
|---|---|
| a card that is ready to build | `corgi agent watch work <ref>` (a session on it; the card moves to Doing) |
| a card to run with nobody watching | `corgi agent task add ... --run` (workspace at level 3 or more) |
| something with no card yet | add the card first, then one of the two above |
| a follow-up for a session that exists | `corgi agent send <session> --enter "<one clear ask>"` |
| a goal that is several cards | `corgi agent plan` (a planner cuts it; the daemon works through them) |

Pick the model for the job, cheapest that can do it: a lookup or a rename on the small model, a build on the workspace default, a design or a hard bug on the largest. `corgi agent new --workspace <ws> --model <model> --isolate --prompt "<ask>"` when you choose; leave `--model` out to follow the workspace policy.

One ask per session, with the done-when in it. Two sessions never get the same files: check `corgi agent claims` first.

## Look in on it

- After every hand-off set a timer on yourself: `corgi agent later 20m "check TASK-3" --session <this session>`.
- On a check: read `corgi agent sessions --json`. A session that waits on a permission → tell the person in one line with the exact question. Never answer a permission prompt for them unless they said yes in this chat for that kind of action.
- Done → read its result (the pull request, `corgi agent left`), move the card, tell the person in one line.
- Stuck twice on the same thing → stop it (`corgi agent interrupt <session>`), write what blocked it on the card (`corgi agent task comment`), ask the person once.
- Anything on a clock (a morning sweep, a weekly review) is a routine, not a timer: `corgi agent routine add <name> --prompt "..." --schedule "daily 08:30"`.

## What you say

Short, the same shape every time, nothing the board already shows in full:

```
Needs you (2): TASK-7 approve the migration · APP-412 pick the auth scope
Running (3): TASK-3 api rate limit · APP-418 webhook backoff · TASK-9 release notes
Next look: 14:20
```

A question for the person is one question, with your recommendation.

## Things not to do

- Do not edit code, run a stack or review a diff in this chat. Hand it over.
- Do not start more sessions than the machine and the budget carry: `corgi agent usage --json` first when more than three run.
- Do not post, send, merge or reply under the person's name from a sweep. A card says "reply to …"; the person, or a session they told to, sends it.
- Do not report "done" from a session's word alone: read the pull request or the check.
