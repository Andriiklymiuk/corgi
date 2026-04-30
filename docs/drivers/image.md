# image driver

Generic Docker-image driver. Use for any service that ships as a public image — gotenberg (PDF conversion), mailhog (SMTP catcher), jaeger (tracing), meilisearch (search), redis-commander, and so on.

Lives under `db_services:` because corgi treats it as infra (declared, lifecycle-managed, env-emitting), not a code repo.

## Quick start

```yaml
db_services:
  gotenberg:
    driver: image
    image: gotenberg/gotenberg:8
    port: 3100              # host bind port
    containerPort: 3000     # container's internal port (defaults to `port:`)
    healthCheck: /health
```

`corgi run` boots the container, `corgi status` HTTP-probes `/health`, `corgi clean -i db` brings it down.

## Emitted env

Default prefix = uppercased `ServiceName` with `-` → `_`:

```
GOTENBERG_URL=http://localhost:3100
GOTENBERG_HOST=localhost
GOTENBERG_PORT=3100
```

Consumers override via `envAlias:` on their `depends_on_db` entry:

```yaml
services:
  api:
    depends_on_db:
      - name: gotenberg
        envAlias: PDF_SERVICE   # → PDF_SERVICE_URL=http://localhost:3100
```

`envAlias: none` collapses the prefix entirely (just `URL=`, `HOST=`, `PORT=`) — usually not what you want for `image` because emitted vars would clash with other drivers.

## Fields

### `image: string`

Docker image reference. Tag explicit (don't use `latest` for reproducibility).

### `port: int`

Host port to bind. Required if you want emitted env vars and TCP healthcheck.

### `containerPort: int`

Internal port inside the container. Defaults to `port:` if unset. Used in docker-compose `<port>:<containerPort>` mapping. Common case: gotenberg listens on 3000 inside, you bind 3100 outside.

### `environment: []string`

Docker-compose `environment:` list, passed verbatim. Use for image-specific config.

```yaml
db_services:
  meilisearch:
    driver: image
    image: getmeili/meilisearch:v1.10
    port: 7700
    environment:
      - MEILI_MASTER_KEY=local-master-key
      - MEILI_NO_ANALYTICS=true
      - MEILI_LOG_LEVEL=INFO
```

### `volumes: []string`

Docker-compose `volumes:` list, passed verbatim. Required for stateful images that need persistence across `corgi clean -i db`.

```yaml
db_services:
  meilisearch:
    driver: image
    image: getmeili/meilisearch:v1.10
    port: 7700
    volumes:
      - ./meili_data:/meili_data
```

Bind mounts (`./local:/inside`) and named volumes (`my-vol:/inside`) both supported. Named volumes need a top-level `volumes:` block in docker-compose, which corgi doesn't auto-emit yet — bind mounts only for now.

### `command: []string`

Override the container's entrypoint args. Renders as docker-compose `command:` array.

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

### `healthCheck: /path`

`corgi status` HTTP-probes this path. Accept any non-5xx as healthy. If unset, corgi falls back to TCP connect on `port:`.

## Lifecycle

| corgi action | Maps to |
| --- | --- |
| `corgi init` | Emit `corgi_services/db_services/<service>/{Makefile,docker-compose.yml}` |
| `corgi run` (driver `up`) | `docker compose up -d` |
| `corgi clean -i db` (driver `down`) | `docker compose down --volumes` |
| `corgi status` | HTTP probe `healthCheck:` (or TCP `port:`) |

`corgi <db-service> id` returns the container ID. `corgi <db-service> logs` tails docker logs.

## When NOT to use the image driver

- **Service ships from your own repo with a Dockerfile** — use `services:` with `runner: { name: docker }` instead.
- **Service is a database with first-class corgi support** (postgres, redis, mysql, etc.) — use the dedicated driver. You get DB-specific env emission (`DB_HOST`, `DB_PORT`, etc.) and seeding helpers (`seedFromFilePath:`, `seedFromDb:`).
- **Service has corgi-emitted bootstrap logic** (supabase, localstack) — use those drivers; they handle config files, JWT signing, queue creation, etc.

The `image` driver is for the long tail of "useful third-party stateless tools" where corgi has no bespoke driver and probably never will.

## Limitations (today)

- No `depends_on:` between db_services (corgi has no inter-service ordering for db_services yet).
- No `restart:` policy override (hardcoded `unless-stopped`).
- No `networks:` override (uses corgi's default `corgi-network` bridge).
- No top-level named-volume declaration — only bind mounts work without manual docker-compose edits.

These can be added when the first user needs them.
