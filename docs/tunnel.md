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

## Stable URLs (named mode)

Add a `tunnel:` block to a service in `corgi-compose.yml`:

```yaml
services:
  api:
    port: 3030
    tunnel:
      provider: cloudflared       # cloudflared (default) | ngrok
      hostname: ${API_TUNNEL_HOST} # required, supports ${VAR}
      name: ${USER}-api-dev       # cloudflared only
```

`${VAR}` resolves from shell env first, then the service's env file (`env/source/<svc>.env`). Missing vars produce a strict error — no silent fallback to Quick mode.

CLI override: `corgi tunnel api --provider ngrok` swaps the provider while keeping the same hostname.

### cloudflared one-time setup (per dev)

Free for Cloudflare Zero Trust orgs ≤50 users. Requires a domain in Cloudflare DNS.

```bash
cloudflared tunnel login                                      # browser OAuth
cloudflared tunnel create andrii-api                          # creates tunnel + creds
cloudflared tunnel route dns andrii-api api-andrii.dev.example.com
echo 'export API_TUNNEL_HOST=api-andrii.dev.example.com' >> ~/.zshrc
```

`corgi tunnel api` now hits `https://api-andrii.dev.example.com` every time. corgi preflight checks `~/.cloudflared/cert.pem` + `cloudflared tunnel list` for the named tunnel and aborts with the exact missing command if either fails.

### ngrok one-time setup (per dev)

Free static domain — one per ngrok account, on `*.ngrok-free.app`. No DNS work.

```bash
# 1. Sign up at ngrok.com (free)
# 2. Dashboard → Domains → Claim free static domain → e.g. my-api-andrii.ngrok-free.app
ngrok config add-authtoken <YOUR_TOKEN>
echo 'export API_TUNNEL_HOST=my-api-andrii.ngrok-free.app' >> ~/.zshrc
```

Compose with `provider: ngrok`:
```yaml
tunnel:
  provider: ngrok
  hostname: ${API_TUNNEL_HOST}
  # name: not used for ngrok
```

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
