---
name: yml-schema
description: Full corgi-compose.yml schema — top-level keys, services, db_services, required. Read when authoring or editing a corgi-compose.yml.
---

# `corgi-compose.yml` schema

## Top-level keys

```yaml
name:         string                # Project name (required in practice)
description:  string                # Free-text description
useDocker:    bool                  # Run services via Docker (vs native)
useAwsVpn:    bool                  # Initialize AWS VPN before run
init:         [string]              # Shell commands run on `corgi init`
beforeStart:  [string]              # Shell commands run before any service starts
afterStart:   [string]              # Shell commands run on shutdown (SIGINT/SIGTERM)
db_services:  map<name, DbService>  # See below
services:     map<name, Service>    # See below
required:     map<tool, Required>   # See below
```

## `db_services.<name>`

```yaml
driver:               string   # Required. See references/db-drivers.md for full list
host:                 string   # Default: localhost
port:                 int      # Host port (0 = no exposed port)
port2:                int      # Secondary port (e.g. admin UI for neo4j, dgraph)
user:                 string
password:             string   # For mssql: min 8 chars
databaseName:         string   # Db/schema name
version:              string   # Image tag (default: "latest")
manualRun:            bool     # If true, skip unless --dbServices=<name> passed
healthCheck:          string   # HTTP path for `corgi status` (e.g. /health)

# Seeding (choose one)
seedFromFilePath:     string   # Path to dump file (.sql, .bak, .cql, etc.)
seedFromDbEnvPath:    string   # Path to .env holding seed-source creds
seedFromDb:                    # Inline seed-source creds
  host: string
  port: int
  user: string
  password: string
  databaseName: string

# Driver-specific
additional:
  definitionPath: string       # rabbitmq: path to JSON definitions file
services: [string]             # localstack: AWS services (default: [sqs, s3])
queues:   [string]             # localstack: auto-create SQS queues; emits AWS_SQS_<UPPER>
buckets:  [string]             # localstack/supabase: auto-create buckets
                               #   localstack → AWS_S3_<UPPER>_BUCKET
                               #   supabase   → SUPABASE_BUCKET_<UPPER> (via Storage API)
jwtSecret: string              # supabase: override stock JWT secret; driver re-signs ANON/SERVICE_ROLE keys
authUsers:                     # supabase: seed via Admin API on `up`
  - email:    string
    password: string
    metadata: object           # yaml map serialized to JSON for user_metadata
configTomlPath: string         # supabase: optional path (relative to corgi-compose.yml) to a config.toml that corgi copies to <projectRoot>/supabase/config.toml on every init. If unset, supabase init runs at first `up`.
image: string                  # image: docker image reference (e.g. "gotenberg/gotenberg:8")
containerPort: int             # image: container's internal port. Defaults to `port:` if unset
environment: [string]          # image: docker-compose environment list, e.g. ["MEILI_MASTER_KEY=secret"]
volumes:     [string]          # image: docker-compose volume mounts, e.g. ["./data:/app/data"]
command:     [string]          # image: override container entrypoint args, e.g. ["--collector.zipkin.host-port=9411"]
```

## `services.<name>`

```yaml
path:                    string   # Relative path to service repo (default: cwd)
cloneFrom:               string   # Git URL; used if path missing
branch:                  string   # Git branch to checkout
port:                    int
portAlias:               string   # Env var name for port (default: PORT)
manualRun:               bool
ignore_env:              bool     # Skip .env generation
envPath:                 string   # Where .env lives inside the repo (default: .env)
copyEnvFromFilePath:     string   # Template .env to copy in
localhostNameInEnv:      string   # Default: localhost; becomes host.docker.internal under Docker
environment:             [string] # Extra env vars (KEY=value). Supports ${OWN_VAR} (own env) and ${producer.VAR} (cross-service exports)
autoSourceEnv:           bool     # Default true. False = corgi skips auto-`set -a; . .env; set +a` prefix on commands (avoids leaking secrets to subprocesses)
healthCheck:             string   # HTTP path for `corgi status`
interactiveInput:        bool     # Keep stdin open for start commands

depends_on_db:
  - name:           string        # db_service name
    envAlias:       string        # Prefix: SEED_ => SEED_DB_HOST, SEED_DB_USER, …
    forceUseEnv:    bool

depends_on_services:
  - name:           string
    envAlias:       string        # Env var name for that service's URL
    suffix:         string        # Appended to URL (e.g. /api/v1)
    forceUseEnv:    bool

exports:                 [string]   # Whitelist of vars exported to dependents.
                                    #   "NAME"           re-export own env var (must exist)
                                    #   "NAME=value"     inline literal (${OWN_VAR} expanded)

runner:
  name: string                    # "docker" or custom

beforeStart:  [string]            # Run before `start`
start:        [string]            # Main blocking command(s)
afterStart:   [string]            # Run on exit

scripts:                          # Named scripts invoked via `corgi script -n <name>`
  - name: string
    manualRun: bool
    commands: [string]
    copyEnvFromFilePath: string
```

## `required.<tool>`

```yaml
why:       [string]   # Displayed to the user as reasons
install:   [string]   # Shell commands to install it
optional:  bool       # If true, prompt before installing (default: false)
checkCmd:  string     # Verification command (default: `<tool> -v`)
```

## Minimal valid example

```yaml
name: hello-corgi
description: one service + postgres

required:
  docker:
    why: [container runtime]
    install: [brew install --cask docker]
    checkCmd: docker --version

db_services:
  app-db:
    driver: postgres
    port: 5432
    user: app
    password: app
    databaseName: app

services:
  api:
    path: ./api
    port: 3000
    depends_on_db:
      - name: app-db
        envAlias: ""
    start:
      - npm run dev
```

## Env var generation rules

For each `depends_on_db`:
- Emits `<envAlias>DB_HOST`, `<envAlias>DB_PORT`, `<envAlias>DB_USER`, `<envAlias>DB_PASSWORD`, `<envAlias>DB_NAME` into the service's `.env`.
- Driver-specific prefix (`REDIS_`, `MONGO_`, etc.) is used when `envAlias: ""` for non-postgres drivers. See `references/db-drivers.md`.

For each `depends_on_services`:
- Emits `<envAlias>=http://<localhostNameInEnv>:<port><suffix>`.

`useDocker: true` rewrites `localhost` → `host.docker.internal` in generated `.env` values so containers can reach host ports.

## Exporting and referencing service env vars

A service can declare a whitelist of env vars it exports to dependents:

```yaml
services:
  notifier:
    port: 7000
    copyEnvFromFilePath: .env-notifier   # contains NOTIFICATION_API_TOKEN=xxx
    environment:
      - URL=http://localhost:${PORT}
    exports:
      - NOTIFICATION_API_TOKEN              # re-export own env var
      - URL                                 # re-export own env var
      - HEALTH=http://localhost:${PORT}/healthz   # inline literal
```

Dependents reference these via `${producer.VAR}` inside their own `environment`:

```yaml
services:
  app:
    depends_on_services:
      - name: notifier
    environment:
      - NOTIFICATION_API_TOKEN=${notifier.NOTIFICATION_API_TOKEN}   # shared secret
      - NOTIFIER_URL=${notifier.URL}                                # rename
      - NOTIFIER_PING=${notifier.HEALTH}/extra                      # compose
```

Rules:
- Producer must be listed in consumer's `depends_on_services` to be referenced.
- Only names in `exports` are visible — typos and unexported names error at env generation.
- Cycles in `depends_on_services` graph error.
- Service name in `${producer.VAR}` must match the raw yaml key exactly (case-sensitive, no `-`/`/` normalization, unlike legacy `<NAME>_URL`).
- `exports` entries that reference a missing own-env var error at env generation.
- `manualRun` producers still produce exports (values are static); URL-based exports may point at nothing live.
