# Corgi Agent Usability — Design

Date: 2026-05-21

## Problem

Corgi is built for humans at a terminal: colored TUI output, emoji, interactive
prompts, and a post-command "continue?" confirmation loop. AI agents (Claude
Code and similar) that drive corgi from scripts hit three classes of friction:

1. **Hangs.** Interactive prompts block forever when no human can answer
   (`main.go` continuation loop; `create`/`fork`/`logs`/`db shell` pickers).
2. **Blind parsing.** Only `corgi status` emits JSON. Everything else is colored
   emoji text the agent must scrape.
3. **No authoring contract.** The corgi-compose.yml schema lives only in Go
   structs; agents guess the format. `create`/`fork` are interactive-only, so
   agents cannot scaffold services non-interactively.

## Goals

- Agents never hang on a prompt.
- Every read command can emit machine-readable JSON.
- Agents can discover the compose schema and author/modify services
  non-interactively.
- Zero behavior change for humans at a real terminal.

## Decisions

- Scope: all identified items.
- Machine output via a **global `--json`** persistent flag (consistent with the
  existing `status --json`).
- **Auto-detect non-interactive** when no TTY or an agent/CI env var is present;
  `--interactive` forces prompts back on.

## Architecture

Three new primitives in `utils/`, everything else rides on them.

### 1. `utils.IsTTY()`
Promote the existing `isStdoutTTY()` from `cmd/status.go` into `utils`. Checks
`os.Stdout` (and stdin where relevant) for `os.ModeCharDevice`. Single source of
truth for "is a human terminal attached".

### 2. `utils.NonInteractive` (bool) + `DetectMode()`
Extend the existing `DetectCIMode()` (rename concept to `DetectMode()`, keep
`CIMode` for spinner/banner suppression). `NonInteractive` is true when ANY of:
- a known CI env var is set (existing `ciEnvVars` list), OR
- an agent env var is set (`CLAUDECODE`, `CLAUDE_CODE`, `ANTHROPIC_*`), OR
- stdin or stdout is not a TTY.

A global `--interactive` flag forces `NonInteractive = false`.

When `NonInteractive`:
- the `main.go` continuation prompt is skipped (treated as "do not continue"),
- any command that would open an interactive picker instead errors with a clear
  message naming the flag the agent should pass, and exits 2.

### 3. `utils.JSONOutput` (bool) + helpers
Global `--json` persistent flag sets `utils.JSONOutput`. Helpers:
- `utils.PrintJSON(v any)` — marshals indented JSON to stdout.
- `utils.JSONError(code, message string)` — emits `{"error":{"code":...,
  "message":...}}` to stdout and is the single exit path for errors when
  `--json` is set.

## Behavior changes per command

| Command | Change |
|---|---|
| `main.go` loop | Skip continuation prompt when `NonInteractive`. |
| `create` | Full flag surface for every prompted field; error+exit 2 if a required field is missing under `NonInteractive`. |
| `fork` | Same: flags for selection/name/visibility; guard pickers. |
| `logs` | Without `--service` under `NonInteractive`: error listing valid services (exit 2). Add `--list --json`. |
| `db shell` | Without service name under `NonInteractive`: error (exit 2). `-e/--exec` already non-interactive. |
| `run`,`doctor`,`list`,`config`,`docs` | Render JSON when `--json`. |
| `ps` (new) | Runtime snapshot of running services/dbs. |
| `docs` | New `--json-schema` flag emits JSON Schema for corgi-compose.yml. |

## Schema export (`corgi docs --json-schema`)

Hand-written JSON Schema (draft-07) embedded beside the config structs in
`utils/config.go`. Chosen over runtime reflection: config changes rarely, no new
dependency, and we can attach agent-friendly `description`/`examples` per field.
Emitted to stdout. Carries a `$id`/`$schema` so editors can validate
corgi-compose.yml via a `# yaml-language-server: $schema=` directive.

## `corgi ps`

New command. Reads tracked process state (`utils.StoredProcesses`), db container
state, and configured ports. Default: human table. `--json`: array of
`{name, kind, pid, port, status, url}`. Becomes the single "what is running"
answer so agents stop scraping `run` output.

## Errors and exit codes

- `0` success.
- `1` operational failure (unhealthy, check failed, command error).
- `2` usage / missing-input (interactive input required but unavailable).

Under `--json`, all error paths go through `utils.JSONError` so agents parse a
stable shape instead of regexing stderr.

## Docs

`docs/agents.md`: safe flag recipes, JSON output examples, the exit-code table,
and an explicit "do not call interactively without these flags" list. The corgi
skill points at it.

## Build order (dependency-sorted, each step shippable)

1. Foundation: `IsTTY`, `NonInteractive`/`DetectMode`, `JSONOutput`,
   `PrintJSON`, `JSONError`, global `--json`/`--interactive` flags.
2. Hang-killers: continuation-prompt skip, picker→error guards.
3. JSON output rollout per read command.
4. `corgi ps`.
5. `create`/`fork` flag surfaces.
6. Schema export.
7. `docs/agents.md` + skill pointer.

## Testing

TDD per step. Table tests for `DetectMode` (env matrix), `IsTTY` (mocked fd),
JSON helpers (golden output), and per-command JSON shape. Non-interactive guards
tested by asserting exit 2 + error message when input is withheld.

## Non-goals

- No daemon / long-running API server.
- No change to how services are actually launched.
- No removal of human TUI features — humans see identical behavior.
