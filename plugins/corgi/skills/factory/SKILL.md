---
name: factory
description: "Use to decide how much of the work runs without a person, or to climb that ladder: \"software factory\", \"dark factory\", \"how autonomous should this be\", \"run this unattended\", \"what's our next step to more automation\", \"agents that ship without me\". NOT building one ticket (stories), one batch overnight (autopilot) or a single routine (agent)."
---

# Climb the autonomy ladder

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** `corgi agent level` was read, the one next step and its brake are in place, and the
report says what to watch for a week before the step after it.

## Overview

corgi works anywhere from a skill called by hand to agents that merge what passes. It is a ladder, not
a switch. Most repos should not be at the top, and a repo with no tests cannot be. Find the rung, then
take one step.

```bash
corgi agent level        # where this workspace stands, which brakes are on, the next step
corgi agent level --all  # every workspace; each has its own rung
corgi agent level 4 --done-when "go test ./..."   # move this workspace, and only this one
```

Pick the rung per workspace by what a mistake costs there. Docs can sit high while payments stay at 2.

| Level | What the person does | corgi parts |
|---|---|---|
| 1 You drive | starts each session | skills, `corgi run`, `corgi agent claude` |
| 2 It tells you | gets rung, starts the work | `watch enable`, the board, the phone |
| 3 It drafts, you review | reads every diff | `watch --action fix`, `stories`, `review` |
| 4 It works a shift | writes specs and checks, reads PRs | `--isolate`, `--done-when`, `plan`, `routine`, `autopilot` |
| 5 Lights out | writes specs and scenarios; reads only what is held for them | `--merge-unread`, `scenario`, `scorecard` |

At level 5 a pull request merges with nobody reading it only if a run opened it, the checks are green, the run ended
clean, the diff is small and stays away from auth, payments, data, CI, dependencies and native code, its risk card is 2
or below, and that exact commit satisfies the scenarios. Anything else waits for a person, who hears once why. A pull
request a person opened never merges this way.

## Rules for climbing

**Start from a problem, not from the factory.** Untriaged tickets, PRs that wait for a first review, a
flaky suite, bugs nobody reads. Name it, and pick the smallest thing that solves it.

**Every trigger is an accelerator. Fit the brake first.** There are three: checks a machine runs,
a review by something that reads the diff cold, a person. A trigger with no brake makes a slop cannon.

**One trigger at a time, throttled.** One kind of event, a daily clock, `--max-per-day 5`. Read every PR
it opens. Open the throttle when `corgi agent scorecard` has earned it: runs that end green, not runs
that ended.

**Cap every loop.** Retries, review rounds, fix attempts: a few, then a person. An uncapped loop spends the
whole window on one mistake.

**What a person catches becomes a check.** Each comment a reviewer repeats is a lint rule or a scenario not
yet written. That is how the human gets out of the loop without quality falling.

## The factory's slots

| Slot | In corgi |
|---|---|
| Where work arrives | the tracker, `watch`, `task add`, `idea` tickets |
| What starts a run | `watch` events, `routine` clocks, `plan`, another run's result |
| Where a run happens | a worktree (`--isolate`), `agent container`, another laptop (`peers`) |
| A run's memory | the tracker, git, `handoff`, `lesson`. Nothing else survives a run |
| What stops a run | `--done-when`, `scope`, `guard`, `cap`, the breaker, quiet hours |
| What you see | the board, `scorecard`, `cost`, `selfcheck`, `digest` |

Work out what the workspace lacks before adding a trigger. A run must leave everything the next one needs
in those memories, written down, never in a session's head.

## The loop on one ticket

```bash
corgi agent task add "Fix the export" --workspace api --run   # the dashboard has "Start now, without me"
corgi agent task comment TASK-3 "the total is still wrong"    # or the box on its card; a new run reads the thread
```

Level 3 and up. Teammates review on the pull request (`--prs` wakes the run), and need no corgi. Errors from Sentry
and Coralogix land in the inbox to be picked (`--sentry-org`, `/hooks/coralogix`); `--auto-for errors` runs them
without you, and only belongs where the service is quiet.

Tasks move between `peers`: one that has not started goes to a peer after its machine is silent for 10 minutes, and
`task send TASK-3 --to <peer>` moves one by hand. A run in progress stays. `task add --on <peer>` adds one there.

## Scenarios: an exam the builder cannot read

An agent that can read its tests can pass them without doing the work. Keep the proof where it does not look.

```bash
corgi agent scenario add refund --text "A customer who paid yesterday asks for a refund and sees it on their statement."
corgi agent scenario run --dir <worktree> --times 3 --min 90
corgi agent routine add scenarios
```

- Write what a person does and sees, never how the code does it.
- A share over several runs says more than one tick. Compare it over time.
- Never paste a scenario into a ticket, a PR or a prompt. Pass on only the judge's one line.
- It hides by location. A session on the same machine could open the folder, so at level 5 run the build
  and the judge on different accounts.
- Passing says nothing about the code inside. Keep `complexity`, `quality` and `ratchet` running.

## Plans are graphs

`corgi agent plan` turns a goal into tasks that wait for each other; the daemon runs what is ready, in worktrees.

```bash
corgi agent plan "add rate limits to the public API" --retries 1 --slots 2
corgi agent plan graph P-3            # who waits for whom, where each stands; --dot for Graphviz
```

- Use a graph when work has a real order or can run side by side. One change wants `stories`, not a graph.
- A node is one run, with one clear done. State lives on disk, so a crash or a closed lid resumes.
- A failed node retries with the reason, up to the cap, then stops for a person.
- Branching belongs in the tracker: a label or a column change triggers the next run. It stays visible and a
  person can step in. Do not build a workflow language until a real plan cannot be said this way.

## Fakes of what you do not own

Third-party services block a factory: rate limits, costs, failure modes you dare not trigger. Put a fake behind
the same API as a service in `corgi-compose.yml`, built from the public docs and checked against the vendor's
own client. It pays for itself only for a service you hit constantly, and a fake drifts: test it against the
real one on a clock. Start with the one service that blocks the most runs.

## What makes unattended work good

- **Split the work.** Interactive when the intent is unclear (`align`), unattended only when specs, tests or an
  existing app pin it down. Most bad runs started from a sentence.
- **Show a working example.** "Do it like `<file>`" beats a description.
- **Files are the memory.** Plans, handoffs and notes on disk survive compaction and reach the next run.
- **Summaries in layers.** A line, a paragraph, the full text: read the short one and open the long one only when
  needed. Keep files every session loads small.
- **Budget it.** `corgi agent cost` per ticket. Heavy token use is a cost to weigh, not a goal.

## The factory improves itself

```bash
corgi agent routine add retro      # what went wrong in recent sessions, cheapest fix first
corgi agent routine add ratchet    # tighten one check a week, never loosen
corgi agent routine add lessons    # reviews become rules
corgi agent selfcheck              # what keeps failing in unattended runs
```

```
sessions ──▶ retro ──▶ one change ──▶ a person says yes ──▶ measure next week
   ▲                                                              │
   └──────────────────────────────────────────────────────────────┘
```

- **A loop gaming its own number is the main failure.** An agent that can edit the test or the eval it is judged by
  will. The loop proposes; it never changes its own exam.
- **Judge a change by what happens next:** the scorecard, the same mistake's count. A rule nobody can measure is a
  wish. Remove it when it does nothing.
- Keep a person on the approval until the scorecard says otherwise, and keep it longer for the skills themselves.

## Red flags

- Auto-merge on a repo whose tests the agent wrote and nobody reads.
- Two new triggers in one week: you cannot tell which one made the mess.
- More runs and fewer merged, or green runs that get reverted. Step down a rung.
- Sensitive or irreversible work (payments, migrations, auth) at level 5. Keep a person there, at any level.
- "We need a graph engine" before a plan has ever failed for lack of one.

Step down as readily as up. Report the level, what changed, the brake, and what to watch.
