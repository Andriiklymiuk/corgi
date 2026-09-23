# corgi as an MCP server

`corgi mcp` runs corgi as a [Model Context Protocol](https://modelcontextprotocol.io)
server over **stdio** (JSON-RPC). MCP clients (Claude Code, Claude Desktop)
spawn it as a subprocess and call corgi's commands as structured tools - no
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

stdio is the default and recommended transport for local use - the client owns
the process lifecycle and nothing is exposed on the network.

## Server instructions

At `initialize` the server sends a short instructions block: which call to make
first (`corgi_context`, or `corgi_workspace_resolve` for a stack named by a human),
that `corgi_up` is not a ready gate, that `corgi_status` - not `corgi_ps` - is
liveness, and which tools the public-tunnel gate refuses. Clients that honour
instructions (Claude Code, Claude Desktop, the Claude app connector) show it to the
model before its first tool call, so a client with no corgi skills loaded still gets
the rules. The plugin's skills carry the same rules in more depth.

## HTTP transport

Instead of stdio, corgi can serve the same tools/resources over Streamable HTTP:

```
corgi mcp --http 127.0.0.1:8765   # a bare :8765 also binds 127.0.0.1
corgi mcp --http :8765 --bind-all # every interface; needs --token or a paired device
```

The endpoint is served at `/mcp`. Point an HTTP-capable MCP client at the URL
rather than a command:

```json
{ "mcpServers": { "corgi": { "url": "http://localhost:8765/mcp" } } }
```

### What the endpoint enforces

`/mcp` follows the MCP Streamable HTTP transport rules, which matter most on
localhost where a web page could otherwise reach corgi through DNS rebinding:

- `POST`, `GET` (an SSE stream) and `DELETE` only; anything else is `405`
  with an `Allow` header.
- A request that carries a browser `Origin` must be a loopback origin, the
  tunnel's own origin, or one passed with `--allow-origin scheme://host[:port]`
  (repeatable; also `CORGI_MCP_ALLOWED_ORIGINS`, comma-separated). Anything
  else is `403`. A request with no `Origin` header (Claude's backend, curl,
  an MCP client binary) passes.
- An `MCP-Protocol-Version` header corgi does not speak is `400`; a missing
  one is taken as `2025-03-26`, as the spec says.
- A `401` carries `WWW-Authenticate: Bearer realm="corgi",
  resource_metadata="<origin>/.well-known/oauth-protected-resource/mcp"`, with
  `error="invalid_token"` added only when a credential was presented. The
  `resource_metadata` pointer is what lets a client find the sign-in.
- Every tool result is cut at 100,000 emitted characters (measured after
  JSON escaping, which is what the client counts) and ends with a
  `{"truncated":true,…}` note; `CORGI_MCP_MAX_RESULT_CHARS` moves the cap.
  Claude.ai and Desktop refuse a result past ~150,000 characters.
- Every tool call is budgeted at 200 s (`CORGI_MCP_CALL_BUDGET`), under
  the 240 s Claude.ai and Desktop allow: `corgi_wait_for_log` clamps its
  timeout and reports `timedOut`, `corgi_exec` and `corgi_test` kill the
  process group at the budget, and `corgi_up` boots the stack in a child
  process and returns a handle within about 20 s (`status: starting`) -
  poll `corgi_status` after it.
- Every tool carries a `title` and a `readOnlyHint` or `destructiveHint`,
  so a connector asks before `corgi_exec` and not before `corgi_status`.

### Add corgi to Claude.ai, Desktop or the phone

corgi is its own sign-in: the `/mcp` endpoint is an OAuth 2.1 resource server
and authorization server on the tunnel origin, so the connector dialog's
preselected *Sign in now* works with one click. No token to copy.

1. Have the endpoint up with a tunnel: `corgi agent up`, or by hand
   `corgi mcp --http 127.0.0.1:8765 --tunnel --pair`. `corgi agent status`
   prints the URLs:

   ```
   corgi agent running (pid 84639, version 2.29.0)
     launcher   https://<host>/app
     connector  https://<host>/mcp   add in Claude: Connect, or No sign-in + Authorization: Bearer <device token>
   ```

2. In Claude, *Add custom connector* → paste the connector URL → leave
   Authentication on **Sign in now** → **Add**. A browser tab opens corgi's
   consent page.
3. Approve. A browser that has opened the dashboard before (`corgi agent
   dashboard`) shows one **Approve** button. Any other browser shows a code:
   run `corgi agent approve ABCD-2345` on the machine the daemon runs on and
   the page finishes on its own. Codes live ten minutes.
4. The connector shows the **Corgi** server with its 52 tools; read-only
   tools run without a prompt, destructive ones ask first.

The access token corgi hands Claude is a paired device named `Claude · oauth
<id>` with a one-hour life; Claude refreshes it silently for 30 days. It
shows in `corgi mcp devices` and `corgi mcp devices revoke "<name>"` ends
the whole grant - Claude then asks you to sign in again. A refresh token
that is replayed after rotation revokes the grant too.

Claude Code can use the same path over HTTP -
`claude mcp add --transport http corgi https://<host>/mcp` opens the same
consent - but stdio (`corgi mcp`) needs none of this.

**The header path still works.** Authentication **No sign-in** plus a
request header `Authorization` = `Bearer corgi_dev_…` from
`corgi agent dashboard --print --name claude-web` (a terminal you are
looking at; the phone's Settings mints one too). A phone's own token is
refused on `/mcp`.

#### What the sign-in serves

| Path | Purpose |
|------|---------|
| `/.well-known/oauth-protected-resource[/mcp]` | RFC 9728: who the authorization server is (corgi itself) |
| `/.well-known/oauth-authorization-server[/mcp]` | RFC 8414: endpoints, `S256`, `none`, `client_id_metadata_document_supported` |
| `POST /oauth/register` | RFC 7591 dynamic registration: public clients, allowlisted redirect URIs, `corgi_client_…` ids, newest 200 kept |
| `GET /oauth/authorize` | PKCE `S256` required; client and redirect URI are checked **before** anything can redirect |
| `POST /oauth/approve`, `GET /oauth/pending/<id>` | the consent page's approve and poll |
| `POST /oauth/token` | `authorization_code` and `refresh_token`, form-encoded; refresh tokens rotate |
| `POST /oauth/revoke` | RFC 7009 |

Client IDs that are `https` URLs are Client ID Metadata Documents (what
Claude and ChatGPT prefer): corgi fetches the document from an allowlisted
host only, refuses any address that resolves to a private or loopback
range, follows no redirects, and caches per `Cache-Control` for at most a
day. Redirect URIs are loopback on any port (RFC 8252) or `https` to
`claude.ai`, `claude.com` or a host passed with `--oauth-client-host`
(repeatable; also `CORGI_MCP_OAUTH_CLIENT_HOSTS`, comma-separated; a
single-label host such as `com` is ignored). Nothing else, so the endpoint
cannot be used as an open redirect.

State lives in `<data>/agent/oauth.json` (clients and refresh-token
families) and `devices.json` (access tokens as devices), both `0600`.
Sign-ins waiting on the consent page are in memory only - at most twenty at
once, ten minutes each.

`--no-oauth` turns all of this off and the `401` loses its
`resource_metadata` pointer; `--insecure` never serves it (there is nothing
to sign in to).

### Bearer-token auth

Plain `corgi mcp --http` stays no-auth (unchanged), so local-only setups are
unaffected. Pass `--token` to require a bearer token, or `--tunnel` to
auto-generate one (`corgi_mcp_<rand>`). The token is printed once to stderr on
startup, and clients send it as an `Authorization: Bearer` header:

```json
{
  "mcpServers": {
    "corgi": {
      "url": "https://mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer corgi_mcp_..." }
    }
  }
}
```

Auth uses a constant-time comparison; mismatches get `401 {"error":"unauthorized"}`
with a `WWW-Authenticate: Bearer realm="corgi", resource_metadata="…"` header.
Tokens are read from the `Authorization` header only, never from the query
string.
Use `--insecure` to disable auth even when a token is set.

### Public tunnels

`--tunnel` opens a public HTTPS tunnel to the `--http` addr using the same
providers as `corgi tunnel` (cloudflared|ngrok|localtunnel). `--tunnel` requires
`--http` (otherwise exit 2). The tunnel subprocess is bound to the server's
lifetime and torn down on SIGINT/SIGTERM.

```
corgi mcp --http 127.0.0.1:8765 --tunnel
corgi mcp --http 127.0.0.1:8765 --tunnel --tunnel-provider ngrok --tunnel-hostname '${API_TUNNEL_HOST}'
corgi mcp --http 127.0.0.1:8765 --tunnel --tunnel-provider cloudflared --tunnel-name my-mcp --tunnel-hostname mcp.example.com
```

- `--tunnel-provider` (default `cloudflared`).
- `--tunnel-hostname` - custom public host; `${VAR}` is expanded from the shell
  env. Per-provider meaning: cloudflared pairs it with `--tunnel-name` for a
  named tunnel; ngrok maps it to `--domain` (a reserved domain); localtunnel
  uses it as the requested subdomain. Omit it for a quick (random-URL) tunnel.
- `--tunnel-name` - cloudflared named-tunnel name.

When the tunnel resolves, corgi prints the public `/mcp` URL plus a ready-to-paste
`mcpServers` block (with the `Authorization` header when a token is set).

**Security:** a public tunnel exposes corgi control - including `corgi_up` and
`corgi_exec`, which run arbitrary commands. A bearer token is generated by
default; combining `--tunnel` with `--insecure` lets ANYONE with the URL run
commands on your machine, and corgi prints a loud warning. Without a tunnel
corgi binds `127.0.0.1` unless you pass `--bind-all`; if you do, front it
with an authenticated proxy.

## Tools

| Tool | Args (JSON) | Returns | Wraps |
|------|-------------|---------|-------|
| `corgi_validate` | `{composePath?}` | `{ok, errors[], warnings[]}` | `utils.ValidateCompose` |
| `corgi_plan` | `{composePath?, profile?}` | dry-run plan (`order`, `databases`, `services`, `warnings`) | `computeDryRunPlan` |
| `corgi_status` | `{composePath?, service?, unhealthyOnly?}` | `[{label, port, kind, url, healthy, detail}]` - `service` picks one target, `unhealthyOnly` drops the healthy ones; probe results are reused for 1s | `collectStatusRows` + `probeAll` |
| `corgi_env` | `{composePath?, service?, key?}` | `{service: {KEY: {value, source}}}` - `service` = one service uncapped, `key` = one var across services; with neither, each service is capped at 40 vars plus a `_truncated` marker entry | `utils.ResolveAllEnv` |
| `corgi_ps` | `{composePath?}` | `[{name, kind, port, status, url, startedAt}]` - `status` is process/container state, not health | `buildPsRows` |
| `corgi_up` | `{composePath?, profile?, omit?, seed?, serviceBranch?, serviceDir?}` | `{status, handle{pid, logPath}, next, state?, error?}` - `state` is the run-state (`services[]`, `dbServices[]`) once `status` is `started`; `starting` after ~20 s of boot, `failed` with the log tail - **always detached** | child `corgi run --detach` |
| `corgi_down` | `{composePath?}` | `{stopped[], failed[]}` | stop machinery (`stopProcessGroup`) |
| `corgi_logs` | `{composePath?, service, lines?, grep?, since?, errorsOnly?}` | `{service, lines[], truncated}` - filters run before the tail (`grep` regexp or literal, `since` = `10m` or RFC3339, `errorsOnly` = the `corgi logs --json` level heuristic) | newest captured log run, same matcher as `corgi logs --grep/--since` |
| `corgi_exec` | `{composePath?, service, command, ensureDeps?, serviceBranch?, serviceDir?}` | `{exitCode, output, truncated, durationMs}` | `RunServiceCommandExitCode` (output captured) |
| `corgi_test` | `{composePath?, service?, profile?, ensureDeps?, changed?, base?, e2e?, serviceBranch?, serviceDir?}` | `{services[], passed, note?}` - `changed` keeps only repos that differ from `base` (default `main`), `e2e` runs the compose `e2e:` block with output captured in `message` | `runTests` / e2e suite (does not start db/services) |
| `corgi_doctor` | `{composePath?}` | `{ok, checks[]}` | `buildDoctorResult` (required tools, Docker, ports) |
| `corgi_restart` | `{composePath?, profile?}` | same shape as `corgi_up` - **always detached** | `corgi_down` then `corgi_up` |
| `corgi_db_query` | `{composePath?, service, query}` | `{service, output, truncated}` | `utils.ExecDBQueryCapture` (non-interactive) |
| `corgi_db_snapshot` | `{composePath?, service?, name?, force?}` | `{service, name, archive, sizeBytes, pgVersionMajor, image, arch}` | `utils.RunSnapshot` - postgres-family only, same as `corgi db snapshot` |
| `corgi_db_restore` | `{composePath?, name, service?, force?}` | `{service, archive}` - **wipes the data volume**, no prompt | `utils.RunRestore`, same as `corgi db restore --yes` |
| `corgi_schema` | `{}` | the JSON Schema (draft-07) as text | `utils.ComposeJSONSchema` |
| `corgi_context` | `{composePath?, noGit?}` | topology + status + per-repo git state + tier/profiles + validation | `buildContextReport` |
| `corgi_why` | `{composePath?, service, logLines?}` | `{verdict, detail, dependencies[], port, lastExitCode, env, logTail[], nextStep}` | `diagnoseService` |
| `corgi_wait_for_log` | `{composePath?, service, pattern, timeoutSec?}` | `{matched, line, waitedMs}` - blocks | `utils.WaitForLogLine` |
| `corgi_checkout` | `{composePath?, branch?, allowDirty?}` | `[{name, branch, status, usedDefaultBranch, message}]` | `utils.CheckoutRepo` |
| `corgi_checkpoint` | `{composePath?, name?}` | `{name, createdAt, repos[]}` | `utils.CaptureWorkTree` |
| `corgi_restore` | `{composePath?, name}` | `{checkpoint, safetyCheckpoint, restored[], failed[]}` | `utils.RestoreWorkTree` |

`corgi_context` is the call to make first in a workspace: it answers "where am I"
without a round of `corgi_ps` + `corgi_status` + `corgi_validate` + git. `corgi_why`
is the one to make when a service is not up - it returns a `verdict` to branch on
rather than prose. `corgi_wait_for_log` blocks on purpose: use it instead of polling
`corgi_logs`. `corgi_checkpoint` / `corgi_restore` make a cross-repo change
reversible; the restore captures whatever is dirty first and names that safety
checkpoint in its result.

`corgi_db_snapshot` before a mutating `corgi_db_query` makes it reversible with
`corgi_db_restore`. Both keep the CLI's rule and answer `E_ALREADY_RUNNING`
while a detached run is supervising service processes (the snapshot stops the
db container for a moment); a stack with only its databases up is fine.

The cheap way to read a stack: `corgi_status` with `unhealthyOnly`, `corgi_logs`
with `errorsOnly` or `grep`, `corgi_env` with `service` or `key`, `corgi_test`
with `changed`. Each call reuses the parsed compose (invalidated when
`corgi-compose.yml` or its sibling `.env` changes on disk) and the last status
sweep for one second, so polling costs one probe pass per second at most.

`corgi_up` is **always detached**: it brings databases up, generates env, then
spawns each service as a detached process group and writes
`.corgi/corgi_services/.state.json`, returning immediately. Use `corgi_down` to stop.

`serviceBranch` / `serviceDir` (on `corgi_up` / `corgi_exec` / `corgi_test`) run
service(s) from a git branch (isolated reused worktree, non-destructive) or an
existing dir, without editing `path:` in `corgi-compose.yml`. Format
`"svc=branch[,svc2=branch2]"` / `"svc=/path"`.

## Resources

| URI | Content |
|-----|---------|
| `corgi://schema` | JSON Schema (draft-07) for `corgi-compose.yml` (static) |
| `corgi://drivers` | JSON array of supported `db_services.driver` values (`utils.KnownDrivers`) |
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

## Pairing a device

A phone should never hold the server's bearer token. That token reaches
`corgi_exec` and `corgi_db_query`, so a QR containing it is a credential for the
whole machine - anyone who sees the screen or a photo of it has it, and there is
no way to revoke one device without re-pairing every other.

Instead, open a pairing window:

```bash
corgi mcp --http 127.0.0.1:8765 --pair

  pairing code: 7KQ2M9XVBT
  valid for 2m0s, single use
  POST http://127.0.0.1:8765/pair  {"code":"7KQ2M9XVBT","device":"my-phone"}
```

The client posts the code once and receives its own token:

```json
{"token":"corgi_dev_…","daemon":"macbook","device":"my-phone","version":"1.20.25"}
```

That token then works as a normal `Authorization: Bearer` credential against
`/mcp`, alongside the server token.

Properties worth knowing:

- **Single use.** A correct code closes the window, so a code seen in transit
  cannot be replayed.
- **Two minutes.** A code left on a screen is not a standing invitation.
- **Attempt-capped.** Ten wrong codes close the window; the correct one is
  refused after that too.
- **Per-device and revocable.** This is the property a shared token cannot give
  you.
- **Stored hashed.** `devices.json` is `chmod 600` and holds SHA-256 hashes, so a
  readable store lists which devices exist but yields no working credential.
- **`/pair` only exists while the window is open.** Without `--pair` the route
  is not mounted at all.

Managing devices:

```bash
corgi mcp devices                 # who is paired
corgi mcp devices revoke my-phone # kill exactly one, others keep working
```

Treat a lost phone as a compromised machine and revoke it. There is deliberately
no "last seen" tracking: recording it would mean rewriting the device store on
every request, and a concurrent revoke could then be undone by a write that
loaded before it - resurrecting the device you just revoked.
