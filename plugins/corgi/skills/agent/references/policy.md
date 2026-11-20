# Scope, drift, models, routines, hardening

**Scope** is the contract a ticket's change stays inside, written after the
spec is agreed (the stories skill does it): `corgi agent scope set ABC-123
--path "api/limits/**" --lines 400 --tests 2 --done "…"`. Two hooks
`corgi agent track enable` installs hold the session to it: a write outside
the paths is refused with the way to widen (`corgi agent scope add ABC-123
--path …` - do it when the change needs it, and say why in the PR), and a
diff over budget is reported once when the turn ends (trim it, or raise the
budget on the record). Both are silent on a branch with no scope.

**Drift** is what the daemon concludes from the numbers on a live session:
context ≥ 85 %, the same tool failing three times running, a diff twice the
scope's budget (or past 800 lines with none), files outside the scope. The
key says `DRIFT` with the first reason; the phone offers **Fresh** - a clean
restart from a handoff under the same account (`corgi agent carry <session>
--fresh`). The other ways out are `/compact`, `/rewind`, or splitting the
branch by plan task.

**Models** are a policy per phase in the user config (`models:` under a
workspace or `defaults:`): `plan` and `review` on the largest model, `execute`
and `triage` on the default or the small one, `escalate` after a failed run,
`kinds:` per event. Write the alias (`opus`, `sonnet`, `haiku`), not a dated
id: an alias follows the newest model of its tier, an id goes stale.
`corgi agent claude --model auto` starts on the policy's `auto` (opusplan by
default); the unattended runner picks per kind and steps up after a failure.

**Routines** are runs on a clock through the same runner: `corgi agent
routine add digest` (daily 08:30), `babysit-pr` (every 2h), `deps`,
`release-notes`, `flaky`, `doc-drift`, `suggest`, `lessons` (reviews of your PRs → rules every session reads), or `--prompt "…" --schedule
"daily 03:00"`. Each report is one inbox row with the log behind it; `routine
run <name>` runs one now. Caps, quiet hours and budget apply. `--bot <name>`
runs a routine as that bot (soul, model, account, filed under its name):
`bot add proactive --template proactive` + `routine add suggest --bot
proactive` is the Proactive bot - one thing worth building next, weekly, as
a task on the board (the `suggest-proactive` skill).

**Hardening**: `corgi agent harden` writes deny rules for secrets and
destruction plus a hook that refuses to write a credential into a file, into
the workspace's `.claude/settings.local.json`; `corgi agent doctor
--security` says what is still loose. `corgi surface` is the changed public
surface of the stack's diff (first section of every PR body, first thing a
review reads); `corgi docs check` the docs that name what changed and the
CLAUDE.md pointers that no longer land.

**Own tickets**: `corgi agent task add "Meta SDK on iOS" --workspace app
--body "…"` is a ticket the person writes themselves, kept on this machine
and shown on the same board as the tracker's - Todo, Doing, Review, Done -
and never sent to a tracker. `corgi agent watch work TASK-3` (or the phone's
**Work on it**) starts a session on it with the body as the prompt; the
session runs `corgi agent task move TASK-3 Review` when its draft PR is up
and `task done TASK-3` when nothing is left. `corgi agent refresh` reloads
everything now - sessions rescanned, every tracker polled, the board
published - when a person says the board looks stale.

**A task's links, owner and order**: `corgi agent task link TASK-3 <url>…`
ties it to a Jira or Linear ticket or a GitHub or GitLab pull request (`--rm`
takes one off). `corgi agent task claim TASK-3 [--as name]` says who holds it
and `release` gives it back: the daemon does not start a task another name
holds. `corgi agent task set TASK-3 --priority high --due 2026-12-01
--blocked-by TASK-2` orders the queue: highest priority first, and a task that
waits for another stays in Blocked until that one is finished.

**Triage, cycles, views**: a task added with `--triage` (and every task an agent or a guest adds) waits
in `corgi agent task triage` until `task accept TASK-7`; `task snooze TASK-7 --for 3d` puts it to sleep.
`corgi agent cycle start --days 7`, `cycle add TASK-3 TASK-4`, `cycle` (where it stands) and `cycle close`
(done and slipped) box a week of work. `corgi agent kanban --view needs-me|unreviewed|red|stale|overdue|
blocked|triage|mine|cycle` filters the board; `kanban views save <name> --owner anna --priority urgent`
keeps a filter. Over MCP an agent claims before it starts: `corgi_task_claim`, then `corgi_task_release`
when it stops without finishing.

**Show the board to a teammate**: `corgi agent kanban share --workspace api
--for 7d --label "for Anna"` prints a read-only link once. It shows titles,
columns, owners and links, never a description or a comment. `--list` shows the
open links, `--off <id>` closes one. `--write` lets the guest add a task (into triage), comment and take
a task; a guest's words never start a run.

`corgi agent kanban [--workspace X] [--json]` is one card per ticket in a
column corgi works out - Inbox, Ready (deferred, or a handoff waiting),
Running (a run or a session on the branch), Blocked, Review (a PR is open),
Done - with the session, the branch, the PRs, the handoff's next step and
what the ticket has cost (runs keep claude's own receipt; sessions on the
branch add their transcripts). The phone's Board tab draws the same. Moving a
card is `watch move`; nobody drags one into Running.

What a card also says (2.40): `now` is the live line of a Running card - the
session's tool and file, or "a run is on it", with the model and the agent;
`cost.models` and `cost.agents` are who worked on it, once each (claude,
codex, and each model by its own id); `feature` is the plan (`P-3`) or the
parent ticket the card is one piece of, `after` the cards it waits for. The
`features` list in `--json` groups them with a state and `waves`: the cards
of one wave run side by side, the waves in a row (`TASK-1 → TASK-2 + TASK-3`).
A plan's tasks and a tracker's subtasks both show this way; a feature is
`running` while one card runs, `review` when only reviews are left, `done`
when every card is.

**Done is history, not a column to read.** Every surface hides it by default
and shows it on a switch; a Done card leaves after a day. Once an hour the
daemon runs `corgi agent kanban reconcile`: a run that answered a week ago
with nothing since, a review whose pull request closed, a ticket nothing
touched for two weeks, a Done card - all leave the board. Never a card with a
session, a run, an open pull request or a fresh pick, whatever its age; each
card leaves once (one line in `agent.log`, nothing rings); anything new on
the ticket brings its card back. `--dry-run` says what would leave,
`--back ABC-12` puts one back by hand. `corgi agent history [--days 30]
[--workspace X] [--json]` is what already happened, newest first: every run
with what it said, its cost, model, agent and how long it took, every merge,
every finished task, every card that left and why. When a person asks "what
did corgi do this week" or "where did that ticket go", read this, not the
board. The phone has it under + › History; `/launch/history` serves it.

**Who reviews, and as whom.** A review request on a watched workspace is
answered by the watch, as an unattended run under the person's own name, and
the comment reads as theirs. A **bot** acts as itself - its own name on the
comment, its own soul and model. A bot sheet that says "last seen 50d ago"
with every trigger off is a bot with no job, not a sign that reviews
stopped: check `corgi agent watch` and `corgi agent history` before saying
anything is broken, and only suggest a reviewer bot when the person wants the
agent's name on the review instead of theirs.
