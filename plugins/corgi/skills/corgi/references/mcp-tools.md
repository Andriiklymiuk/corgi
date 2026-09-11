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
| `corgi_status` | `composePath?, service?, unhealthyOnly?` | `[{label, kind, port, url, healthy, detail}]` | **the only liveness truth** — live TCP/HTTP probe. Targets without a declared port are not listed |
| `corgi_ps` | `composePath?` | `[{name, kind, port, status, url, startedAt}]` | `status` = process/container exists (`running`/`crashed`/`stopped`), not health; db_services and container-backed services never show `crashed` |
| `corgi_why` | `service, logLines?` | `{verdict, detail, dependencies[], port, lastExitCode, env, logTail[], nextStep}` | one verdict for one down service — use before the ps/status/logs ladder |
| `corgi_logs` | `service, lines?, grep?, since?, errorsOnly?` | `{service, lines[], truncated?}` | newest captured run; needs a prior `corgi_up`. Filter first (`errorsOnly`, `grep`, `since: "10m"`), then the tail: 200 raw lines is the expensive default |
| `corgi_wait_for_log` | `service, pattern, timeoutSec?` | `{matched, line, waitedMs}` | **blocks** until a line matches — use instead of polling `corgi_logs` |
| `corgi_restart` | `composePath?, profile?` | run-state | `corgi_down` + `corgi_up`, same caveats |
| `corgi_down` | `composePath?` | `{stopped[], failed[]}` | runs `afterStart`, brings dbs down; idempotent |
| `corgi_env` | `composePath?, service?, key?` | `{service: {KEY: {value, source}}}` | real values — never echo into a transcript. Pass `service` or `key`; unfiltered output is capped at 40 vars per service |
| `corgi_exec` | `service, command, ensureDeps?, serviceBranch?, serviceDir?` | `{exitCode, output, truncated, durationMs}` | one-off command in the service's resolved env; **tunnel-gated** |
| `corgi_test` | `service?, profile?, ensureDeps?, changed?, base?, e2e?, serviceBranch?, serviceDir?` | `{services[], passed, note?}` | runs each `test` script; starts nothing. `changed: true` (with `base`, default `main`) is the smallest set for this diff; `e2e: true` runs the compose `e2e:` block |
| `corgi_http` | `service, path, method?, body?, headers?` | `{status, headers, body, truncated, ms}` | one request to a running service by name on 127.0.0.1; JSON body sent as JSON; body capped at 64 KB. The way to check a route as a client would |
| `corgi_explain` | `service, query, analyze?` | `{service, plan, truncated, hint}` | EXPLAIN (or EXPLAIN ANALYZE, which runs the query) on a postgres-family db; the hint names the sequential scan or spilled sort; **tunnel-gated** |
| `corgi_db_query` | `service, query` | `{service, output, truncated}` | driver's own client syntax; writes are not blocked; **tunnel-gated**. Take a `corgi_db_snapshot` before a mutating query |
| `corgi_db_snapshot` | `service?, name?, force?` | `{service, name, archive, sizeBytes}` | postgres-family only; refused while `corgi_up` services run (databases-only or after `corgi_down`) |
| `corgi_db_restore` | `name, service?, force?` | `{service, archive}` | **wipes the data volume**; **tunnel-gated** |
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
| `corgi_watch_status` | `workspace?` | per workspace: what is watched, which sources have a token (fingerprint only), last poll, `whatIsMissing[]` | read this BEFORE enabling — half the answer is usually already there |
| `corgi_watch_enable` | `workspace?, tracker?, project?, repos[], states[], labels[], comments?, prs?, action?` | the saved rules + `whatIsMissing[]` | `project`/`repos` are what route an event to a workspace — derive them from the repos, never guess. **Takes no token**: tokens go through `corgi agent watch auth --local`, never a tool call. Needs `corgi agent restart` |
| `corgi_watch_fixes` | `workspace?, limit?` | `[{key, ref, kind, workspace, startedAt, running, issueUrl?, prs[], note?, error?}]` | what the unattended mode did and what it opened — outlives the notification that announced it |
| `corgi_today` | `since?` | `{since, headline, totals, workspaces[]}` | "what have I done today?" in one call: commits, prompts, and the unattended runs with their PRs. Midnight unless `since` is a duration |
| `corgi_watch_board` | `workspace?, refresh?` | `{tracker, project, columns[], me}` | the tracker's real columns, cached — read this before offering a move, never guess a column name |
| `corgi_watch_move` | `workspace?, ref, status` | `{done}` | moves one ticket. A refused move names the columns it could have gone to |
| `corgi_watch_events` | `workspace?, limit?` | `[{key, kind, ref, title, url, workspace, at, canWorkOn}]` | answers "what came in?"; `canWorkOn` means a skill exists — issue → `/corgi:stories`, review → `/corgi:review` |
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
