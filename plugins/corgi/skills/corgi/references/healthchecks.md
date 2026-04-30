---
name: healthchecks
description: How corgi's healthCheck key works — TCP vs HTTP probes, defaults per driver, exit behavior. Read when adding healthCheck to a service or db, or interpreting `corgi status` output.
---

# Health checks

Health checking runs through the `corgi status` command (aliases: `health`, `healthcheck`). It's a separate, synchronous pass — `corgi run` does not wait for services to become healthy before reporting "running."

## How probing works

For every `services.<name>` and `db_services.<name>` that declares a `port:`, `corgi status` probes:

1. **No `healthCheck:` key** → TCP connect to `localhost:<port>`. Healthy if the connect succeeds.
2. **`healthCheck: /some/path`** → HTTP GET `http://localhost:<port><path>`. Healthy on any non-5xx response. 2xx, 3xx, 4xx all pass.
3. **`driver: localstack` with no override** → HTTP GET `/_localstack/health`. Override by setting `healthCheck:` explicitly.
4. **`driver: supabase`** → set `healthCheck: /rest/v1/`. Returns 401 (kong rejects unauth requests), corgi accepts non-5xx as healthy. Don't omit it — TCP probe on :54321 races kong startup.

## Syntax

```yaml
services:
  api:
    port: 3000
    healthCheck: /health        # path only, no scheme or host

db_services:
  app-db:
    driver: postgres
    port: 5432
    # no healthCheck → TCP probe on 5432
```

## Exit codes

- `corgi status` exits **0** if every probed target is healthy.
- Exits **1** if any single target fails — a worker with port 3001 down will fail the whole command even if everything else is fine.

## Watch + ready modes

- `corgi status -w` (alias `--watch`) loops, prints transitions only (kubectl-style). Tunable via `-i, --interval` (default 2s). Ctrl+C exits.
- `corgi status -r` (aliases `--ready`, `--until-healthy`) loops until every target is up — exits 0 when all green, exits 1 on `--timeout` (default 5m). CI-friendly block-until-ready gate.
- `--service api,broker-portal` narrows the probed set. Use with `--ready` when iterating on one service.
- `--json` switches output: one-shot JSON array; watch/ready emit NDJSON one-per-transition.
- `-q, --quiet` suppresses output; rely on exit code only.

Output is colored text to stdout (no stderr, no JSON unless `--json`):

```
🩺 corgi status
  ✅ db_services.app-db          localhost:5432 listening
  ✅ services.api                http://localhost:3000/health [HTTP 200]
  ❌ services.worker             http://localhost:3001/health [HTTP 503]
1 down, 2 up — check `corgi run` logs for the failing services.
```

## Why a healthCheck is failing (fast triage)

- `HTTP 5xx` → the service is up but its handler is erroring. Check logs from `corgi run`; don't blame corgi.
- `not listening` → the service never bound the port. Either it crashed during boot, or it's listening on a different port than `services.<name>.port` declares.
- `http: no route` → path doesn't exist. 4xx is considered healthy, but a 404 on your `/health` route means you haven't implemented it. Point `healthCheck:` at a real route or drop it to fall back to TCP.
- `localstack` shows ❌ but container is up → localstack services boot async; give it a few seconds after `corgi run` before probing.

## When to add healthCheck

- **Add it for services** where "port open" isn't enough — e.g. a Rails server binds the port long before migrations finish.
- **Skip it for databases** unless you have a specific readiness endpoint. TCP probing is usually sufficient for postgres/mysql/redis/etc.
- **Always set it on localstack** if you've overridden the default port or disabled the health service, to avoid probing a non-existent endpoint.
