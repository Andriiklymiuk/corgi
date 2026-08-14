# corgi-remote — a mobile control surface

A design for a phone app that talks to corgi. Nothing is built yet; this
records the shape and, more importantly, the things not to build.

Prerequisite reading: [agent mode](agent.md), and [the MCP server](mcp.md).

## What this is not

The obvious app — streamed transcript, approval cards, session list, push
notifications for gates — **should not be built.** Claude Code's
[Remote Control](https://code.claude.com/docs/en/remote-control) ships all of
that, on both platforms, maintained by Anthropic. A second one would be a worse
version of a shipped product, going stale on every release.

What is left is the half Remote Control has no concept of:

| | Claude app | corgi-remote |
|---|---|---|
| conversation, approvals, push | **yes** | never |
| is the agent daemon up, under which account | no | **yes** |
| which stacks exist, on which machine | no | **yes** |
| stack health, ports, start/stop, logs | no | **yes** |
| **cross-repo diff** | no | **yes** |
| live preview: URL, state, freeze, TTL | no | **yes** |

Two apps side by side: Claude for the conversation, corgi-remote for the
machine. That division is what keeps this small enough to finish.

**If only one screen ever gets built, build the diff viewer.** It is the thing
you would actually open, it works on bad signal, and nothing else renders a
cross-repo diff.

## Architecture: the app is an MCP client

corgi already speaks a protocol the app can use. Do not invent a second one.

```
  corgi-remote (Expo)
        │  Streamable HTTP + Bearer token
        ▼
  POST https://<host>/mcp        ← corgi mcp --http [--tunnel]
        │
        ├─► corgi_agent_status        is the daemon up, which account
        ├─► corgi_workspaces          what stacks exist
        ├─► corgi_status / _ps        service health, ports
        ├─► corgi_up / _down          start and stop the stack
        ├─► corgi_logs                tail a service
        ├─► corgi_diff                cross-repo change  ← the payoff screen
        └─► corgi_preview_*           URL, state, freeze, teardown
```

Three consequences worth stating:

1. **No new server code in corgi.** Every screen is a tool call that exists.
2. **The app is a thin renderer.** If a screen needs logic corgi does not
   expose, add a corgi tool — not app code. The logic then stays testable in Go
   and shared with every other MCP client.
3. **`tools/list` is the capability handshake.** An older daemon lists fewer
   tools and the app hides those screens. No bespoke protocol versioning.

Use `@modelcontextprotocol/sdk` with a Streamable HTTP transport behind one
typed wrapper module. Hand-write the types from the Go structs — they are small
and change rarely; generating them is more machinery than it saves.

## Pairing

**A phone must never hold the server's bearer token.** That token reaches
`corgi_exec` and `corgi_db_query`, so a QR containing it is a credential for the
whole machine, and there is no way to revoke one device without re-pairing every
other.

corgi implements this already — see [pairing](mcp.md#pairing-a-device):

```bash
corgi mcp --http 127.0.0.1:8765 --pair
```

prints a single-use code, valid two minutes, attempt-capped. The client posts it
once to `/pair` and receives its own token, revocable with
`corgi mcp devices revoke <name>`.

The app stores that token in **`expo-secure-store`** (Keychain / Keystore),
never `AsyncStorage`. And it must **refuse to connect to an endpoint with no
bearer token at all**, rather than working and leaving an unauthenticated shell
endpoint reachable — working insecurely is worse than not working.

## Reachability

| | how | notes |
|---|---|---|
| same network | `http://<lan-ip>:8765/mcp` | fastest, nothing third-party in the path |
| named tunnel | `https://corgi.example/mcp` | stable URL, survives restarts |
| quick tunnel | `https://<random>.trycloudflare.com/mcp` | instant, but **the URL changes on every restart**, so pairing must be redone |

Try LAN first, fall back to the tunnel. **Show connection state honestly** —
*"last seen 4m ago"* beats a green dot that lies.

## Screens

Six. Anything beyond these is scope creep.

1. **Daemons** — one card per paired daemon, from `corgi_agent_status`. Surface
   the **Claude account per workspace**: this is the one place the app can catch
   agent mode's silent wrong-account failure, and it costs one line. Show
   `lastReason` verbatim for a disabled workspace — the daemon already writes
   messages meant for a person.
2. **Workspaces** — health, ports, start/stop. Render `unreachable` as *"drive
   not mounted or folder moved"*, never *"not found"*.
3. **Diff** — the payoff screen; build it first and make it good. Collapsible
   per repo and file, syntax highlighted, `+N/−M` counts. New files (`new: true`)
   need a distinct treatment or they render as one enormous green block.
   `truncated: true` gets an explicit footer; `binary: true` a placeholder, never
   bytes. Fetch with `includePatch: false` first for the shape, then lazily per
   file, so the first render is fast on cellular.
4. **Preview** — webview plus a **state banner** (*building…* / *build error* /
   live), freeze toggle wired to the real state, idle countdown, and a prominent
   teardown. Badge `quickTunnel: true` as impermanent.
5. **Logs** — `corgi_logs` with a service picker and follow toggle. Polled.
6. **Settings** — paired daemons, rename, revoke, re-pair.

## Stack

Expo + TypeScript, `expo-router`, TanStack Query, `@modelcontextprotocol/sdk`,
`expo-secure-store`, `expo-camera` for the pairing QR.

Deliberately absent: Redux, GraphQL, code generation, an offline write queue, a
design system. This app polls JSON and renders it.

**Polling, not push.** There is no push channel, and building one means a server
component, APNs/FCM credentials, and a token lifecycle. Remote Control already
pushes the notifications that matter. Poll every few seconds on a screen that
needs it, back off when idle, stop on background.

## Build order

**D1 — pair and see.** Pairing, daemon list, honest connection state.
**D2 — diff.** *This milestone decides whether the app is worth continuing.*
**D3 — workspaces and control.** Health, ports, start/stop.
**D4 — preview and logs.**
**D5 — background/foreground, error states, provisioning.**

**Decide iOS provisioning before D1.** A free Apple account rebuilds every seven
days, which turns a personal tool into a weekly chore and is how projects like
this quietly die. Commit to the paid account, or accept the rebuild knowingly.

## Security

- Device tokens in the Keychain / Keystore. Never `AsyncStorage`, never a log.
- Refuse endpoints with no token.
- **Every tool the app can call, an attacker holding that token can call** —
  including `corgi_exec`. That is why pairing is per-device and revocable. Treat
  a lost phone as a compromised machine: revocation is one tap in Settings and
  one command on the laptop.
- The app never holds Claude credentials. It talks to corgi; corgi talks to
  Claude. There is no path from the phone to an API key.
- No third-party analytics or crash reporting. corgi ships no telemetry and its
  companion should not be where that changes.
- The diff screen shows source and the preview shows a running app, so consider
  excluding the app from the iOS app-switcher snapshot if pointing it at
  someone else's code.

## Non-goals

A conversation UI · editing code · terminal access · multi-user · offline
writes · Android parity in v1.

## Open questions

1. **Is D2 enough on its own?** Plausibly the diff viewer is used for a month
   and D3–D4 are never wanted. Ship D1+D2 and find out before committing.
2. **Does mDNS discovery earn its complexity?** Typing a LAN address once is not
   much worse.
3. **Is a webview preview better than just opening Safari?** A tab has no state
   banner and no freeze — but no app to maintain either. Try the tab first.
4. **Should the app ever call `corgi_exec`?** It would make debugging from a
   phone possible and turn a lost phone into a remote shell. Currently no, and
   the bar for changing that should be high.
