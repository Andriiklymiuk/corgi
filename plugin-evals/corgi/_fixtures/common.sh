#!/usr/bin/env bash
# Builds the small workspace every eval case runs in: one corgi-compose.yml,
# one Python api with tests, a local bare remote. No network, no logins.
set -euo pipefail

TEST_CMD="python3 -m unittest discover -s tests -q"

start_repo() {
  git init -q .
  git symbolic-ref HEAD refs/heads/main
  git config user.email "dev@acme.test"
  git config user.name "Dev"
  git config commit.gpgsign false
  printf '.remotes/\n__pycache__/\n' >> .git/info/exclude
}

commit_all() {
  git add -A
  git commit -q -m "$1"
}

add_origin() {
  git init -q --bare .remotes/origin.git
  git remote add origin "$PWD/.remotes/origin.git"
  git push -q origin main
  git remote set-head origin main >/dev/null
}

write_base() {
  mkdir -p api/app api/tests api/deploy api/.github/workflows

  cat > corgi-compose.yml <<'YML'
name: acme
services:
  api:
    path: ./api
    start:
      - python3 -m app.server
YML

  cat > README.md <<'MD'
# acme

One service, `api`: accounts, orders and the order export.

- `GET /health` reports the build version and whether the store answers.
- `POST /login`, `POST /signup`, `POST /reset-password`
- `GET /orders/<id>`, `GET /orders/export` (CSV)

Run the tests: `cd api && python3 -m unittest discover -s tests -q`
MD

  cat > api/Makefile <<'MK'
test:
	python3 -m unittest discover -s tests -q
MK

  cat > api/.env.example <<'ENV'
DATABASE_URL=postgres://localhost:5432/acme
SESSION_SECRET=change-me
ENV

  cat > api/deploy/task-definition.json <<'JSON'
{
  "family": "acme-api",
  "containerDefinitions": [
    {
      "name": "api",
      "environment": [
        { "name": "DATABASE_URL", "value": "postgres://db.internal:5432/acme" }
      ],
      "secrets": [
        { "name": "SESSION_SECRET", "valueFrom": "arn:aws:ssm:eu-west-1:000000000000:parameter/acme/session-secret" }
      ]
    }
  ]
}
JSON

  cat > api/.github/workflows/deploy.yml <<'YML'
name: Deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - run: make test
      - run: aws ecs register-task-definition --cli-input-json file://deploy/task-definition.json
YML

  : > api/app/__init__.py
  : > api/tests/__init__.py

  cat > api/app/http.py <<'PY'
import json


def response(status, body):
    return {"status": status, "body": json.dumps(body)}


def require_fields(payload, *names):
    missing = [name for name in names if not payload.get(name)]
    if missing:
        return response(400, {"error": "missing " + ", ".join(missing)})
    return None
PY

  cat > api/app/store.py <<'PY'
USERS = {
    "u-1": {"id": "u-1", "email": "ana@acme.test", "password": "pw-ana"},
    "u-2": {"id": "u-2", "email": "bo@acme.test", "password": "pw-bo"},
}

ORDERS = [
    {"id": "o-1", "user_id": "u-1", "product": "Keyboard", "total": 49},
    {"id": "o-2", "user_id": "u-2", "product": "Monitor arm", "total": 89},
    {"id": "o-3", "user_id": "u-1", "product": "Cable, 2m", "total": 9},
]
PY

  cat > api/app/users.py <<'PY'
from app.store import USERS


def fetch_user(user_id):
    return USERS.get(user_id)


def find_by_email(email):
    for user in USERS.values():
        if user["email"] == email:
            return user
    return None
PY

  cat > api/app/auth.py <<'PY'
from app.http import require_fields, response
from app.store import USERS
from app.users import find_by_email


def login(payload, client_ip):
    bad = require_fields(payload, "email", "password")
    if bad:
        return bad
    user = find_by_email(payload["email"])
    if user is None or user["password"] != payload["password"]:
        return response(401, {"error": "invalid credentials"})
    return response(200, {"token": "t-" + user["id"]})


def signup(payload, client_ip):
    bad = require_fields(payload, "email", "password")
    if bad:
        return bad
    if find_by_email(payload["email"]) is not None:
        return response(409, {"error": "email taken"})
    user_id = "u-" + str(len(USERS) + 1)
    USERS[user_id] = {"id": user_id, "email": payload["email"], "password": payload["password"]}
    return response(201, {"id": user_id})


def reset_password(payload, client_ip):
    bad = require_fields(payload, "email")
    if bad:
        return bad
    return response(202, {"sent": find_by_email(payload["email"]) is not None})
PY

  cat > api/app/orders.py <<'PY'
from app.http import response
from app.store import ORDERS
from app.users import fetch_user


def order_view(order_id):
    for order in ORDERS:
        if order["id"] == order_id:
            owner = fetch_user(order["user_id"])
            return response(200, {"id": order["id"], "product": order["product"], "owner": owner["email"]})
    return response(404, {"error": "no such order"})
PY

  cat > api/app/notify.py <<'PY'
from app.users import fetch_user


def owner_email(order):
    owner = fetch_user(order["user_id"])
    return owner["email"] if owner else None
PY

  cat > api/app/export.py <<'PY'
from app.http import response


def export_orders(orders):
    lines = ["id,product,total"]
    for order in orders:
        lines.append(",".join([order["id"], order["product"], str(order["total"])]))
    return response(200, {"csv": "\n".join(lines), "rows": count_rows(lines)})


def count_rows(lines):
    for line in lines:
        if len(line.split(",")) != 3:
            raise ValueError("malformed row: " + line)
    return len(lines) - 1
PY

  cat > api/app/health.py <<'PY'
from app.http import response


def health():
    return response(200, {"ok": True})
PY

  cat > api/app/server.py <<'PY'
import logging

from app import auth, export, health, orders
from app.http import response
from app.store import ORDERS

log = logging.getLogger("api")


def handle(method, path, payload=None, client_ip="127.0.0.1"):
    try:
        return route(method, path, payload or {}, client_ip)
    except Exception as error:
        log.error("%s %s 500 %s: %s", method, path, type(error).__name__, error)
        return response(500, {"error": "internal error"})


def route(method, path, payload, client_ip):
    if path == "/health":
        return health.health()
    if path == "/login":
        return auth.login(payload, client_ip)
    if path == "/signup":
        return auth.signup(payload, client_ip)
    if path == "/reset-password":
        return auth.reset_password(payload, client_ip)
    if path == "/orders/export":
        return export.export_orders(ORDERS)
    if path.startswith("/orders/"):
        return orders.order_view(path.rsplit("/", 1)[1])
    return response(404, {"error": "not found"})
PY

  cat > api/tests/test_auth.py <<'PY'
import unittest

from app import auth


class LoginTest(unittest.TestCase):
    def test_login_returns_a_token(self):
        result = auth.login({"email": "ana@acme.test", "password": "pw-ana"}, "10.0.0.1")
        self.assertEqual(result["status"], 200)

    def test_login_rejects_a_wrong_password(self):
        result = auth.login({"email": "ana@acme.test", "password": "nope"}, "10.0.0.1")
        self.assertEqual(result["status"], 401)

    def test_reset_password_needs_an_email(self):
        self.assertEqual(auth.reset_password({}, "10.0.0.1")["status"], 400)
PY

  cat > api/tests/test_orders.py <<'PY'
import unittest
from unittest import mock

from app import notify, orders


class OrderViewTest(unittest.TestCase):
    def test_order_view_names_the_owner(self):
        with mock.patch("app.orders.fetch_user", return_value={"email": "ana@acme.test"}):
            result = orders.order_view("o-1")
        self.assertEqual(result["status"], 200)

    def test_unknown_order_is_404(self):
        self.assertEqual(orders.order_view("o-404")["status"], 404)


class NotifyTest(unittest.TestCase):
    def test_owner_email(self):
        with mock.patch("app.notify.fetch_user", return_value={"email": "bo@acme.test"}):
            self.assertEqual(notify.owner_email({"user_id": "u-2"}), "bo@acme.test")
PY

  cat > api/tests/test_export.py <<'PY'
import json
import unittest

from app import export


class ExportTest(unittest.TestCase):
    def test_export_has_a_row_per_order(self):
        result = export.export_orders([{"id": "o-1", "product": "Keyboard", "total": 49}])
        self.assertEqual(json.loads(result["body"])["rows"], 1)
PY
}
