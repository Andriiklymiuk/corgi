#!/usr/bin/env bash
source "$(dirname "$0")/../_fixtures/common.sh"

start_repo
printf 'logs/\n' >> .git/info/exclude
write_base
commit_all "Initial api"
add_origin

mkdir -p api/logs
cat > api/logs/api.log <<'LOG'
2026-09-30T10:12:41Z INFO  GET /health 200
2026-09-30T10:12:43Z INFO  GET /orders/o-1 200
2026-09-30T10:12:44Z ERROR GET /orders/export 500 ValueError: malformed row: o-3,Cable, 2m,9
2026-09-30T10:13:02Z INFO  GET /health 200
LOG
