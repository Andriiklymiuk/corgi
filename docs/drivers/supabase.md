# supabase driver

Wraps the [supabase CLI](https://supabase.com/docs/guides/local-development) — corgi runs `supabase init`/`start`/`stop` for you and emits the auth + storage env vars consumers need.

## Quick start

```yaml
db_services:
  supabase:
    driver: supabase
    healthCheck: /rest/v1/
    buckets: [user-uploads]
    authUsers:
      - email: admin@example.com
        password: password123
        metadata:
          role: admin
```

`corgi run` will:
1. `supabase init` (if `supabase/config.toml` missing)
2. `supabase start` (if not already running)
3. Create buckets via Storage API
4. Create auth users via Admin API
5. Write live captured keys to `.env-supabase-runtime` for fallback

## Emitted env vars

For `envAlias: none` (no prefix):

| Var | Source |
| --- | --- |
| `SUPABASE_URL` | `http://<host>:<api_port>` |
| `SUPABASE_ANON_KEY` | HS256 JWT signed with `jwtSecret` (or stock default) |
| `SUPABASE_SERVICE_ROLE_KEY` | HS256 JWT signed with `jwtSecret` |
| `SUPABASE_JWT_SECRET` | yaml `jwtSecret:` or stock default |
| `SUPABASE_DB_URL` | full `postgresql://` URL |
| `SUPABASE_DB_HOST`, `SUPABASE_DB_PORT` | components |
| `SUPABASE_STUDIO_URL` | studio web UI |
| `SUPABASE_INBUCKET_URL` | local mail catcher |
| `SUPABASE_STORAGE_S3_URL` | S3-compatible storage endpoint |
| `SUPABASE_S3_PROTOCOL_ACCESS_KEY_ID` | supabase's S3 protocol creds |
| `SUPABASE_S3_PROTOCOL_ACCESS_KEY_SECRET` | (same) |
| `SUPABASE_S3_PROTOCOL_REGION` | `local` |
| `SUPABASE_BUCKET_<UPPER_NAME>` | one per bucket in `buckets:` |

Use `envAlias: VITE` → `VITE_SUPABASE_*`. `envAlias: EXPO_PUBLIC` → `EXPO_PUBLIC_SUPABASE_*`. Etc.

## Fields

### `buckets: []string`

Storage buckets to create via Storage API on `up`. Idempotent (existing buckets skipped). Also emits per-bucket env vars.

```yaml
buckets:
  - user-uploads
  - public-assets
```

For advanced options (mime types, custom paths), declare in `supabase/config.toml`'s `[storage.buckets.<name>]` instead. Mix freely.

### `authUsers: []SupabaseAuthUser`

Auth users seeded via Admin API on `up`. Idempotent (existing users return HTTP 422, skipped).

```yaml
authUsers:
  - email: admin@example.com
    password: password123
    metadata:
      role: admin
      country: FR
```

`metadata` is a yaml map serialized to JSON for `user_metadata`.

### `jwtSecret: string`

Override the stock JWT secret. Must match `auth.jwt_secret` in your `supabase/config.toml`. Driver re-signs ANON / SERVICE_ROLE keys with this so corgi-emitted env matches what `supabase status` reports.

```yaml
jwtSecret: my-32-character-secret-here-pls-rotate
```

Skip for local-only setups — stock secret works.

### `healthCheck: /path`

`corgi status` HTTP probes this path. Use `/rest/v1/` (returns 401, accepted as up).

### `configTomlPath: string`

Optional. Path (relative to corgi-compose.yml; absolute paths also accepted) to a `config.toml` that corgi copies to `<projectRoot>/supabase/config.toml` on every `corgi init`. Useful when:

- You want to check the config into a templates/ dir and treat it as source of truth.
- Fresh clones should boot with your customizations (custom JWT secret, ports, schemas) without anyone running `supabase init` first.

```yaml
db_services:
  supabase:
    driver: supabase
    configTomlPath: ./templates/supabase-config.toml
```

Behavior:

- Always overwrites the destination — matches how other corgi-emitted files (Makefiles, docker-compose.yml) re-emit on each init.
- If the source file is missing, `corgi init` errors for that service.
- If unset, first `corgi up` triggers `supabase init` (existing flow).

## supabase/config.toml location

corgi expects `<corgi-compose dir>/supabase/config.toml` — same convention as supabase CLI. No yaml field to relocate it; supabase CLI itself doesn't support that.

### Missing file

First `corgi run` triggers `supabase init` automatically. File written with stock defaults. Commit it.

```
→ supabase/config.toml missing — running 'supabase init'...
```

### Deleted file

Next `corgi run` recreates via `supabase init`. Customizations lost (JWT secret, ports, mailer, edge function configs). Don't delete unless you want a fresh reset.

### What corgi reads from it

The `[api].port`, `[db].port`, `[studio].port`, `[inbucket].port` values — to emit matching `SUPABASE_*` URLs that align with what supabase actually binds to. Everything else (auth, storage, realtime, edge functions, mailer) is supabase CLI's domain. Compose `port:` overrides `[api].port`; the Makefile patches the file before `supabase start` so both stay aligned.

## Lifecycle

| corgi action | Maps to |
| --- | --- |
| `corgi run` (driver `up`) | `supabase init` if missing → `supabase start` if not running → bootstrap.sh (buckets + users + write `.env-supabase-runtime`) |
| Ctrl+C / `corgi clean -i db` | `supabase stop --no-backup` |
| `corgi status` | HTTP probe of `healthCheck` path |

Bootstrap script logs timing per phase:
```
configuring supabase (api=http://127.0.0.1:54321)
===================
  wrote /path/to/.env-supabase-runtime
  buckets: 1s
  auth users: 2s
✓ supabase bootstrap done in 4s
```

## Required CLI

Add to your compose's `required:` block so `corgi doctor` flags missing CLI:

```yaml
required:
  supabase:
    why:
      - Local auth + storage stack
    checkCmd: supabase --version
    install:
      - brew install supabase/tap/supabase
```

`corgi doctor` (alias `corgi check` / `corgi preflight`) verifies before run. Auto-installs via brew if user accepts the prompt.

## Custom port

`port:` in compose drives `[api].port`. Two paths:

```yaml
db_services:
  supabase:
    driver: supabase
    port: 8000
```

What happens:
1. corgi env emission reads `[api].port` from `config.toml` but overrides with compose `port:` if set — `SUPABASE_URL=http://...:8000`.
2. Makefile `up`: after `supabase init` (if it ran), an awk pass patches `[api].port = 8000` in `supabase/config.toml`. `supabase start` then binds to 8000.
3. Bind port and emitted URL stay aligned even after first init.

Compose `port:` only controls `[api].port`. db/studio/inbucket stay at whatever `config.toml` says (stock 54322/54323/54324). To override those, edit the file directly:

```toml
[api]
port = 8000

[db]
port = 8001

[studio]
port = 8002
```

For full pre-baked control, check the file into a templates dir and use `configTomlPath:`.

## Two-database setup

The supabase driver runs its own postgres on `[db].port` (default `54322`) for `auth.users`, storage metadata, realtime subs. **App data should NOT live there** — keep a separate `db_services.<name>` (e.g. postgres driver) for app schema. Reasons:

- Supabase upgrades manage their own schema; mixing risks rewrites
- Prisma / Knex / etc. own their migrations cleanly
- Matches prod topology (Supabase cloud + AWS RDS)

```yaml
db_services:
  api-db:
    driver: postgres
    databaseName: myapp
    port: 5432
  supabase:
    driver: supabase
    # supabase's own postgres lives on 54322 internally
```

## Live key capture

Bootstrap writes captured live values from `supabase status -o env` to `<project>/.env-supabase-runtime`. Useful as fallback if supabase rotates internal seeds (S3 protocol keys, etc.) and corgi's hardcoded fallbacks drift. Source it manually if needed:

```bash
set -a; . ./.env-supabase-runtime; set +a
```

Most users won't need this — supabase rarely rotates these.
