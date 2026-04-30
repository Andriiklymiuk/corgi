# localstack driver

Single container that emulates multiple AWS services (SQS, S3, SNS, Secrets Manager, SSM, Kinesis, Lambda, etc.). Replaces standalone `sqs`/`s3` drivers when you need multi-service coverage.

## Quick start

```yaml
db_services:
  aws:
    driver: localstack
    port: 4566
    services: [sqs, s3]                # default if omitted
    queues: [jobs, dead-letter]
    buckets: [uploads, thumbnails]
```

`corgi run` will:
1. Start localstack container on `:4566`
2. Run bootstrap.sh — creates queues, buckets, topics, secrets, parameters, streams via `awslocal` CLI inside the container
3. Probe `/_localstack/health` (default healthcheck for this driver)

## Emitted env vars (prefix `AWS_`)

| Var | Source |
| --- | --- |
| `AWS_HOST`, `AWS_PORT` | localstack container |
| `AWS_ENDPOINT_URL` | `http://<host>:<port>` |
| `AWS_SQS_ENDPOINT` | `http://<host>:<port>/000000000000/` |
| `AWS_S3_ENDPOINT_URL` | `http://<host>:<port>` |
| `AWS_REGION` | `eu-central-1` (driver default) |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | `test` (localstack convention) |
| `AWS_SQS_<UPPER_NAME>` | one per `queues:` entry, value = queue name |
| `AWS_SQS_<UPPER_NAME>_URL` | full queue URL |
| `AWS_S3_<UPPER_NAME>_BUCKET` | one per `buckets:` entry |
| `AWS_SNS_<UPPER_NAME>` / `_ARN` | one per `topics:` entry |
| `AWS_SECRET_<KEY>` | one per `secrets:` entry |
| `AWS_SSM_<KEY>` | one per `parameters:` entry |
| `AWS_KINESIS_<UPPER_NAME>` | one per `streams:` entry |

`-` in names becomes `_` and uppercased: `dead-letter` → `AWS_SQS_DEAD_LETTER`.

## Fields

### `services: []string`

AWS services to enable in the localstack container. Default `[sqs, s3]`. Common values: `sqs`, `s3`, `sns`, `secretsmanager`, `ssm`, `kinesis`, `lambda`, `dynamodb`, `events`, `stepfunctions`.

```yaml
services: [sqs, s3, sns, secretsmanager]
```

Localstack image `3.8` is the last tag that runs without `LOCALSTACK_AUTH_TOKEN`. Newer tags require Hobby plan or higher (since March 2026).

### `queues: []string`

SQS queues to auto-create on bootstrap. Each emits `AWS_SQS_<UPPER>=name` and `AWS_SQS_<UPPER>_URL=full-url`.

```yaml
queues: [jobs, dead-letter, notifications]
```

### `buckets: []string`

S3 buckets to auto-create. Each emits `AWS_S3_<UPPER>_BUCKET=name`.

```yaml
buckets: [uploads, thumbnails, public-assets]
```

### `topics: []string`

SNS topics. Implies the `sns` service. Each emits `AWS_SNS_<UPPER>=name` and `AWS_SNS_<UPPER>_ARN=full-arn`.

```yaml
services: [sqs, sns]
topics: [user-events, order-events]
```

### `subscriptions: []SnsSubscription`

SNS topic → SQS queue wiring. Each entry must reference a topic from `topics:` and a queue from `queues:`.

```yaml
subscriptions:
  - topic: user-events
    queue: jobs
  - topic: order-events
    queue: notifications
```

Bootstrap creates both topic + subscription in dependency order.

### `secrets: []AwsSecret`

Secrets Manager entries seeded on `up`. Implies `secretsmanager` service.

```yaml
services: [sqs, secretsmanager]
secrets:
  - name: api/db-credentials
    value: '{"username":"admin","password":"secret"}'
```

Each emits `AWS_SECRET_<FLATTENED_KEY>` env var with the secret name (NOT value — pull live via SDK).

### `parameters: []SsmParameter`

SSM Parameter Store entries. Implies `ssm` service.

```yaml
services: [sqs, ssm]
parameters:
  - name: /myapp/feature-flag
    value: enabled
    type: String      # String | StringList | SecureString (default: String)
```

### `streams: []string`

Kinesis streams (one shard each). Implies `kinesis` service.

```yaml
services: [sqs, kinesis]
streams: [analytics-events, audit-log]
```

## Healthcheck

Default `GET /_localstack/health` — corgi auto-applies for `driver: localstack` if you don't set `healthCheck:`. Override only if you've changed port or disabled the health service.

## Bootstrap script

corgi generates `corgi_services/db_services/<name>/bootstrap/bootstrap.sh`. Mounted into the container at `/etc/localstack/init/ready.d/`. Localstack auto-runs it after services are up. Logs visible via `docker logs localstack-<name>`.

Idempotent — every `awslocal` call ends with `|| true` so re-runs don't fail on existing resources.

## Versions

Driver default image: `localstack/localstack:3.8`. Override:
```yaml
db_services:
  aws:
    driver: localstack
    version: "3.8"   # quoted because YAML otherwise reads as float
```

Newer tags (4.x) need `LOCALSTACK_AUTH_TOKEN` for many services. Stick with 3.8 unless you have Hobby plan.

## Common patterns

### App needs SQS only

```yaml
db_services:
  aws:
    driver: localstack
    services: [sqs]
    queues: [jobs]
```

### App needs SQS + S3 + SNS pub/sub

```yaml
db_services:
  aws:
    driver: localstack
    services: [sqs, s3, sns]
    queues: [worker-queue]
    buckets: [uploads]
    topics: [events]
    subscriptions:
      - topic: events
        queue: worker-queue
```

## Picking driver vs alternatives

- **Use `localstack`** for any AWS service emulation. Single container, multi-service.
- **Don't use** standalone `sqs`/`s3` drivers — they predate localstack support and only cover one service each.
- **For Storage S3** — if you're using **Supabase Storage**, prefer the `supabase` driver instead. It exposes an S3-compatible API on `:54321/storage/v1/s3` natively.

## Troubleshooting

**`localstack` ❌ right after `corgi run`** — services boot async. Wait 5-10s and re-probe with `corgi status`.

**Bootstrap script ran but resources missing** — check `docker logs localstack-<name>` for `awslocal` errors. Common: typos in queue/bucket name or missing service in `services:` list.

**`subscriptions:` errors** — topic AND queue both must be declared, AND `services:` must include both `sqs` and `sns`.

**Secret value with special chars** — wrap in single quotes; bash-escape `$` and `"` if needed:
```yaml
secrets:
  - name: my-secret
    value: '{"key":"val\"ue"}'
```

**Image pull fails on tag** — verify localstack version. `4.x` may need auth token. Fall back to `3.8`.

**Port 4566 already taken** — old localstack from another project. `docker ps | grep localstack` and `docker stop <id>`.

## Related

- [supabase driver](supabase.md) — for local auth + S3-compatible Storage
- [healthchecks reference](../../plugins/corgi/skills/corgi/references/healthchecks.md)
