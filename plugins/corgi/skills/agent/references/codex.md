# Codex as the harness

`kind: codex` on a workspace or a profile in the user config makes corgi drive
Codex there: `corgi agent claude` opens `codex` (permission modes map:
`acceptEdits` → `--full-auto`, `bypassPermissions` → no sandbox), and the
daemon's fixes, bots and routines run `codex exec --json` and read its
receipt (thread id, tokens). `corgi agent codex` opens it once (`corgi agent claude --kind codex` is the same); `corgi agent watch enable
--kind codex` sets the workspace's kind for good.

**An order of agents** (`agents: [claude, codex]` on a workspace or under
`defaults`; `corgi agent workspaces agents <id> claude,codex`, `--default`,
`corgi agent init --agents`, `watch enable --agents`) is what runs when the
first cannot. `utils/agent/daemon/harnesses.go`: every unattended run calls
`pickHarness` - the first agent that is installed, whose window is not spent
(claude's `usage` limits) and whose newest finished run of the day in this
workspace did not fail on a login, a permission or a limit (`FixRecord.Harness`
tags each run, `watch.Sidelining`). Nothing is remembered between runs. A
fallback drops `CLAUDE_CONFIG_DIR` (its own home, its own login). A spent
window is no reason to defer (`fixDeferral`) when a fallback can run, and the
peers hear `unwell` only when every agent is out (`laptopUnwell`; the MCP-side
pulse reads `watch/agents.json`). The daemon logs the pick and rings once a day
per workspace and agent. `corgi agent claude` opens the first installed agent
of the order; `--kind` overrides. `corgi agent doctor` has an `agents` check. `corgi agent track enable`
writes Codex's hooks when the installed codex runs them (`codex features
list` shows `hooks … true`): `~/.codex/hooks.json` (`CODEX_HOME`) gets
SessionStart (emit + context), UserPromptSubmit, PreToolUse,
PermissionRequest, PostToolUse, Stop (emit + budget) and SessionEnd, all
`corgi agent hook emit --agent codex` - the same stdin JSON as Claude Code,
so a codex row has its status, tool, risk word, TTY / tmux pane / window,
context % (from the rollout's `token_count`) and a Stop summary (from
`last_assistant_message`); corgi's old notify line is removed so a turn is not
reported twice. Codex fires PermissionRequest before its own reviewer
answers, so with `approvals_reviewer = "auto_review"` (top level of the
project's or the user's `.codex/config.toml`) or `permission_mode:
bypassPermissions` the emit hook records it as PreToolUse - work, not a
wait, no "needs you" ring. Codex runs a hook only once the person trusts it in `/hooks`
(the trust is a hash in `config.toml` under `hooks.state`) - corgi never
writes that trust. An older codex keeps the `notify` line (someone else's
notify is left alone, since Codex runs one). `configDir` moves `CODEX_HOME`;
`OPENAI_API_KEY` is the credential. `pickHarness` reads codex's window from
the newest rollouts' `rate_limits` (`usage.ReadCodexWindow`): 98 % or
reached, before its reset, is `limit`, wherever codex stands in the order. Adding another agent is one more
entry in `utils/agent/harness`.

**One session, either harness.** `harness.Open` is what a person's
session asks for - permission mode, model, instructions, resume, first
prompt - and each harness spells it (`OpenArgs`): claude `--permission-mode
bypassPermissions --model --append-system-prompt --resume --fork-session`,
codex `resume <id> --dangerously-bypass-approvals-and-sandbox -m` with the
instructions in front of the prompt. `corgi agent claude|codex` builds it in
`resolveLaunch`, so a workspace's `dangerouslySkipPermissions: true` opens
codex without approvals (what `codex --dangerously-bypass-approvals-and-sandbox`
does by hand), and a bot's soul, model and last thread go through whichever
opens. `configDir` reaches only the first agent of the order: codex under a
claude account keeps its own `~/.codex` login. A model of the other harness
(`opus`, `sonnet`, `opusplan` on codex; `gpt-*`, `o3` on claude) is dropped
so the run goes ahead on the default (`Harness.Model`), which is how the
daemon's model policy, written in claude's words, survives a codex fallback.
`corgi agent new --agent codex` and `/launch/new {agent}` open a named harness
from the editor or the phone.

**Handing work across harnesses.** A conversation cannot cross harnesses, so
`corgi agent carry <session> --to codex` (or `--to claude`) is always a fresh
start: the handoff packet is written (`From.Harness` is the session's own),
and `corgi agent codex --workspace <id> --prompt-id …` opens in the same
workspace with the packet as its first prompt. Only an agent the workspace
lists in its `agents` order may take it (`checkHandTarget`). A codex session
carried to another account starts fresh too (its threads live in
`~/.codex`), and cannot `--fork`. `POST /launch/carry {session, to | profile |
fresh}` is the phone's route; the ended row keeps `agent`, so a headless turn
for a gone codex session runs `codex exec resume <thread>`.
