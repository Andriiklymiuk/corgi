# corgi agent

## corgi agent

Keep Claude Code Remote Control running for your corgi workspaces

### Synopsis

Agent mode makes this machine an always-on, multi-repo Remote Control host.

Remote Control already gives you a phone-driven Claude Code session on your own
machine. Two things stop it being always-on: the local process must keep
running, and it exits after roughly ten minutes awake without network.

corgi agent supervises it — restarting after a network timeout, holding a wake
lock so the machine does not sleep mid-session, and running one per workspace
under that workspace's own Claude config directory.

Getting started:
  corgi agent init      # in a stack, once
  corgi agent install   # start at login
  corgi agent status    # what is running, and under which account

### Options

```
  -h, --help   help for agent
```

### Options inherited from parent commands

```
      --describe                  Describe contents of corgi-compose file
      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")
  -l, --exampleList               List examples to choose from. Click on any example to download it
  -f, --filename string           Custom filepath for for corgi-compose
      --fromScratch               Clean the .corgi/corgi_services folder before running
  -t, --fromTemplate string       Create corgi service from template url
      --fromTemplateName string   Create corgi service from template name and url
  -g, --global                    Use global path to one of the services
      --interactive               Force interactive prompts even when no TTY/agent detected
      --isolate string            Run this workspace under a named lease: own port block, own database names, own containers
      --json                      Emit machine-readable JSON output
      --privateToken string       Private token for private repositories to download files
  -o, --runOnce                   Run corgi once and exit
      --silent                    Hide all welcome messages
```

### SEE ALSO

* [corgi](corgi)	 - Corgi cli magic friend
* [corgi agent answer](corgi_agent_answer)	 - Answer a session's permission prompt from anywhere
* [corgi agent approve](corgi_agent_approve)	 - Approve a Claude connector sign-in waiting on this machine
* [corgi agent ask](corgi_agent_ask)	 - Ask the chief about the board: what to look at first, what is blocked, who is on what
* [corgi agent attempts](corgi_agent_attempts)	 - Compare the sessions a fan-out opened on a ticket, and pick one
* [corgi agent awake](corgi_agent_awake)	 - Keep this machine awake for as long as the agent daemon runs
* [corgi agent away](corgi_agent_away)	 - Set this machine up to work alone for weeks, and say what only you can do
* [corgi agent board](corgi_agent_board)	 - Show or set how many keys the session board has
* [corgi agent bot](corgi_agent_bot)	 - Named sessions you come back to: a workspace, a persona, a model, a thread
* [corgi agent brief](corgi_agent_brief)	 - What the last supervised session was working on before it restarted
* [corgi agent cap](corgi_agent_cap)	 - A token budget for every session, or for one
* [corgi agent carry](corgi_agent_carry)	 - Continue a session under another account, conversation and all
* [corgi agent chat](corgi_agent_chat)	 - Speak in the chat corgi listens to
* [corgi agent claim](corgi_agent_claim)	 - Say which files this session is on, so another session in the repository is told
* [corgi agent claims](corgi_agent_claims)	 - Who holds which files, across the board
* [corgi agent claude](corgi_agent_claude)	 - Run Claude Code the way this folder's workspace is configured
* [corgi agent codex](corgi_agent_codex)	 - Run Codex the way this folder's workspace is configured
* [corgi agent continue](corgi_agent_continue)	 - Let the daemon continue a limited session when its limit is over
* [corgi agent cost](corgi_agent_cost)	 - What the agents cost: tokens by repo, by day, or by bot
* [corgi agent dashboard](corgi_agent_dashboard)	 - Open the dashboard in this machine's browser
* [corgi agent digest](corgi_agent_digest)	 - Today's one-message summary, as the daily digest would send it
* [corgi agent dismiss](corgi_agent_dismiss)	 - Take a finished session off the board until its next event
* [corgi agent doctor](corgi_agent_doctor)	 - Check whether agent mode can actually work here
* [corgi agent down](corgi_agent_down)	 - Stop everything `corgi agent up` started — the daemon and the detached MCP + tunnel
* [corgi agent event](corgi_agent_event)	 - Report one event from an agent that is not Claude Code, so it sits on the board
* [corgi agent focus](corgi_agent_focus)	 - Bring a session's window to the front and reveal its terminal tab
* [corgi agent guard](corgi_agent_guard)	 - A pre-push hook that refuses a branch whose session's gate or tests are red
* [corgi agent handoff](corgi_agent_handoff)	 - Leave a typed handoff for the next run on this ticket
* [corgi agent harden](corgi_agent_harden)	 - Write the safe defaults into this workspace's Claude settings
* [corgi agent hooks](corgi_agent_hooks)	 - Get notified when a session in this workspace needs you
* [corgi agent init](corgi_agent_init)	 - Opt this stack into agent mode
* [corgi agent install](corgi_agent_install)	 - Start agent mode at login (launchd on macOS, systemd on Linux)
* [corgi agent interrupt](corgi_agent_interrupt)	 - Stop what a session is doing — Escape, as you would press it
* [corgi agent kanban](corgi_agent_kanban)	 - One card per ticket: Inbox, Ready, Running, Blocked, Review, Done
* [corgi agent lesson](corgi_agent_lesson)	 - What this workspace learned the hard way — read by every new session
* [corgi agent logs](corgi_agent_logs)	 - Show a workspace's session timeline (starts, exits, why)
* [corgi agent mute](corgi_agent_mute)	 - Nothing rings for a while — no toast, no push; the board goes on
* [corgi agent new](corgi_agent_new)	 - Open a new Claude Code session in the editor window in front
* [corgi agent note](corgi_agent_note)	 - Put your own line under a session on the board
* [corgi agent notify](corgi_agent_notify)	 - Where notifications go when you are away from this machine
* [corgi agent page](corgi_agent_page)	 - Turn the board's overflow page when more sessions run than keys
* [corgi agent pair](corgi_agent_pair)	 - Open a fresh pairing window and print its QR — or write a .corgipair file to AirDrop
* [corgi agent peers](corgi_agent_peers)	 - The other laptops on your trackers: who leads, who is awake
* [corgi agent pin](corgi_agent_pin)	 - Reserve a key for the session on it (--off to release)
* [corgi agent plan](corgi_agent_plan)	 - A planner breaks a goal into tasks on the board; the daemon works through them, a worktree each
* [corgi agent pr-body](corgi_agent_pr-body)	 - A pull request description from the session that made the branch
* [corgi agent profile](corgi_agent_profile)	 - Manage named Claude account profiles for remote session start
* [corgi agent pulse](corgi_agent_pulse)	 - Ping a URL every five minutes so a service alarms you when this machine goes quiet
* [corgi agent refresh](corgi_agent_refresh)	 - Reload everything now: rescan sessions, poll every tracker, publish
* [corgi agent rescan](corgi_agent_rescan)	 - Look for running Claude sessions no hook has reported
* [corgi agent resolve](corgi_agent_resolve)	 - Show which workspace a name resolves to, without starting anything
* [corgi agent restart](corgi_agent_restart)	 - corgi agent down && corgi agent up --fresh, in one command
* [corgi agent routine](corgi_agent_routine)	 - Runs on a clock: digest, babysit-pr, deps, release-notes, flaky, doc-drift, suggest, or your own
* [corgi agent scan](corgi_agent_scan)	 - Find corgi stacks under a directory and register them
* [corgi agent scope](corgi_agent_scope)	 - The contract a ticket's change stays inside: paths, a diff budget, done-when
* [corgi agent send](corgi_agent_send)	 - Type text into a session's terminal, after bringing it forward
* [corgi agent serve](corgi_agent_serve)	 - Supervise Remote Control for every enabled workspace
* [corgi agent session](corgi_agent_session)	 - Start or stop a supervised session in a workspace, on demand
* [corgi agent sessions](corgi_agent_sessions)	 - Show every tracked Claude Code session as the board of keys
* [corgi agent standup](corgi_agent_standup)	 - What you and Claude did, per workspace, since yesterday
* [corgi agent status](corgi_agent_status)	 - Show what agent mode is running, and under which account
* [corgi agent stop](corgi_agent_stop)	 - Stop the agent daemon
* [corgi agent stream](corgi_agent_stream)	 - Which workspaces a paired phone may read as a conversation
* [corgi agent task](corgi_agent_task)	 - Tasks of your own on the board: add one for later, pick it up, move it along
* [corgi agent timeline](corgi_agent_timeline)	 - What happened in a session, in order: prompts, tool bursts, tests, the pull request and its reviews
* [corgi agent today](corgi_agent_today)	 - What has been done today — yours and the watch's
* [corgi agent top](corgi_agent_top)	 - The board in the terminal, live: every session, its standing, what it is doing
* [corgi agent track](corgi_agent_track)	 - Track every Claude Code session on this machine for a Stream Deck or the CLI
* [corgi agent transcript](corgi_agent_transcript)	 - A session's conversation: the newest entries, or everything after an offset
* [corgi agent tunnel](corgi_agent_tunnel)	 - Set up the permanent launcher URL
* [corgi agent turn](corgi_agent_turn)	 - One more turn for a session whose terminal is gone — claude -p --resume
* [corgi agent undo](corgi_agent_undo)	 - Drop what a session left uncommitted in its own worktree; --worktree drops the worktree and its branch too
* [corgi agent uninstall](corgi_agent_uninstall)	 - Stop starting agent mode at login
* [corgi agent up](corgi_agent_up)	 - One command from a stack directory to phone-startable: register, daemon, tunnel, pairing
* [corgi agent usage](corgi_agent_usage)	 - Every account's limits, where they are heading, and today's numbers
* [corgi agent watch](corgi_agent_watch)	 - Watch the tracker and your pull requests: new issues, new comments, reviews — notify, or fix
* [corgi agent while-away](corgi_agent_while-away)	 - What happened while you were gone
* [corgi agent windows](corgi_agent_windows)	 - List the editor windows the corgi VS Code extension has connected
* [corgi agent workspaces](corgi_agent_workspaces)	 - List and manage the workspaces agent mode knows about

###### Auto generated by spf13/cobra on 20-Sep-2026
