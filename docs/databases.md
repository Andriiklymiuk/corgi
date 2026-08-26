# Databases & infra drivers

In `services` you can run anything you like. In `db_services`, corgi ships **managed drivers** that handle the container, seeding, a native shell, and env vars for you — so a database is one short block, not a page of Compose.

A couple are whole stacks rather than single containers: `localstack` stands up a fleet of AWS services, and `supabase` brings up auth, storage, and studio — all from the same file.

## Once a database is up

- **Seed it** with real data from a file (`seedFromFilePath:`) or another database (`seedFromDb:`). `corgi run --seed` loads it; the dump format is chosen per driver automatically.
- **Open a shell** with `corgi db shell [name]` — the right tool (`psql`, `mongosh`, `redis-cli`, …) with credentials already filled in. Add `-e '<query>'` to run one query and exit.
- **Manage them** from the `corgi db` menu — bring containers up or down, seed, or dump.
- **Just the databases.** Running a service from your IDE or debugger? `corgi db -u` brings the databases up on their own and leaves the rest to you.

## What gets wired

corgi writes each service's `.env` and sources it before your commands run.

- A `depends_on_db` edge writes the database's connection vars — for Postgres that's `DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_USER`, `DB_PASSWORD`; other drivers use their own prefix (`REDIS_`, `MYSQL_`, `MONGO_`, …), and `envAlias:` renames it (`envAlias: DATABASE` → `DATABASE_HOST`, …).
- A `depends_on_services` edge writes `<SERVICE>_URL` (e.g. `API_URL=http://localhost:7012`).

Run `corgi env <service>` to see the exact, fully-resolved set a service will get, and where each value came from.

## The 38 managed drivers

- [postgres](https://www.postgresql.org), [example](https://github.com/Andriiklymiuk/corgi_examples/tree/main/postgres)
- [mongodb](https://www.mongodb.com), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/mongodb/mongodb-go.corgi-compose.yml)
- [rabbitmq](https://www.rabbitmq.com), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/rabbitmq/rabbitmq-go-nestjs.corgi-compose.yml)
- [sqs](https://docs.localstack.cloud/user-guide/aws/sqs/) — local AWS SQS, [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/aws_sqs/aws_sqs_postgres_go_deno.corgi-compose.yml)
- [s3](https://docs.localstack.cloud/user-guide/aws/s3/) — local AWS S3 buckets
- [redis](https://redis.io), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/redis/redis-bun-expo.corgi-compose.yml)
- [redis-server](https://redis.io)
- [mysql](https://www.mysql.com)
- [mariadb](https://mariadb.org)
- [dynamodb](https://aws.amazon.com/dynamodb/)
- [kafka](https://kafka.apache.org)
- [mssql](https://www.microsoft.com/en-us/sql-server/sql-server-downloads)
- [cassandra](https://cassandra.apache.org/_/index.html)
- [cockroach](https://www.cockroachlabs.com)
- [clickhouse](https://clickhouse.com)
- [scylla](https://www.scylladb.com)
- [keydb](https://docs.keydb.dev)
- [influxdb](https://www.influxdata.com)
- [surrealdb](https://surrealdb.com)
- [neo4j](https://neo4j.com)
- [arangodb](https://arangodb.com)
- [elasticsearch](https://www.elastic.co/elasticsearch#)
- [timescaledb](https://www.timescale.com)
- [couchdb](https://couchdb.apache.org)
- [dgraph](https://dgraph.io)
- [meilisearch](https://www.meilisearch.com)
- [mailpit](https://mailpit.axllent.org) — mail-mock SMTP + web UI (the local mail server Supabase uses); web UI port via `port2`
- [faunadb](https://fauna.com)
- [yugabytedb](https://www.yugabyte.com)
- [skytable](https://skytable.io)
- [dragonfly](https://www.dragonflydb.io)
- [redict](https://redict.io)
- [valkey](https://github.com/valkey-io/valkey)
- [postgis](https://postgis.net)
- [pgvector](https://github.com/pgvector/pgvector) — postgres + `pgvector` extension. Uses prefix `DB_`, same as plain `postgres`
- [localstack](https://docs.localstack.cloud/) — single container for multiple AWS services (sqs, s3, sns, secretsmanager, ssm, kinesis), with `queues` / `buckets` / `topics` / `secrets` auto-created from config. Full docs: [drivers/localstack.md](drivers/localstack.md)
- [supabase](https://supabase.com/docs/guides/local-development) — wraps `supabase init`/`start`. Emits `SUPABASE_*` + S3 vars, ports from config.toml. Seeds `buckets:` and `authUsers:` on `up`; `jwtSecret:` re-signs keys; `configTomlPath:` makes corgi own config.toml under `corgi_services/`; `port:`/`dbPort:`/`studioPort:`/`inbucketPort:` patch the matching `[section].port`. Full docs: [drivers/supabase.md](drivers/supabase.md)
- `image` — generic docker-image driver for any public image (gotenberg, mailhog, jaeger, meilisearch, …). Set `image:` + `port:` + optional `containerPort:`/`environment:`/`volumes:`/`command:`. Full docs: [drivers/image.md](drivers/image.md)
