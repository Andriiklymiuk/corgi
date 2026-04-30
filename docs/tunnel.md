# corgi tunnel

Opens public HTTPS tunnels to declared services. One subprocess per target, URLs printed as they come up. Ctrl+C closes all.

Common use case: testing webhook integrations (DocuSeal, Stripe, GitHub apps) against your local stack without configuring DNS / VPN.

## Usage

```bash
corgi tunnel                       # tunnel every services.<name> in compose with port: set
corgi tunnel api                   # only `api`
corgi tunnel api,api-2             # csv list
corgi tunnel --port 3030           # raw port, skip compose lookup
corgi tunnel --provider ngrok      # switch provider
```

Default provider is `cloudflared` (Cloudflare Quick Tunnels — free, no signup).

## Output

```
🌐 Tunnels (cloudflared) — Ctrl+C to stop

  api                            :3030  → starting...
  web                            :3010  → starting...
  admin                          :3002  → starting...

  ✓ api                          :3030  → https://kind-zebra-42.trycloudflare.com
    DocuSeal webhook: https://kind-zebra-42.trycloudflare.com/webhooks/docuseal
  ✓ web                          :3010  → https://small-fox-99.trycloudflare.com
  ✓ admin                        :3002  → https://big-owl-7.trycloudflare.com
```

When `api` is among the targets, corgi auto-prints the DocuSeal webhook path as a hint.

## Providers

| Provider | Auth required | URLs | Install |
|----------|---------------|------|---------|
| `cloudflared` (default) | None for Quick Tunnels | `*.trycloudflare.com`, rotate per restart | `brew install cloudflared` |
| `ngrok` | Yes — free authtoken | `*.ngrok-free.app` etc., rotate per restart | `brew install ngrok/ngrok/ngrok` |
| `localtunnel` | None | `*.loca.lt`, rotate per restart | `npm install -g localtunnel` |

Auth-needing providers are detected before any tunnel spawns:

```
✗ ngrok authentication required:

ngrok authtoken not configured.

Get a free token from https://dashboard.ngrok.com/get-started/your-authtoken
then run:

    ngrok config add-authtoken <YOUR_TOKEN>

(Free tier is fine. No paid plan needed for local webhook testing.)
```

corgi exits without partial state, you run the printed command, then retry.

## Stable URLs

Quick Tunnels rotate. For URLs that survive restarts:

- **Cloudflare Named Tunnels** — `cloudflared tunnel login` once, then `cloudflared tunnel create`. Stable subdomain on a domain you control. Free up to high traffic. Not yet wrapped in `corgi tunnel` (use the CLI directly).
- **ngrok paid tier** — reserved domains. Paid feature.

## Limitations of Cloudflare Quick Tunnels

Worth knowing before relying on them for anything but ephemeral testing:

- **No SSE.** Server-Sent Events get buffered/cut. WebSockets are fine.
- **5MB request body cap.**
- **200 concurrent connection cap.**
- **No IPv6 origin.**
- **Subject to anti-abuse limits.** Don't run sustained load through Quick Tunnels — use a Named Tunnel.

DocuSeal webhooks are small POSTs, so Quick Tunnels handle them comfortably.

Reference: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/#limitations

## Adding a new provider

1. Implement [`tunnel.Provider`](../utils/tunnel/provider.go) in `utils/tunnel/<name>.go`. Five methods: `Name`, `Cmd`, `ExtractURL`, `InstallHint`, `AcceptsStdin`, `PreflightAuth`.
2. Register in `tunnel.Providers` map ([provider.go](../utils/tunnel/provider.go)).
3. Add a row to the table above.

`runner.go` handles subprocess + goroutine + URL streaming generically. New providers don't touch lifecycle code.

## Credits

- [cloudflared](https://github.com/cloudflare/cloudflared) by Cloudflare ([Apache 2.0](https://github.com/cloudflare/cloudflared/blob/master/LICENSE)). Quick Tunnels are an extraordinarily generous free service — thanks for shipping it open.
- [ngrok](https://ngrok.com) — closed source but a long-running staple of this niche.
- [localtunnel](https://github.com/localtunnel/localtunnel) ([MIT](https://github.com/localtunnel/localtunnel/blob/master/LICENSE)) — minimal, no-account fallback.
