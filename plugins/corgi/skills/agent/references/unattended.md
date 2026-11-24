# What a workspace does on its own

Switches per workspace, each off until flipped, each read live - from the
CLI or the phone's repo sheet, no restart:

- `corgi agent watch enable --auto-allow reads` - the daemon answers a Read,
  Grep, Glob or web-search prompt itself in an iTerm2 session; never Bash,
  never a write. The row counts them (`autoAllowed`).
- `--done-when "go test ./...,pnpm lint"` - when a session stops with
  changes on its branch, the commands run in its directory; a red one is
  typed back as the next message (*Not done yet: … fix it, run it again,
  stop when green*), the row shows `gate` and *tests ✗*; three reds in a row
  ring a person instead. **Done means the checks say so.**
- `--compact-at 85` - `/compact` sent to a full session the next time it
  stops, once per episode.
- `--rebase` - a stopped session's clean branch is rebased onto main where
  it sits when main moved; with `--hand-over`, one that would conflict is
  told which files. The sweep measures `behind` (`main moved 12 · conflicts
  in api.go`) on every live branch.
- `--lessons` - checks that stayed red and failed bot runs are written one
  line each to `<agentDir>/lessons/<workspace>.md`; a run that answers a review
  adds the rule behind each finding (`corgi agent lesson add "<repo>: <rule>"`);
  the SessionStart hook hands you the newest twenty. **Follow them.**
- `--night-shift`: the quiet hours hold the phone, not the work - fixes and
  routines run through the night, one summary in the morning. `--quiet` must
  be set for it to mean anything. The lid guard (`corgi agent lid on`) is
  machine-wide, not a watch flag: a wake or an open lid by day rings once.
  It is for a trip - `corgi agent away` turns it on, `lid off` when back.
- `--merge-unread` (level 5): low-risk pull requests a run opened merge with nobody
  reading them; the rest wait for a person. See [docs/software-factory.md](https://github.com/Andriiklymiuk/corgi/blob/main/docs/software-factory.md).
- `--hand-over`, `--auto-merge`: feedback typed into the session on
  the branch; a ready pull request merged.
- `--arm-merge`: after a run fixed review comments or a red build on a pull request of mine, corgi
  turns on the forge's own auto-merge for it. The forge's required approvals and checks still decide.
  A draft, someone else's, and a GitLab merge request with no pipeline running are left alone.
- `--after-merge "<column>"`: once **every** pull request of a run is
  merged (by the daemon or by a person), the ticket moves to that column -
  a name `corgi agent watch board` prints. `--after-merge-subtasks Done`
  sends a subtask to a column of its own. Jira boards mostly; Linear's
  GitHub link usually moves the issue by itself, so leave it off there.
- `--batch 3`: tickets that arrive within 90 s of each other (a planner
  assigning a few at once) share one run - `/corgi:stories A B C` - one
  preflight and one context. Each still counts toward the caps; the notice
  says `fixed A + B + C`; a crash retries them one by one. 1 is off.
- What is a new ticket: one created assigned to the user, **or an old one
  a person hands over** - assigned to the user or moved to a column, by
  someone else (the changelog / issue history says who). The user's own
  moves and an automation's moves are not news. A parent with open subtasks
  of the user's is left to them (`its open subtasks are the work`); a
  subtask's run gets the parent's key and title for context.
- `corgi agent watch sweep --states "<not-started column>"`: the watch
  only sees what changes after it starts, so work already assigned before
  it was enabled never arrives. A sweep reads every open ticket of the
  user's once and hands the ones in that column to the daemon as new
  issues; seen, ran, ignored, blocked and parents-with-subtasks stay put.
  `--dry-run` lists first. Never sweep In Progress / In Review - that is
  the user's own hand work.
- A story run has three hours per ticket (a review 45 min): it builds,
  tests, opens the pull requests and watches CI.
- A run that crashed once (exit code, timeout) gets exactly one more go
  30 min later; a second crash trips the breaker (`watch unblock` clears
  it). `--no-retry` turns the retry off with the rest.
- `--approve`: an unattended review of a pull request someone asked
  the user to review may end in an approval - only when nothing blocks and
  the risk card says `auto-approve: yes`; otherwise findings, no stamp.
  Off by default: an approval carries the user's name. A review the user
  starts at the desk approves a good PR without this flag. `--auto-for all`
  (or `requests`) is what makes review requests run at all; without it
  they only notify, and with `--silent` not even that.
- `--bots`: comments from bot accounts count. Off, a bot is not a
  person waiting - right for a coverage or pipeline bot, wrong for an AI
  reviewer whose findings are meant to be fixed. Ask which account posts
  those before turning it on.
- Comment fixes settle: a fix for a comment on a pull request
  starts a minute after the **last** comment on it - reviewers and review
  bots post several in a row - and one run reads every open thread, so
  three comments are one fix. A comment that lands while a fix is running
  is one more run after it, through the same settle, never a parallel one.
  The user's own comments never count; a merged or closed pull request is
  skipped before any run.

**Several tries at once**: `corgi agent watch work ABC-123 --attempts 3
--models opus,sonnet` opens three sessions in worktrees of their own on
`corgi/ABC-123-N`; `corgi agent attempts ABC-123` compares them (status,
changes, tests, done-when, cost, PR) and `attempts pick ABC-123 2` keeps
one. The phone's "Try 3 ways" does the same.

**Bots** (`corgi agent bot add reviewer --template reviewer --on
pr.review`) act on their own when their kind arrives; a failed run retries
once a model up (haiku → sonnet → opus); `bot show` sums a ledger - runs,
failed, retried, PRs and how many merged, the bill.

**Reviews from anywhere**: `corgi agent watch pr approve|request|comment
<ref> [words]` posts on the pull request the row is about, the user's name
on it. **Mute**: `corgi agent mute [1h|off]` - nothing rings, the board goes
on; the phone, the bar and a deck key flip it. **Transcript**: `corgi agent
transcript <session> --json` reads a conversation the way the phone does.
**Any agent on the board**: `corgi agent event start|prompt|tool|done|fail|
permission|stop|end --agent codex --session $ID` from another CLI's hooks.
**A read-only phone**: `corgi agent up --viewer` (or `corgi agent pair
--viewer`) pairs a teammate who can look but press nothing.

**Pairing without a QR**: `corgi agent pair` opens a fresh window
on the running server; `--file` writes `~/Desktop/<laptop>.corgipair` to
AirDrop to the phone, which opens it with corgi and is paired. With
corgi-bar the Mac is findable by a phone beside it the AirDrop way
(Bonjour over Bluetooth / peer-to-peer Wi-Fi / LAN, relayed to the daemon),
no tunnel needed nearby; Handoff shows the session in front on the iPhone's
lock screen. A quick tunnel's address dies with a restart: `agent up` warns,
pushes the new address to paired phones (they relink themselves), and
`corgi agent tunnel setup <host>` makes one that never changes.

From an MCP client: `corgi_watch_switches` reads all of the above per
workspace, `corgi_watch_set` flips the live ones (autoAllow, doneWhen,
compactAt, rebase, lessons, handOver, autoMerge - when the user asks for the
behaviour), `corgi_agent_mute`, `corgi_agent_attempts` (with `pick`),
`corgi_agent_lessons` (with `add`).

**The stack from the phone**: with a `corgi-compose.yml` the repo sheet
lists the services at a glance and starts, stops, restarts or tests them
(`/launch/stack`); without one the section stays away. **The whole diff**:
`/launch/diff?all=1`.

**A session whose terminal is gone**: `corgi agent turn <session> "<message>"`
runs one headless turn in its checkout under its account (`acceptEdits`,
nobody to ask; the log is `watch/runs/continue-<id>.log` in the agent dir).
A session that still has a terminal is typed into with `corgi agent send`,
never resumed beside itself. `watch enable --headless` does this on its own
for a message from the phone.

**A run that went the wrong way**: `corgi agent undo <session>` drops every
uncommitted edit and new file in the session's own worktree; `--worktree`
removes the worktree and its `corgi/<ref>` branch too. Only for an isolated
session: one on the user's checkout is refused. It asks first; `--yes` is
the person's to give, never yours.

**The phone reads a session as a chat** only in workspaces the user opened:
`corgi agent stream enable --workspace <id>` (or `--all`), `stream disable`,
`corgi agent stream` to list. Pulled over the sealed link while the chat
sheet is open; what looks like a secret is scrubbed first.

**A pull request body from the session that made the branch**:
`corgi agent pr-body <session>` prints it (what was asked, the commits, the
files, the last test run); `--apply` writes it onto the session's PR.

**A push that a red session cannot make**: `corgi agent guard install` puts
a pre-push hook in this checkout. A branch whose session has a red
done-when gate or a red last test run is refused, with the command that
failed. `corgi agent guard check` is what the hook runs. `CORGI_FORCE=1`
is the person's override; do not set it for them.

**The machine went quiet**: `corgi agent pulse https://hc-ping.com/<uuid>`
(then `corgi agent restart`) pings that URL every five minutes, so a
healthchecks.io, Uptime Kuma or cronitor check alarms the user when the
daemon stops. `corgi agent pulse off` clears it.

**The whole loop in a container**: `corgi agent container up` builds and
starts the daemon, the watch and the routines on this Docker host;
`--host ssh://me@build-1` on a server; `--clone <git url>` registers a
stack at first start; `--docker` mounts the host's Docker socket so a fix
can run the stack. `container shell | logs | stop | down | status` for the
rest. `up` asks once for the Claude sign-in the container will use (`claude setup-token` makes it):
that is the person's step.
