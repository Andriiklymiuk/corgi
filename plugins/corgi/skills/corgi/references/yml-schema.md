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
buckets:  [string]             # localstack: auto-create S3 buckets; emits AWS_S3_<UPPER>_BUCKET
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
environment:             [string] # Extra env vars (KEY=value)
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
