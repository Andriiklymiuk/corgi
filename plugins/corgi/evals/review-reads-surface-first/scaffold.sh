#!/usr/bin/env bash
source "$(dirname "$0")/../_fixtures/common.sh"

start_repo
printf '.pr/\n' >> .git/info/exclude
write_base
commit_all "Initial api"
add_origin

git checkout -q -b feature/ABC-42/tenant-scoped-users
python3 - <<'PY'
from pathlib import Path

store = Path("api/app/store.py")
store.write_text(store.read_text().replace('"password": "pw-ana"}', '"password": "pw-ana", "tenant": "t-1"}').replace('"password": "pw-bo"}', '"password": "pw-bo", "tenant": "t-2"}'))

users = Path("api/app/users.py")
users.write_text(users.read_text().replace(
    "def fetch_user(user_id):\n    return USERS.get(user_id)\n",
    "def fetch_user(user_id, tenant_id):\n    user = USERS.get(user_id)\n    if user is None or user[\"tenant\"] != tenant_id:\n        return None\n    return user\n",
))

orders = Path("api/app/orders.py")
orders.write_text(orders.read_text().replace(
    "def order_view(order_id):", "def order_view(order_id, tenant_id):"
).replace(
    "owner = fetch_user(order[\"user_id\"])\n            return response(200, {\"id\": order[\"id\"], \"product\": order[\"product\"], \"owner\": owner[\"email\"]})",
    "owner = fetch_user(order[\"user_id\"], tenant_id)\n            if owner is None:\n                break\n            return response(200, {\"id\": order[\"id\"], \"product\": order[\"product\"], \"owner\": owner[\"email\"]})",
))

server = Path("api/app/server.py")
server.write_text(server.read_text().replace(
    "return orders.order_view(path.rsplit(\"/\", 1)[1])",
    "return orders.order_view(path.rsplit(\"/\", 1)[1], payload.get(\"tenant\", \"\"))",
))

tests = Path("api/tests/test_orders.py")
tests.write_text(tests.read_text().replace('orders.order_view("o-1")', 'orders.order_view("o-1", "t-1")').replace('orders.order_view("o-404")', 'orders.order_view("o-404", "t-1")'))
PY
commit_all "ABC-42 scope user lookups by tenant"
git push -q origin feature/ABC-42/tenant-scoped-users
git checkout -q main

mkdir -p .pr/42
git diff main...feature/ABC-42/tenant-scoped-users > .pr/42/diff.patch
mkdir -p .pr/42/head
git archive feature/ABC-42/tenant-scoped-users | tar -x -C .pr/42/head
cat > .pr/42/pr.json <<'JSON'
{
  "number": 42,
  "url": "https://github.com/acme/api/pull/42",
  "title": "ABC-42 Scope user lookups by tenant",
  "state": "OPEN",
  "isDraft": false,
  "author": { "login": "mkowalski" },
  "baseRefName": "main",
  "headRefName": "feature/ABC-42/tenant-scoped-users",
  "body": "An order's owner is now looked up inside the caller's tenant, so one tenant can no longer read another's order.\n\n## Changed surface\n- changed (breaking): `app.users.fetch_user(user_id)` -> `fetch_user(user_id, tenant_id)`\n- changed (breaking): `app.orders.order_view(order_id)` -> `order_view(order_id, tenant_id)`\n\nTests pass.",
  "statusCheckRollup": [{ "name": "test", "conclusion": "SUCCESS" }]
}
JSON
