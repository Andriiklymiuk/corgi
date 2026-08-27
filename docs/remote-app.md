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
| start/stop a session in any registered workspace | no | **yes** |
| per-workspace session timeline across restarts | no | **yes** |
| session failure push (via `notifyUrl`) | no | **yes** |
| **cross-repo diff** | no | **yes** |
| live preview: URL, state, freeze, TTL | no | **yes** |

The session rows are machine-scoped, which is why they belong here: claude.ai
lists sessions per *account*, corgi's timeline lists them per *workspace on a
machine*, with the daemon's exit reasons attached. The launcher page (`/app`)
already ships that view in a browser; the app is its multi-machine version.

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
        ├─► corgi_session_start/_stop start a session anywhere, end it
        ├─► corgi_session_events      the timeline: starts, exits and why, links
        ├─► corgi_diff                cross-repo change  ← the payoff screen
        └─► corgi_preview_*           URL, state, freeze, teardown
```

The `/launch/*` REST endpoints behind the same bearer token mirror the session
tools for the launcher page. The app uses the MCP tools — one protocol, typed
once — and leaves `/launch/*` to the browser.

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

## Multiple machines

The app's core model is a list of machines, each paired independently:

```
machine = { name, lanUrl?, tunnelUrl, deviceToken }   ← token in secure store
```

- Pairing is per machine (each runs its own `corgi mcp`); adding a laptop is
  scanning one more QR. Revocation stays per machine + per device.
- The home screen aggregates: every workspace from every reachable machine,
  grouped by machine, with per-machine connection state. A machine that is
  asleep renders as *"last seen …"*, its workspaces greyed but listed — the
  registry outlives the connection.
- No cross-machine server, no sync: the phone is the only place the list
  exists, which is exactly the trust model of a remote control.
- Same workspace name on two machines is fine — everything is keyed
  (machine, workspace), never workspace alone.

## Notifications

corgi's daemon can now POST every notification (restarts, failures, remote
starts) to a `notifyUrl` — title in the `Title` header, plain-text body, the
[ntfy](https://ntfy.sh) contract. That gives three tiers, in build order:

1. **None (v1):** the app polls while open. Remote Control keeps pushing the
   conversation-side notifications that matter most.
2. **ntfy app (zero code):** the user sets `notifyUrl` to a private topic and
   subscribes in the ntfy app. Documented in agent.md; nothing for
   corgi-remote to build.
3. **In-app (later, still no server):** the app subscribes to the same topics
   itself over ntfy's SSE/WebSocket API, one subscription per machine, so
   machine alerts land in the same app that can act on them. Self-hosted ntfy
   works unchanged for people who do not want a third party in the path.

Native APNs/FCM push stays a non-goal: it is the tier that needs server
infrastructure, credentials, and a token lifecycle, for latency ntfy already
delivers.

## Screens

Seven. Anything beyond these is scope creep.

0. **Sessions** — per workspace: Start (with profile picker), Stop, and the
   timeline from `corgi_session_events` — each captured link opens the
   conversation in the Claude app, each exit shows its reason. This is the
   launcher page as a native screen, across machines.
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

**Polling for state, ntfy for push.** Poll every few seconds on a screen that
needs it, back off when idle, stop on background. Alerts arrive through the
notifyUrl tiers above — never through a bespoke push server.

## Build order

**D1 — pair and see.** Pairing (multi-machine from day one), daemon list,
honest connection state.
**D2 — diff.** *This milestone decides whether the app is worth continuing.*
**D3 — sessions.** Start/stop + timeline; the launcher page stops being needed.
**D4 — workspaces and control.** Health, ports, start/stop.
**D5 — preview and logs.**
**D6 — in-app ntfy subscriptions, background/foreground, error states,
provisioning.**

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
