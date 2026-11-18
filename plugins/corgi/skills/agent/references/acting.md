# Acting on the board from elsewhere

Everything the keys do is a command, so the same board reaches other places:

```bash
corgi agent send <session> --enter "run the tests"   # type into a session (VS Code terminal, iTerm, Terminal.app)
corgi agent answer <session> allow|always|deny       # its permission prompt; risky Bash (rm -rf, sudo, --force, drop) is refused
corgi agent note <session> "waiting on PR"           # a line of yours under the session
corgi agent usage [--json|--watch]                   # every account: 5h and week windows, forecast, when a reached limit lifts
corgi agent carry <session> --profile work           # continue a limited session under another listed account
corgi agent continue on                              # daemon types "continue" into a limited session once its window resets (off by default)
corgi agent watch ignore <REF|key>                   # a ticket out of the inbox everywhere, for good; unignore puts it back
corgi agent watch done <REF|link|key>...             # handled on this side (you replied, pushed, reviewed): its rows so far leave every surface; later rows still come. watch comment / watch pr mark it themselves
corgi agent claude --profile auto                    # start under the listed account with the most budget
corgi agent watch enable --auto-for reviews,comments # work those unattended; a fresh ticket only notifies
corgi agent watch enable --ci                        # red builds too: the one kind with its own test for done
corgi agent watch enable --from max                  # only the reviewer you are waiting on
corgi agent watch undo [REF] [--dry-run]             # close what a run opened, put its ticket back
corgi agent watch replay [--since 168h]              # the week unattended mode would have had
corgi agent while-away                               # what corgi opened, could not do, is still running
corgi agent dashboard                                # open the dashboard on this machine, paired
corgi agent watch enable --pickup "In Progress"      # picking a story up moves it on the board
corgi agent watch board [--refresh]                  # the tracker columns corgi knows
corgi agent watch move ABC-123 "Ready for staging"  # move, assign, comment: writes as you
corgi agent watch comment ABC-123 "text" --file a.png --file repro.mp4 --file report.pdf  # pictures, videos and documents show inside the comment (Jira needs this); [!12](url) and URLs are links
corgi agent today [--write]                          # today since midnight, the watch's own runs included
corgi agent today --json                             # + waits{count, medianS} and days[14] from the daemon's own ledger - the phone's share card numbers
corgi agent standup [--since 48h] [--write]          # a rolling window of the same
corgi agent left [--done <id>]                       # what unattended runs left for a person; green checks and merges tick items off by themselves
corgi agent selfcheck [--days 7]                     # what kept going wrong in unattended runs (skipped checks and why, machine failures, idle spend)
corgi agent digest --send                            # the daily message (digestAt in the user config)
corgi agent workspaces pause|resume <id>             # stop supervising one (autostart: false), or resume
```

Surfaces that draw the board: **corgi-bar** (macOS menu bar: rows by
workspace, Allow/Deny, accounts, Talk, Remote devices with Pause), the
**VS Code extension** (Agent sessions view, status bar, toast with Go, quick
pick), **Corgi Agent Deck** (Stream Deck keys; "+" opens a session in the
window in front, a hold picks another open window), and the **Telegram bot**
(`corgi agent notify telegram --token …`: reply to a "needs you" message to
type into it; `/sessions`, `/usage`, `/send`, `/allow`, `/always`, `/deny`,
`/focus`). "limit lifted on <account> - back to work" arrives the moment a
limited session resumes (after a 20 s grace), and "the turn resumed after
the limit … is done" when that turn ends. A limited key says which kind: **quota** (the
account's window is spent; `resets 2pm`, carry or wait) or **overload**
(the API said try later; minutes). With `corgi agent continue on` the daemon
plans the resume itself - from the account's numbers, or from the `resets 2:50am`
in the limit message when those numbers are older than the limit - the key shows
`continues 14:02`, and it gives up after three tries when the limit comes straight
back. A session in the Claude Code panel is continued through the corgi VS Code
extension (it fills the chat box and presses Return; VS Code needs Accessibility once). A finished turn whose background tasks still run says so on the key
("2 tasks still running in the background"): it wakes itself when they end. The **phone launcher** has the same under each
session row: Allow, Always, Deny when it needs you, Send… for any live one,
and a PR link when the session mentioned one; its **New chat** box opens a
session in a chosen editor window with a first prompt, a model and an
account (the prompt travels by id in a 0600 file, never in the shell line).

The board also feeds back into sessions: with tracking on, every new session
starts with a few lines from `corgi agent hook context` (the other sessions
in the workspace with their branch, the account's 5h and week percent and
pace, the last handover brief, the workspace memory count). Read them before
editing: another session on the same branch means coordinate, a budget near
its limit means keep the turn short or `corgi agent carry`.
