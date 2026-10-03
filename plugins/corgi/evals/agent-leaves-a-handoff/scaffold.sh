#!/usr/bin/env bash
source "$(dirname "$0")/../_fixtures/common.sh"

start_repo
write_base
cat >> corgi-compose.yml <<'YML'
  web:
    path: ./web
    start:
      - python3 -m http.server 3000
YML
mkdir -p web/src
printf '<main id="app"></main>\n' > web/src/index.html
commit_all "Initial api and web"
add_origin

git checkout -q -b feature/ABC-7/usage-limit-banner
cat > api/app/usage.py <<'PY'
from app.http import response

LIMIT = 100


def check_usage(used):
    if used >= LIMIT:
        return response(429, {"error": "usage limit reached", "limit": LIMIT})
    return None
PY
cat > api/tests/test_usage.py <<'PY'
import unittest

from app import usage


class UsageTest(unittest.TestCase):
    def test_over_the_limit_is_429(self):
        self.assertEqual(usage.check_usage(100)["status"], 429)

    def test_under_the_limit_passes(self):
        self.assertIsNone(usage.check_usage(3))
PY
commit_all "ABC-7 api returns 429 when the usage limit is hit"
