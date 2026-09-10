---
name: budget
description: Use when the user asks "how much budget do I have", "can I start another task", "when does the limit reset". Reads `corgi agent usage --json`, answers in numbers, suggests `corgi agent carry` when a limit is near.
---

# Budget

Read `../_shared/conventions.md` first.

Answer "can I start another task" with numbers, not a feeling.

## Read

`corgi agent usage --json`. Per entry in `accounts[]`:

- `profile`, `sessions` (running under it now)
- `limits.fiveHour.percent` and `.resetsAt`, `limits.sevenDay.percent` and `.resetsAt`,
  `limits.fetchedAt`

Top level: `waitsToday` and `limitedToday` (`count`, `medianS`, `longestS`, `totalS`).

Stale numbers: `fetchedAt` older than an hour → say so. Claude Code refreshes the limits
when a session runs `/usage`, so ask the user to run it once under the quiet account,
then read again. `resetsAt` in the past → that window has reset; treat it as 0%.

## Judge the pace

Per account, per window:

- **room** = 100 - percent
- **time left** = `resetsAt` - now, printed as "in 2h 10m" and as a clock time
- **pace** = percent used per hour elapsed in the window (5h window: 5h - time left;
  7d window: 7d - time left). Project it to `resetsAt`: under 90% is safe, 90 to 100%
  is tight, over 100% is over.

The window that binds is the tighter of the two. The account to start work on is the
one with the most room in its binding window.

## Print

```
account    5h                          7d                      pace
work       57% (lifts 19:50, in 1h05)  41% (lifts Mon 06:00)   safe
personal   95% (lifts 15:00, in 0h20)  58% (lifts Sun 00:00)   over
waits today: 0 · limited today: 0
verdict: start on work; personal lifts in 20 minutes, let it idle
```

One verdict line: which window binds, when it lifts, whether the current pace is safe.

## When a limit is near (90% or more)

- Another account has room → suggest `corgi agent carry <session> --profile <name>`
  for the session that will hit the wall; it moves the conversation, not just the
  account. Take the session name from `corgi agent sessions`.
- No other account → say how long until the reset and suggest queueing the work
  (`/corgi-queue` later) rather than starting a long build now.
- Never carry, stop, or dismiss a session yourself; print the command.
