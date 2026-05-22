# corgi as an MCP server

`corgi mcp` runs corgi as a [Model Context Protocol](https://modelcontextprotocol.io)
server over **stdio** (JSON-RPC). MCP clients (Claude Code, Claude Desktop)
spawn it as a subprocess and call corgi's commands as structured tools — no
CLI text parsing, every tool returns JSON.

Each tool is a thin wrapper over the same function the CLI uses, so a tool's
output matches the corresponding `corgi <cmd> --json` output for the same input.

## Client config

Register corgi in `.mcp.json` (project-local) or `~/.claude.json` (global):

```json
{ "mcpServers": { "corgi": { "command": "corgi", "args": ["mcp"] } } }
```

The server resolves `corgi-compose.yml` from the working directory the client
launches it in. Any tool also accepts an explicit `composePath`.

stdio is the default and recommended transport for local use — the client owns
the process lifecycle and nothing is exposed on the network.

## HTTP transport

Instead of stdio, corgi can serve the same tools/resources over Streamable HTTP:

```
corgi mcp --http :8765          # or 127.0.0.1:8765
```

The endpoint is served at `/mcp`. Point an HTTP-capable MCP client at the URL
rather than a command:

```json
{ "mcpServers": { "corgi": { "url": "http://localhost:8765/mcp" } } }
```

**Security:** the HTTP transport has no authentication. It exposes corgi
control — including `corgi_up`, which starts containers and processes. Bind it
to `localhost` or put it behind an authenticated reverse proxy. Never expose it
on a public interface. corgi prints this warning to stderr on startup.

## Tools

| Tool | Args (JSON) | Returns | Wraps |
|------|-------------|---------|-------|
| `corgi_validate` | `{composePath?}` | `{ok, errors[], warnings[]}` | `utils.ValidateCompose` |
| `corgi_plan` | `{composePath?, profile?}` | dry-run plan (`order`, `databases`, `services`, `warnings`) | `computeDryRunPlan` |
| `corgi_status` | `{composePath?}` | `[{label, port, kind, url, healthy, detail}]` | `collectStatusRows` + `probeAll` |
| `corgi_ps` | `{composePath?}` | `[{name, kind, port, status, url}]` | `buildPsRows` |
| `corgi_up` | `{composePath?, profile?, seed?}` | run-state (`services[]`, `dbServices[]`) — **always detached** | run prelude + `runDetached` machinery |
| `corgi_down` | `{composePath?}` | `{stopped[], failed[]}` | stop machinery (`stopProcessGroup`) |
| `corgi_logs` | `{service, lines?}` | `{service, lines[]}` | newest captured log run |
| `corgi_exec` | `{service, command, ensureDeps?}` | `{exitCode, output, durationMs}` | `RunServiceCommandExitCode` (output captured) |
| `corgi_schema` | `{}` | the JSON Schema (draft-07) as text | `utils.ComposeJSONSchema` |

`corgi_up` is **always detached**: it brings databases up, generates env, then
spawns each service as a detached process group and writes
`corgi_services/.state.json`, returning immediately. Use `corgi_down` to stop.

## Resources

| URI | Content |
|-----|---------|
| `corgi://schema` | JSON Schema (draft-07) for `corgi-compose.yml` (static) |
| `corgi://compose` | the resolved/interpolated current compose, marshaled to JSON |
| `corgi://status` | live status snapshot (re-read on each fetch) |

## Errors

Tool failures come back as MCP tool errors whose message is prefixed with the
stable error code (see `docs/agents.md`), e.g.
`E_COMPOSE_NOT_FOUND: …`, `E_SERVICE_NOT_FOUND: …`, `E_PORT_CONFLICT: …`.
Agents can branch on the code prefix.

## stdout purity

stdout is the JSON-RPC channel. The server forces non-interactive mode and
routes all of corgi's human/JSON logging to stderr; the startup banner is
suppressed for the `mcp` subcommand. `corgi_exec` captures the child command's
combined output into the returned `output` field rather than streaming it.
