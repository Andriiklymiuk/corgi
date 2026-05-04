---
name: db-drivers
description: Full list of supported corgi db_services drivers with default ports, env var prefixes, container images, and driver-specific keys. Read when choosing or configuring a database in corgi-compose.yml.
---

# `db_services` drivers

Set the driver with `driver: <name>`. Corgi generates a `docker-compose.yml` and `Makefile` for each db under `corgi_services/db_services/<name>/`.

## Driver table

| Driver | Default port | Env prefix | Image | Notes |
|---|---|---|---|---|
| `postgres` | 5432 | `DB_` | `postgres:latest` | |
| `pgvector` | 5432 | `DB_` | `pgvector/pgvector:latest` | Postgres + pgvector extension. Same env prefix as postgres. |
| `postgis` | 5432 | `POSTGIS_` | `postgis/postgis:latest` | Postgres + GIS extension |
| `timescaledb` | 5432 | `TIMESCALE_DB_` | `timescale/timescaledb:latest` | Postgres-based |
| `mongodb` | 27017 | `MONGO_` | `mongo:latest` | |
| `mysql` | 3306 | `MYSQL_` | `mysql:latest` | |
| `mariadb` | 3306 | `MARIADB_` | `mariadb:latest` | |
| `mssql` | 1433 | `MSSQL_` | `mcr.microsoft.com/mssql/server:latest` | Password must be >= 8 chars |
| `cockroachdb` | 26257 | `COCKROACH_` | `cockroachdb/cockroach:latest` | |
| `clickhouse` | 9000 | `CLICKHOUSE_` | `clickhouse/clickhouse-server:latest` | |
| `cassandra` | 9042 | `CASSANDRA_` | `cassandra:latest` | |
| `scylla` | 9042 | `SCYLLA_` | `scylladb/scylla:latest` | Cassandra-compatible |
| `redis` | 6379 | `REDIS_` | `redis:latest` | Supports `users.acl` |
| `redis-server` | 6379 | `REDIS_` | `redis:latest` | Same as redis |
| `keydb` | 6379 | `KEYDB_` | `eqalpha/keydb:latest` | Redis-compatible |
| `dragonfly` | 6379 | `DRAGONFLY_` | `dragonflydb/dragonfly:latest` | Redis-compatible |
| `redict` | 6379 | `REDICT_` | `redict/redict:latest` | Redis fork |
| `valkey` | 6379 | `VALKEY_` | `valkey/valkey:latest` | Redis fork |
| `rabbitmq` | 5672 | `RABBITMQ_` | `rabbitmq:latest` | Supports `additional.definitionPath` for JSON definitions |
| `kafka` | 9092 | `KAFKA_` | `confluentinc/cp-kafka:latest` | |
| `surrealdb` | 8000 | `SURREALDB_` | `surrealdb/surrealdb:latest` | |
| `influxdb` | 8086 | `INFLUXDB_` | `influxdb:latest` | |
| `neo4j` | 7687 | `NEO4J_` | `neo4j:latest` | Dashboard on `:7474` (set via `port2`) |
| `dgraph` | 8080 | `DGRAPH_` | `dgraph/dgraph:latest` | Dashboard on `:8000` |
| `arangodb` | 8529 | `ARANGO_` | `arangodb:latest` | |
| `elasticsearch` | 9200 | `ELASTIC_` | `docker.elastic.co/elasticsearch/elasticsearch:latest` | Pairs with Kibana on `:5601` |
| `couchdb` | 5984 | `COUCHDB_` | `couchdb:latest` | UI at `:<port>/_utils` |
| `meilisearch` | 7700 | `MEILISEARCH_` | `getmeili/meilisearch:latest` | |
| `faunadb` | 8443 | `FAUNADB_` | `fauna/faunadb:latest` | Password hardcoded to `secret` in template |
| `yugabytedb` | 5433 | `YUGABYTEDB_` | `yugabytedb/yugabyte:latest` | Dashboard on `:15433` |
| `skytable` | 2003 | `SKYTABLE_` | `skytable/skytable:latest` | |
| `dynamodb` | 8000 | `DYNAMODB_` | `amazon/dynamodb-local:latest` | Standalone local emulator |
| `localstack` | 4566 | `AWS_` | `localstack/localstack:latest` | Unified AWS emulator — see below |
| `supabase` | 54321 | `SUPABASE_` | wraps `supabase` CLI | Local auth + storage. Reads ports from `supabase/config.toml`. Seeds `buckets:` + `authUsers:` on `up`. See below + [docs/drivers/supabase.md](../../../../docs/drivers/supabase.md) |
| `image` | (you set) | `<SERVICE>_` | (you set via `image:`) | Generic stateless docker-image driver. For services that ship as a public image with no DB/state (gotenberg, mailhog, jaeger). See below |

## localstack special keys

Prefer `driver: localstack` over standalone `sqs`/`s3` drivers when multiple AWS services are needed — one container covers all.

```yaml
db_services:
  aws:
    driver: localstack
    port: 4566
    services: [sqs, s3, dynamodb]   # default: [sqs, s3]
    queues: [jobs, dead-letter]     # emits AWS_SQS_JOBS + AWS_SQS_JOBS_URL, same for DEAD_LETTER
    buckets: [uploads, thumbnails]  # emits AWS_S3_UPLOADS_BUCKET, AWS_S3_THUMBNAILS_BUCKET
    healthCheck: /_localstack/health
```

`corgi status` uses `/_localstack/health` by default for the localstack driver — you don't need to set `healthCheck` unless overriding.

## supabase special keys

Wraps supabase CLI — corgi runs `supabase init`/`start`/`stop`. Auto-creates Storage buckets via Storage API and auth users via Admin API on `up`. Idempotent.

```yaml
db_services:
  supabase:
    driver: supabase
    healthCheck: /rest/v1/
    port: 54321              # api (kong gateway). Patches [api].port.
    dbPort: 54322            # optional. Patches [db].port.
    studioPort: 54323        # optional. Patches [studio].port.
    inbucketPort: 54324      # optional. Patches [inbucket].port.
    buckets: [user-uploads, public-assets]
    authUsers:
      - email: admin@example.com
        password: password123
        metadata:
          role: admin
    # jwtSecret: my-32-char-secret  # only if you customized auth.jwt_secret in config.toml
    # configTomlPath: ./config/supabase.config.toml  # source of truth — copied to corgi_services/db_services/<svc>/supabase/config.toml on every init
```

Compose ports always win: the Makefile awk-patches `[api/db/studio/inbucket].port` in config.toml before `supabase start`, so emitted env URLs and bind ports stay aligned. Unset yaml ports keep whatever `config.toml` says (stock defaults 54321/54322/54323/54324).

`configTomlPath` controls where the canonical config.toml lives:
- **set** → corgi copies the file to `corgi_services/db_services/<svc>/supabase/config.toml` and runs the CLI from that dir. Edit the source file (e.g. `config/supabase.config.toml`) only — destination is regenerated each init.
- **unset** → legacy behavior. `supabase init` writes `<projectRoot>/supabase/config.toml` on first run. Dev edits live there directly.

Emitted env (with `envAlias: none`): `SUPABASE_URL`, `SUPABASE_ANON_KEY`, `SUPABASE_SERVICE_ROLE_KEY`, `SUPABASE_JWT_SECRET`, `SUPABASE_DB_URL`, `SUPABASE_DB_HOST`, `SUPABASE_DB_PORT`, `SUPABASE_STUDIO_URL`, `SUPABASE_INBUCKET_URL`, `SUPABASE_STORAGE_S3_URL`, `SUPABASE_S3_PROTOCOL_*`, `SUPABASE_BUCKET_<UPPER_NAME>`.

For frontend frameworks use `envAlias: VITE` (→ `VITE_SUPABASE_*`) or `envAlias: EXPO_PUBLIC` (→ `EXPO_PUBLIC_SUPABASE_*`).

Requires supabase CLI on PATH. Add to `required:` block:
```yaml
required:
  supabase:
    why: [Local auth + storage stack]
    checkCmd: supabase --version
    install: [brew install supabase/tap/supabase]
```

Full docs: [docs/drivers/supabase.md](../../../../docs/drivers/supabase.md)

## image driver (generic docker image)

Use for services that ship as a public docker image. Sits inside `db_services:` because corgi treats it as infra (declared, lifecycle-managed, env-emitting), not a code repo.

```yaml
db_services:
  gotenberg:
    driver: image
    image: gotenberg/gotenberg:8
    port: 3100              # host bind
    containerPort: 3000     # container's internal port (default = port)
    healthCheck: /health
```

Emits (default prefix = uppercased service name):
```
GOTENBERG_URL=http://localhost:3100
GOTENBERG_HOST=localhost
GOTENBERG_PORT=3100
```

Override prefix via `envAlias:` on the consumer side:
```yaml
services:
  api:
    depends_on_db:
      - name: gotenberg
        envAlias: PDF_SERVICE   # → PDF_SERVICE_URL=http://localhost:3100
```

### Optional fields

`environment:` — passed verbatim to docker-compose `environment:` list.

```yaml
db_services:
  meilisearch:
    driver: image
    image: getmeili/meilisearch:v1.10
    port: 7700
    environment:
      - MEILI_MASTER_KEY=local-master-key
      - MEILI_NO_ANALYTICS=true
```

`volumes:` — passed verbatim to docker-compose `volumes:` list. Required for stateful images that need persistence across restarts.

```yaml
db_services:
  meilisearch:
    driver: image
    image: getmeili/meilisearch:v1.10
    port: 7700
    volumes:
      - ./meili_data:/meili_data
```

`command:` — override container entrypoint args. Passed as docker-compose `command:` array.

```yaml
db_services:
  jaeger:
    driver: image
    image: jaegertracing/all-in-one:1.55
    port: 16686
    command:
      - --collector.zipkin.host-port=9411
      - --memory.max-traces=10000
```

`up`/`down`/`stop`/`logs`/`id` follow the standard Makefile shape (same as postgres/etc).

## Picking the right driver

- User says **"Redis"** generically → `redis` unless they need a specific fork. `keydb`/`dragonfly`/`valkey` are drop-in replacements with the `REDIS` wire protocol but different env prefixes, so don't change the driver on a running project without updating env usage.
- User says **"AWS SQS" or "AWS S3"** → prefer `localstack` with `queues:` / `buckets:` over the legacy standalone `sqs`/`s3` drivers.
- User says **"Supabase"**, **"local auth"**, **"GoTrue"**, or **"local Storage"** → `supabase`. Don't try to recreate auth/storage manually with separate containers.
- User wants **"vector search on Postgres"** → `pgvector` (not `postgres` + manual extension install).
- User wants **"time-series on Postgres"** → `timescaledb`.
- User wants **"Postgres with geo"** → `postgis`.

## Port collisions to watch for

- 5432: `postgres`, `pgvector`, `postgis`, `timescaledb` — only one can bind per project.
- 6379: `redis`, `keydb`, `dragonfly`, `redict`, `valkey` — same.
- 3306: `mysql`, `mariadb`.
- 9042: `cassandra`, `scylla`.
- 8000: `surrealdb` and `dynamodb` both default here — change one if using both.
- 4566: `localstack`, `sqs`, `s3` all share this; only one at a time.
- 54321..54324: `supabase` driver claims api/db/studio/inbucket here. Override via compose `port:` (api), `dbPort:`, `studioPort:`, `inbucketPort:` — driver patches config.toml + emits matching env URLs.

If two drivers need the same port, change `port:` on one of them. Corgi will substitute it into the generated compose file and env vars.
