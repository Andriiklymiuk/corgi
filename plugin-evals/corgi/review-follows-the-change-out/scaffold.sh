#!/usr/bin/env bash
source "$(dirname "$0")/../_fixtures/common.sh"

start_repo
printf '.pr/\n' >> .git/info/exclude
write_base
commit_all "Initial api"
add_origin

git checkout -q -b feature/ABC-57/login-rate-limit
printf 'RATE_LIMIT_PER_MINUTE=60\n' >> api/.env.example
cat > api/app/rate_limit.py <<'PY'
import os
import time

_hits = {}


def limit_per_minute():
    return int(os.environ.get("RATE_LIMIT_PER_MINUTE", "0"))


def check_rate_limit(key):
    limit = limit_per_minute()
    if limit <= 0:
        return True
    now = time.time()
    recent = [hit for hit in _hits.get(key, []) if now - hit < 60]
    recent.append(now)
    _hits[key] = recent
    return len(recent) <= limit


def rate_limited(handler):
    handler.rate_limited = True
    return handler
PY
python3 - <<'PY'
from pathlib import Path

path = Path("api/app/auth.py")
source = path.read_text()
source = source.replace(
    "from app.http import require_fields, response\n",
    "from app.http import require_fields, response\nfrom app.rate_limit import check_rate_limit, rate_limited\n",
)
source = source.replace(
    "def login(payload, client_ip):\n    bad = require_fields(payload, \"email\", \"password\")\n    if bad:\n        return bad\n",
    "@rate_limited\ndef login(payload, client_ip):\n    bad = require_fields(payload, \"email\", \"password\")\n    if bad:\n        return bad\n    if not check_rate_limit(client_ip):\n        return response(429, {\"error\": \"too many attempts\"})\n",
)
path.write_text(source)
PY
cat > api/tests/test_rate_limit.py <<'PY'
import unittest

from app import auth


class RateLimitTest(unittest.TestCase):
    def test_login_is_rate_limited(self):
        self.assertTrue(auth.login.rate_limited)
PY
commit_all "ABC-57 rate limit the login handler"
git push -q origin feature/ABC-57/login-rate-limit
git checkout -q main

mkdir -p .pr/57
git diff main...feature/ABC-57/login-rate-limit > .pr/57/diff.patch
mkdir -p .pr/57/head
git archive feature/ABC-57/login-rate-limit | tar -x -C .pr/57/head
cat > .pr/57/pr.json <<'JSON'
{
  "number": 57,
  "url": "https://github.com/acme/api/pull/57",
  "title": "ABC-57 Rate limit login",
  "state": "OPEN",
  "isDraft": false,
  "author": { "login": "mkowalski" },
  "baseRefName": "main",
  "headRefName": "feature/ABC-57/login-rate-limit",
  "body": "Login is now limited per client IP.\n\nDelivery: RATE_LIMIT_PER_MINUTE is in .env.example (60).\nTests: a unit test covers the rate limit on the login handler.",
  "statusCheckRollup": [{ "name": "test", "conclusion": "SUCCESS" }]
}
JSON
