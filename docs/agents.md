# Driving corgi as an agent

Guide for AI agents and scripts running corgi non-interactively.

## Non-interactive mode

Corgi auto-detects when it must not prompt and either skips the prompt or exits
with a clear error (exit code 2) instead of hanging. It is triggered by any of:

- A CI env var (`CI=true`, etc.).
- An agent env var: `CLAUDECODE`, `CLAUDE_CODE`, `ANTHROPIC_AGENT`.
- No TTY on stdin **or** stdout (piped / redirected).

Force prompts back on with the global `--interactive` flag.

## JSON output

Global `--json` makes stdout pure machine-readable JSON; human/log lines go to
stderr. Commands that emit pure-JSON stdout:

- `status --json` (one-shot: array; `--watch`: NDJSON, one object per transition)
- `doctor --json`
- `list --json`
- `config --json`, `config path --json`
- `ps --json`
- `docs --json-schema`

`run --json` is best-effort: it prints one JSON startup summary
(`{"started":[...],"failed":[...]}`) and then streams service logs to stderr —
stdout is not a single JSON document for the whole run.

Errors under `--json` have the shape:

```json
{"error": {"code": "INPUT_REQUIRED", "message": "..."}}
```

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Operational failure (a check/command failed) |
| 2 | Usage / missing required input |

## Commands that need a flag (or error in non-interactive mode)

These would normally show a picker; non-interactively they exit 2 unless you
pass the flag:

- `logs` → `--service <name>` (or `--all`)
- `db shell` → service-name arg, or `-e "<query>"` for one-shot
- `create` → `--kind <db_service|service|required> --name <name>`
- `fork` → `--all` **or** `--service <name>`, plus `--gitProvider <github|gitlab>`
- any command needing the compose file → run from a dir containing
  `corgi-compose.yml`, or pass `-f <path>`

Non-interactive scaffolding:

```bash
corgi create --kind db_service --name db --driver postgres --port 5439
corgi create --kind service    --name api --path ./api --port 8080
corgi fork --all --gitProvider github
corgi fork --service api --gitProvider github --newName api-fork --private
```

## Schema

Get a draft-07 JSON Schema for `corgi-compose.yml`:

```bash
corgi docs --json-schema > corgi-compose.schema.json
```

In an editor, point the YAML language server at it with a top-of-file directive:

```yaml
# yaml-language-server: $schema=./corgi-compose.schema.json
```

## Safe agent recipe

`corgi run` is long-running and has no detach flag — background it. Probe with
`status`/`ps`, never by re-running `run`.

```bash
# 1. preflight (exit 1 if a port is taken / docker down)
corgi --json doctor

# 2. launch in the background
corgi run > /tmp/corgi.run.log 2>&1 &

# 3. block until every probed target is up (exit 1 on timeout)
corgi status --ready --timeout 2m

# 4. inspect declared targets / health as JSON
corgi --json status
corgi --json ps

# 5. read a service's persisted logs (requires `corgi run --logs`)
corgi logs --service api --idle 0

# 6. tear down
corgi clean -i all
```

## JSON output examples

`corgi --json doctor` (object with `ok` + `checks`; `checks` is `null` when no
ports are declared):

```json
{
  "ok": true,
  "checks": [
    {"name": "port:8080", "ok": true}
  ]
}
```

`corgi --json ps` (array of declared targets):

```json
[
  {
    "name": "app",
    "kind": "service",
    "port": 8080,
    "status": "stopped",
    "url": "http://localhost:8080"
  }
]
```

`corgi --json config`:

```json
{
  "version": 1,
  "notifications": true,
  "path": "/Users/you/.corgi/config.yml"
}
```

Top-level keys of the compose schema:

```bash
corgi docs --json-schema | jq '.properties | keys'
# ["afterStart","beforeStart","db_services","description","init",
#  "name","required","services","start","useAwsVpn","useDocker"]
```
