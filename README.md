<div align="center">
  <img width="300" height="300" src="./resources/corgi.png">
  
  # 🐶 CORGI 🐶
  [![Reliability Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=reliability_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Bugs](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=bugs)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
  [![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

[![Security Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=security_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Vulnerabilities](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=vulnerabilities)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Lines of Code](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=ncloc)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)
[![Technical Debt](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=sqale_index)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=Andriiklymiuk_corgi&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=Andriiklymiuk_corgi)

</div>

Send someone your project yml file, init and run it in minutes.

No more long meetings, explanations of how to run new project with multiple microservices and configs. Just send corgi-compose.yml file to your team and corgi will do the rest.

Auto git cloning, db seeding, concurrent running and much more.

While in services you can create whatever you want, but in db services **for now it supports**:

- [postgres](https://www.postgresql.org), [example](https://github.com/Andriiklymiuk/corgi_examples/tree/main/postgres)
- [mongodb](https://www.mongodb.com), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/mongodb/mongodb-go.corgi-compose.yml)
- [rabbitmq](https://www.rabbitmq.com), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/rabbitmq/rabbitmq-go-nestjs.corgi-compose.yml)
- [aws sqs](https://docs.localstack.cloud/user-guide/aws/sqs/), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/aws_sqs/aws_sqs_postgres_go_deno.corgi-compose.yml)
- [redis](https://redis.io), [example](https://github.com/Andriiklymiuk/corgi_examples/blob/main/redis/redis-bun-expo.corgi-compose.yml)
- [redis-server](https://redis.io)
- [mysql](https://www.mysql.com)
- [mariadb](https://mariadb.org)
- [dynamodb](https://aws.amazon.com/dynamodb/)
- [kafka](https://kafka.apache.org)
- [mssql](https://www.microsoft.com/en-us/sql-server/sql-server-downloads)
- [cassandra](https://cassandra.apache.org/_/index.html)
- [cockroachDb](https://www.cockroachlabs.com)
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
- [faunadb](https://fauna.com)
- [yugabytedb](https://www.yugabyte.com)
- [skytable](https://skytable.io)
- [dragonfly](https://www.dragonflydb.io)
- [redict](https://redict.io)
- [valkey](https://github.com/valkey-io/valkey)
- [postgis](https://postgis.net)
- [pgvector](https://github.com/pgvector/pgvector) — postgres + `pgvector` extension. Uses prefix `DB_`, same as plain `postgres`
- [localstack](https://docs.localstack.cloud/) — single container for multiple AWS services (sqs, s3, sns, secretsmanager, ssm, kinesis), with `queues` / `buckets` / `topics` / `secrets` auto-created from config. Full docs: [docs/drivers/localstack.md](docs/drivers/localstack.md)
- [supabase](https://supabase.com/docs/guides/local-development) — wraps `supabase init`/`start`. Emits `SUPABASE_*` + S3 vars, ports from `supabase/config.toml`. Seeds `buckets:` and `authUsers:` on `up`; `jwtSecret:` re-signs keys. Full docs: [docs/drivers/supabase.md](docs/drivers/supabase.md)
- `image` — generic docker-image driver for any public image (gotenberg, mailhog, jaeger, meilisearch, …). Set `image:` + `port:` + optional `containerPort:`/`environment:`/`volumes:`/`command:`. Full docs: [docs/drivers/image.md](docs/drivers/image.md)

## Preflight & healthcheck

- `corgi doctor` (aliases: `check`, `preflight`) — before `corgi run`: verifies every tool in `required:`, Docker is up, and every port in `db_services.*.port` / `services.*.port` is free (lists the offending process if not)
- `corgi status` (aliases: `health`, `healthcheck`) — after `corgi run`: TCP-probes every declared port. If a service sets `healthCheck: /some/path`, corgi does an HTTP probe and accepts any non-5xx response as healthy. The `localstack` driver defaults to `GET /_localstack/health`. Flags: `-w/--watch` (live monitor), `-r/--ready` (block until all healthy or `--timeout`), `--service <csv>` (narrow scope), `--json` (machine output), `-q/--quiet` (exit-code-only).

## Tunneling

`corgi tunnel` opens public HTTPS tunnels to declared services. Useful for webhook testing (DocuSeal, Stripe, etc.) without ngrok-style signup.

```bash
corgi tunnel                       # tunnel every services.<name> with port: set
corgi tunnel api                   # single service
corgi tunnel api,api-2     # csv
corgi tunnel --port 3030           # raw port, skips compose lookup
corgi tunnel --provider ngrok      # default: cloudflared (free, no signup)
```

Default provider = [Cloudflare Quick Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/) via `cloudflared`. Anonymous, free, ephemeral URLs (rotate per restart). For stable URLs, use Cloudflare Named Tunnels (login required) or another provider.

Auth-needing providers (e.g. ngrok) are detected before any subprocess starts — corgi prints the exact login command and exits without partial state.

## Documentation

You can check documentation on https://andriiklymiuk.github.io/corgi/

And here is small 2 min video showcase https://youtu.be/rlMCjs4EoFs?si=o3SQaymM55zxBCUY

## VSCODE users

You can install [corgi extension](https://marketplace.visualstudio.com/items?itemName=corgi.corgi) to get syntax highlights and much more

## Claude Code users

This repo ships a [Claude Code](https://claude.com/claude-code) plugin so an AI agent can author your `corgi-compose.yml`, run corgi, and debug failures accurately.

```
/plugin marketplace add Andriiklymiuk/corgi
/plugin install corgi@corgi
```

Then in any project that has a `corgi-compose.yml`, Claude will recognize it and use `corgi run` / `corgi doctor` / `corgi status` instead of inventing its own commands. A `/corgi-new` slash command scaffolds a fresh `corgi-compose.yml` from a short conversation.

## Install

After install, `corgi` is available globally — run it from any folder.

### macOS / Linux — [Homebrew](https://brew.sh)

```bash
brew install andriiklymiuk/homebrew-tools/corgi
```

### macOS / Linux — install script

No Homebrew? One-liner that picks the right OS/arch binary from GitHub releases:

```bash
curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | sh
```

Installs to `/usr/local/bin` if writable, otherwise `~/.local/bin` (auto-added to PATH for zsh/bash/fish).

Useful overrides:

- `CORGI_VERSION=1.10.0` — pin a version
- `CORGI_INSTALL_DIR=$HOME/bin` — force a directory
- `CORGI_NO_MODIFY_PATH=1` — don't touch shell rc files

### Windows — PowerShell

```powershell
irm https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\corgi\bin` and adds it to your user PATH.

### Verify

```bash
corgi -h
```

`corgi update` (alias `corgi upgrade`) detects how you installed and uses the matching method to upgrade.

Try it with expo + hono server example

```bash
corgi run -t https://github.com/Andriiklymiuk/corgi_examples/blob/main/honoExpoTodo/hono-bun-expo.corgi-compose.yml
```

## Credits & thanks

- `corgi tunnel` defaults to [cloudflared](https://github.com/cloudflare/cloudflared) ([Apache 2.0](https://github.com/cloudflare/cloudflared/blob/master/LICENSE)) and its free, no-signup [Quick Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/do-more-with-tunnels/trycloudflare/). Big thanks to Cloudflare for shipping this open and free — makes local webhook testing painless.
- Optional providers: [ngrok](https://ngrok.com) (closed source, free tier with authtoken) and [localtunnel](https://github.com/localtunnel/localtunnel) ([MIT](https://github.com/localtunnel/localtunnel/blob/master/LICENSE)) — thanks to both projects for the alternatives.
- <a href="https://www.freepik.com/free-vector/cute-corgi-dog-astronaut-floating-space-cartoon-vector-icon-illustration-animal-science-icon-concept-isolated-premium-vector-flat-cartoon-style_22271104.htm#query=corgi%20icon&position=7&from_view=keyword">Corgi image by catalyststuff</a> on Freepik
