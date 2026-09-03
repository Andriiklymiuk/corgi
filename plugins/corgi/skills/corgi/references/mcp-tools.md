# corgi MCP tools

`corgi mcp` serves the CLI as MCP tools (stdio by default; `--http` for a phone or a
remote client). Every tool takes an optional `composePath` (default: cwd). Results are
JSON; errors carry the same stable `E_*` codes the CLI uses. The server sends these
rules as its instructions at initialize, so a client with no skills loaded still sees
them.

## Order of operations

1. `corgi_context` — where am I: topology, ports, health, each repo's branch/dirty
   state, active tier, profiles, validation. Make it first in a workspace you have
   not looked at this session.
2. `corgi_workspace_resolve { query }` when the user names a stack by a human name —
   returns one workspace or candidates; never guesses. Echo the resolved path back
   before working in it.
3. `corgi_up` → poll `corgi_status` → work → `corgi_down`.

## Stack lifecycle

| Tool | Args | Returns | Notes |
|------|------|---------|-------|
| `corgi_validate` | `composePath?` | `{ok, errors[], warnings[]}` | static, no side effects |
| `corgi_plan` | `composePath?, profile?` | `{order, databases, services, warnings}` | dry run |
| `corgi_doctor` | `composePath?` | `{ok, checks[]}` | required tools, Docker, ports |
| `corgi_up` | `composePath?, profile?, seed?, serviceBranch?, serviceDir?` | run-state `{services[], dbServices[]}` | **always detached; not a ready gate.** Runs every `beforeStart` before returning — minutes on a cold stack. `E_ALREADY_RUNNING` while a run is live → `corgi_down` first |
| `corgi_status` | `composePath?` | `[{label, kind, port, url, healthy, detail}]` | **the only liveness truth** — live TCP/HTTP probe. Targets without a declared port are not listed |
| `corgi_ps` | `composePath?` | `[{name, kind, port, status, url, startedAt}]` | `status` = process/container exists (`running`/`crashed`/`stopped`), not health; db_services and container-backed services never show `crashed` |
| `corgi_why` | `service, logLines?` | `{verdict, detail, dependencies[], port, lastExitCode, env, logTail[], nextStep}` | one verdict for one down service — use before the ps/status/logs ladder |
| `corgi_logs` | `service, lines?` | `{service, lines[]}` | newest captured run; needs a prior `corgi_up` |
| `corgi_wait_for_log` | `service, pattern, timeoutSec?` | `{matched, line, waitedMs}` | **blocks** until a line matches — use instead of polling `corgi_logs` |
| `corgi_restart` | `composePath?, profile?` | run-state | `corgi_down` + `corgi_up`, same caveats |
| `corgi_down` | `composePath?` | `{stopped[], failed[]}` | runs `afterStart`, brings dbs down; idempotent |
| `corgi_env` | `composePath?` | `{service: {KEY: {value, source}}}` | real values — never echo into a transcript |
| `corgi_exec` | `service, command, ensureDeps?, serviceBranch?, serviceDir?` | `{exitCode, output, truncated, durationMs}` | one-off command in the service's resolved env; **tunnel-gated** |
| `corgi_test` | `service?, profile?, ensureDeps?, serviceBranch?, serviceDir?` | `{services[], passed}` | runs each `test` script; starts nothing |
| `corgi_db_query` | `service, query` | `{service, output, truncated}` | driver's own client syntax; writes are not blocked; **tunnel-gated** |
| `corgi_schema` | — | JSON Schema text | also the `corgi://schema` resource |

## Repositories

| Tool | Args | Returns | Notes |
|------|------|---------|-------|
| `corgi_checkout` | `branch?, allowDirty?` | `[{name, branch, status, usedDefaultBranch, message}]` | every repo onto one branch (or its own default), fast-forwarded; dirty repos skipped |
| `corgi_checkpoint` | `name?` | `{name, createdAt, repos[]}` | records branch, HEAD and uncommitted work per repo |
| `corgi_restore` | `name` | `{checkpoint, safetyCheckpoint, restored[], failed[]}` | current uncommitted work is checkpointed first |
| `corgi_worktrees_materialize` | `branch, services?` | one entry per service with `dir` | one shared branch across every repo; **edit in the returned `dir`**, not the user's checkout; **tunnel-gated** |
| `corgi_diff` | `base?, branch?, includePatch?` | per-repo diff | read-only, no tunnel, no running stack — the default way to show a change on a bad connection |
| `corgi_pr_open` | `branch, title, body?, base?, draft?` | one PR per repo with commits | pushes, calls `gh`/`glab`, cross-links siblings; **tunnel-gated** |
| `corgi_worktrees_release` | `branch, force?` | removed/kept | keeps a worktree with uncommitted changes unless `force`; **tunnel-gated** |

## Agent mode (Remote Control)

| Tool | Args | Returns | Notes |
|------|------|---------|-------|
| `corgi_workspaces` | — | registered stacks with `status` (`ok`/`unreachable`/`disabled`) | |
| `corgi_workspace_resolve` | `query` | one workspace or candidates | never guesses |
| `corgi_agent_status` | — | daemon health, per-workspace session, restarts, wake lock, account | answers "is it up" / "why did it die" |
| `corgi_session_start` | `workspace, profile?, name?` | `state: starting` | poll `corgi_agent_status` for `running` + `sessionUrl`; idempotent |
| `corgi_session_stop` | `workspace` | `state: stopping` | stopping a non-running workspace is a no-op |
| `corgi_session_events` | `workspace, limit?` | timeline, newest first | starts, exits with cause, session links; never session output |
| `corgi_session_brief` | `workspace?` | previous session's branches / dirty repos / worktrees, or `null` | call first when the user resumes after a restart |
| `corgi_preview_start` | `service, branch?, provider?, idleMinutes?` | `state: starting` | public tunnel to a running service; refused for `sensitive`; **tunnel-gated** |
| `corgi_preview_state` | `id?` | `starting` / `ready` (has `url`) / `broken` / `stopped` | `broken` = tunnel up, port silent — usually a build |
| `corgi_preview_freeze` | `id, frozen?` | — | pin against idle reaping; **tunnel-gated** |
| `corgi_preview_stop` | `id` | — | close the public URL when the user is done; **tunnel-gated** |

## The tunnel gate

Over a public tunnel the mutating tools (`corgi_exec`, `corgi_db_query`, `corgi_pr_open`,
`corgi_worktrees_*`, `corgi_preview_*`) return an error unless
`CORGI_MCP_ALLOW_DANGEROUS_TUNNEL=1` is set on the server's machine. Read-only tools and
session start/stop stay available; a workspace marked `sensitive` refuses remote start and
previews regardless.
