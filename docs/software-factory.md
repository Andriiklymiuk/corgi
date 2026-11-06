# From a skill to a software factory

corgi is a ladder. You can call one skill by hand, or let agents take tickets to merged code while you write specs.
Stand wherever suits the repo. `corgi agent level` says where you are and what comes next.

```bash
corgi agent level                                  # this workspace
corgi agent level --all                            # every workspace
corgi agent level 3                                # move this workspace, and only this one
corgi agent level 4 --done-when "go test ./..."
corgi agent level 1 --workspace payments
corgi agent restart                                # applies it
```

Each workspace has its own rung, so docs can run high while payments stays low. Going up refuses a rung whose
brake is missing. Going down turns the accelerators off and keeps your checks and caps. The level is not a separate
setting: it is what the watch settings add up to, so `watch enable` and `level` always agree.

## The ladder

| Level | You | Agents | Turn it on |
|---|---|---|---|
| 1 You drive | start each session | do what you ask | `/corgi-review`, `/corgi-run`, any skill |
| 2 It tells you | get rung, start the work | watch trackers and PRs | `corgi agent level 2` |
| 3 It drafts, you review | read every diff | take tickets to draft PRs | `corgi agent level 3` |
| 4 It works a shift | write specs and checks, read PRs | run in worktrees, on a clock, in plans | `corgi agent level 4 --done-when "..."`, `corgi agent plan`, `corgi agent routine` |
| 5 Lights out | write specs and scenarios | merge what passes | `corgi agent scenario add`, then `corgi agent level 5` |

Level 5 is not the goal for every repo. It needs tests you trust and scenarios it cannot read, and it is the wrong
place for payments, auth and migrations.

## Three brakes

Every automatic trigger is an accelerator. Fit a brake first.

- **Checks:** `--done-when`, `corgi agent guard`, `corgi agent scope`, CI.
- **Review by something that reads the diff cold:** `/corgi-review`, the second reviewer in `stories`.
- **A person:** `--plan-review`, a human merge, a cap on runs (`--max-per-day`, `--limit-ceiling`).

Add one trigger at a time, throttled. Open the throttle when `corgi agent scorecard` shows runs ending green.

## Scenarios

An agent that can read its tests can pass them without doing the work. A scenario is a story kept outside the
repository. A separate read-only run tries the behaviour and says if a person would be satisfied.

```bash
corgi agent scenario add refund --text "A customer who paid yesterday asks for a refund and sees it on their statement."
corgi agent scenario run --dir <worktree> --times 3 --min 90
corgi agent routine add scenarios
```

Results are a share over runs, kept per workspace. The folder is out of the way, not locked: a session on the same
machine could open it, so run the builder and the judge on separate accounts if that matters.

## Plans are graphs

```bash
corgi agent plan "add rate limits to the public API" --retries 1 --slots 2
corgi agent plan graph P-3 --dot | dot -Tpng -o plan.png
```

Tasks wait for the ones they depend on and run side by side in worktrees. State lives on disk, so a restart resumes.
A failed task starts again with the reason, up to three times, then waits for a person.

## It improves itself

| Routine | What it does |
|---|---|
| `retro` | reads recent sessions and proposes the cheapest fix for what went wrong |
| `ratchet` | tightens one automated check a week, never loosens one |
| `lessons` | turns reviews of your PRs into rules every session reads |
| `scenarios` | judges main against the stories kept outside the repository |

The loop proposes and a person approves. Nothing may change its own test or eval to pass, and a change is judged by
what the scorecard shows the week after.

## Read more

- [The five levels](https://www.danshapiro.com/blog/2026/01/the-five-levels-from-spicy-autocomplete-to-the-software-factory/)
- [How one team runs a software factory](https://simonwillison.net/2026/Feb/7/software-factory/) and [its techniques](https://factory.strongdm.ai/techniques)
- [Symphony: a tracker as the control plane](https://openai.com/index/open-source-codex-orchestration-symphony/)
