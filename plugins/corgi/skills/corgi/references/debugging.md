---
name: debugging
description: Common corgi failures and how to fix them — port conflicts, missing tools, docker down, clone failures, seed failures, healthcheck 5xx. Read when corgi doctor, corgi run, or corgi status reports an error.
---

# Debugging corgi failures

Corgi's output is colored plain text, not JSON. Look for `❌` markers and the label after them.

## `corgi doctor` failures

### `❌ <tool> is missing`

A tool in `required:` failed its `checkCmd`. The `install:` commands for that tool are shown; offer to run them, or let the user do so. Don't retry `corgi doctor` without installing first.

### `❌ Docker daemon is not running`

- macOS: start Docker Desktop, OrbStack, or Colima (whichever the user has).
- Check `--dockerContext` if the user is on a non-default context.
- Don't try to fix this inside corgi — it's an environment problem.

### `❌ <port> busy — needed for <service> — held by: <pid>/<name>`

Three options in order of preference:
1. Stop the offending process (if it's leftover: `corgi clean -i db`, or `kill <pid>` for something unrelated).
2. Change the port in `corgi-compose.yml` for this service/db.
3. If it's a previous corgi run still alive, kill it.

Never silently edit the user's compose to change a port — confirm first, because it can break `.env` values baked into cloned repos.

## `corgi run` failures

### "couldn't find corgi-compose.yml"

User is in the wrong directory, or the file is named non-default. Options:
- `cd` to the repo root.
- Use `-f <path>` to point at a custom filename.
- If this is a new project, scaffold one: see `/corgi-new`.

### "failed to clone <repo>"

- Private repo: pass `--privateToken <token>` or make sure SSH keys are set up.
- Branch doesn't exist: check `services.<name>.branch`.
- Wrong URL: verify `cloneFrom:`.

### Service crashes immediately

Corgi streams the service's own error. Read the lines above the crash for the actual stack trace — don't assume corgi is at fault. Usually it's one of:
- Missing env var (service's code requires something that `depends_on_db` / `depends_on_services` / `environment` didn't supply).
- Port already bound inside the service (e.g. `nodemon` already running).
- DB not ready yet and service didn't wait — solution: add `healthCheck:` to the db (not always supported per driver) or a retry loop in the service.

If the crash output already scrolled off the terminal, restart with `corgi run --logs` to persist each service's stdout/stderr under `corgi_services/.logs/<name>/<timestamp>.log`. Read back with `corgi logs` (interactive picker) or `corgi logs --service <name>`. Logs are capped 50 MB per run and rotated (10 newest kept), so leaving `--logs` on for development is cheap.

If a service crashed unattended, enable desktop notifications once with `corgi doctor` — subsequent runs fire a system toast whenever a service exits non-zero (suppressed during corgi's own shutdown so Ctrl-C never alerts).

### "env file exists but is empty" / wrong values

Corgi regenerates `.env` at every run from `depends_on_db` + `depends_on_services` + `environment`. If a cloned repo has its own `.env` with values corgi is overwriting, set `ignore_env: true` on that service, or use `copyEnvFromFilePath:` to feed a template.

## `corgi status` failures

### `❌ services.<x> http://…/health [HTTP 5xx]`

Service is bound to the port but its handler errored. Check the service's own logs in the `corgi run` pane. Corgi didn't cause this.

### `❌ services.<x> localhost:<port> not listening`

Service never bound the port. Either it crashed (see `corgi run` logs) or it's listening on a different port than `services.<name>.port:` declares.

### `❌ services.<x> http://…/health [HTTP 404]`

The `healthCheck:` path is wrong. 404 is treated as unhealthy in corgi. Either drop `healthCheck:` (falls back to TCP probe) or point it at a real route.

### localstack ❌ right after `corgi run`

Localstack boots its individual AWS services asynchronously. Wait ~5-10s and re-probe. If still down, run `docker logs` on the localstack container.

### supabase ❌ or hangs on `up`

- **First run takes minutes** — supabase pulls 10+ container images. Watch terminal for `[+] Pulling N/M`. Don't kill it.
- **`InvalidRequestException`** from `supabase status -o env` — cli too old. `brew upgrade supabase`.
- **Port 54321/54322/etc. taken** — old supabase project still running. `supabase stop --no-backup` from any project dir kills it globally.
- **Auth users not seeded** — bootstrap.sh logs `auth users:` timing. Missing? `SERVICE_ROLE_KEY` empty in `supabase status`. Try `supabase status` standalone to confirm stack is healthy.
- **Bucket creation 409** — already exists, idempotent skip. Not an error.
- **Custom JWT secret in config.toml but not in compose** → corgi-emitted ANON/SERVICE_ROLE keys won't match. Mirror the secret as `jwtSecret:` in compose.

## Seed failures

Seeding is only attempted when `--seed` / `-s` is passed to `corgi run`.

- `seedFromFilePath`: the file must exist relative to the compose file's dir. Check path.
- `seedFromDb` / `seedFromDbEnvPath`: corgi connects to the source DB via the provided creds and dumps it live. If this fails, the source DB is usually unreachable (VPN, firewall) — not a corgi bug.
- Post-seed the target DB is left populated. Re-running `corgi run -s` will re-seed and typically overwrite.

## `corgi tunnel` failures

### `<provider> not found on PATH. Install: …`

The tunnel binary is missing. Run the install command corgi prints (e.g. `brew install cloudflared`).

### `<provider> authentication required: …`

Provider needs login. corgi prints the exact command to run (e.g. `ngrok config add-authtoken <TOKEN>`). Run it once, then retry `corgi tunnel`. cloudflared (Quick Tunnels mode) and localtunnel never need auth.

### Tunnel comes up but webhook calls fail

- **Cloudflare Quick Tunnel + SSE endpoint** — Quick Tunnels don't support Server-Sent Events. Use ngrok or localtunnel for SSE flows.
- **5MB POST cap exceeded** — Quick Tunnel limit. Use a Named Tunnel (or ngrok) for larger payloads.
- **Webhook URL stale after restart** — Quick Tunnel URLs rotate per tunnel restart. Re-paste into the webhook console (Stripe/GitHub/e-sign/etc.) or set up a Named Tunnel.

### Compose `tunnel:` block silently ignored (Quick mode runs instead of named)

If `corgi tunnel <svc>` falls back to Quick mode despite a `tunnel:` block being declared:

- Confirm binary version supports the field — `corgi --version` and check the changelog for tunnel support. Older builds parse but drop the `tunnel:` block during runtime Service struct construction.
- Confirm the env var resolves. Hostname `${VAR}` is read from shell env → `<service-dir>/.env` → `env/source/<svc>.env`. If unset everywhere, expect a strict error (`env vars not set for tunnel.hostname: …`), not a silent Quick fallback.
- If error path missing too, suspect stale binary: rebuild from source and re-test before debugging further.

See `../../../docs/tunnel.md` for full provider matrix + Quick Tunnel limitations.

## "It was working yesterday" recipes

- `corgi clean -i corgi_services` — regenerates all the docker-compose/Makefile artifacts from templates. Safe, non-destructive to cloned service repos.
- `corgi pull` — pulls latest in every service dir. Does not touch corgi itself.
- `corgi upgrade` — upgrade corgi binary via Homebrew.

## When you've tried everything

- `corgi run --describe` — prints a parsed summary of the compose file. Useful to confirm corgi sees what you think it sees.
- `corgi run --fromScratch` — wipes `corgi_services/` and rebuilds. Heavyweight but fixes drift between template and generated files.
- Check the GitHub issues: https://github.com/Andriiklymiuk/corgi/issues
