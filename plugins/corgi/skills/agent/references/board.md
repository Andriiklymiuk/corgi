# Tracking every session on the machine

`corgi agent hooks` covers one workspace. `corgi agent track` covers **every**
Claude Code session on the machine, wherever it was started - a VS Code
terminal, the Claude Code panel, iTerm2 - and keeps them on a fixed board of
keys the daemon publishes as `sessions.json`. That board is what a Stream Deck
plugin draws, what the phone launcher shows at the top ("2 waiting on you"),
what `corgi_sessions` returns, and what `corgi agent sessions` prints:

```text
7 session(s) on 6 keys, 1 more than fit
 1   ▲ acme-api           NEEDS YOU  permission: Bash  work · vscode-terminal · 12s
 2 📌 ● web                WORKING    Edit              default · vscode-panel · 3m
 6  +2  (press to page)
```

Set it up once, for the whole account (and every corgi profile's config dir):

```bash
corgi agent track enable            # hooks into ~/.claude/settings.json + each profile's dir
corgi agent board --slots 15        # a bigger deck (default 6, a Stream Deck Mini), applied live
corgi agent track enable --no-tab-title
corgi agent doctor                  # "session tracking" and "session board" lines
```

What the user gets, and the words to use for it:

- **Status per session**: `working` (model or tool running), `needs_input`
  (permission prompt, a question, an API failure - the one that matters),
  `done`, `stale` (30 min of nothing), `gone` (exited, but the key is pinned).
  Claude's "waiting for your input" nudge after a *finished* turn is not
  `needs_input`; the same nudge mid-turn is.
- **Tab titles**: every terminal tab running Claude reads `● repo`,
  `▲ repo NEEDS YOU` or `✓ repo`. VS Code shows them once
  `terminal.integrated.tabs.title` is `${sequence}` (its default, `${process}`,
  shows only "claude"); the corgi VS Code extension offers that setting once.
- **Focus**: `corgi agent focus <label|id|key>` brings that session's window
  to the front. The exact terminal tab or the Claude Code panel needs the
  corgi VS Code extension (it injects `CORGI_VSCODE_WINDOW` and reveals tabs);
  iTerm2 and Terminal.app tabs are found by tty. Without either, the app comes
  forward and nothing else - never a guessed folder, which would open a new
  window.
- **Keys never move**: a new session takes the lowest free key, an ending one
  frees its key. `corgi agent pin <key>` reserves one (its session stays even
  after exit, dimmed, until `--off`). More sessions than keys → the last
  unpinned key is a `+N` pager, `corgi agent page next|prev` turns it.
- **New session from the deck**: `corgi agent new` opens a fresh terminal
  running `claude` in the last-focused editor window (needs the corgi VS
  Code extension); the session takes the lowest free key within a second.
- **Nothing is lost when the daemon is down**: hooks are async and exit 0, and
  the next daemon rescans the process table (`corgi agent rescan` on demand).
  Remote sessions (`CLAUDE_CODE_REMOTE`) and subagents are never registered;
  neither is the `claude remote-control` server corgi itself supervises.

When a key press goes nowhere, `corgi agent doctor` names sessions with no
known window (`unknown` host). The usual cause is a terminal opened before the
extension activated: reopen it. `corgi agent windows` lists the editor windows
the extension has connected. `corgi agent track disable` removes the hooks and
touches nothing else in `settings.json`.

From a phone, `corgi_sessions` is the same board as JSON - call it for "is
anything waiting on me" or "what is running right now"; it says so when
tracking is not enabled yet.

## The board from a terminal

```bash
corgi agent top                              # live, like top: j/k move, a allow, d deny, i interrupt, o open, q quit
corgi agent ask "what should I look at first?"   # one short answer about the whole board
corgi agent ask --diff api·APP-412           # what that session's branch does, three lines and a risk word
corgi agent transcript <session> --json      # the newest 40 entries and the offset after them
corgi agent transcript <session> --why       # the conversation as steps: what was said, the tools run on it, the files touched
corgi agent logs <workspace>                 # starts, exits and why (alias: timeline)
corgi agent dismiss <session>                # free the key of a done, idle or closed session
corgi agent resolve <name>                   # which workspace a name means, nothing started
```

- **"Why did it do that?"** → `transcript --why`, not a guess from the diff.
- **`ask` reads the board only**: sessions, inbox, kanban, workspace names. A question
  about the code goes to a session in that repo.
- **`dismiss` never touches the session.** Its next event puts it back on a key. A
  working or waiting session is refused.

## Sessions that share a repository

```bash
corgi agent claim auth/refresh.go auth/session.go   # "these files are mine for now"
corgi agent claims                                  # who holds what
corgi agent claim --release                         # give all of this session's back
corgi agent cap 50M                                 # the token budget every session runs under
corgi agent cap <session> 20M                       # one session's own; `off` removes it
corgi agent cap --repo api 20M                      # tokens per day for a workspace
```

A claim is advisory: nothing is locked, the daemon rings once when another session edits
a claimed file. Claim before a wide edit when `corgi agent claims` shows a second
session in the repo; release when the PR is up. A cap stops nothing either: the row says
"over budget" and the daemon rings once.
