#!/usr/bin/env bash
source "$(dirname "$0")/../_fixtures/common.sh"

start_repo
printf '.mr/\n' >> .git/info/exclude
write_base
commit_all "Initial api"
add_origin

git checkout -q -b feature/ABC-41/user-profile
cat > api/app/profile.py <<'PY'
from app.http import response
from app.users import fetch_user


def get_user(user_id):
    user = fetch_user(user_id)
    if user is None:
        return response(404, {"error": "no such user"})
    return response(200, {"id": user["id"], "email": user["email"]})
PY
python3 - <<'PY'
from pathlib import Path

path = Path("api/app/server.py")
source = path.read_text()
source = source.replace("from app import auth, export, health, orders\n", "from app import auth, export, health, orders, profile\n")
source = source.replace(
    "    if path.startswith(\"/orders/\"):",
    "    if path.startswith(\"/users/\"):\n        return profile.get_user(path.rsplit(\"/\", 1)[1])\n    if path.startswith(\"/orders/\"):",
)
path.write_text(source)
PY
cat > api/tests/test_profile.py <<'PY'
import json
import unittest

from app import profile


class ProfileTest(unittest.TestCase):
    def test_get_user_returns_the_email(self):
        result = profile.get_user("u-1")
        self.assertEqual(result["status"], 200)
        self.assertEqual(json.loads(result["body"])["email"], "ana@acme.test")
PY
commit_all "ABC-41 add the user profile endpoint"
git push -q -u origin feature/ABC-41/user-profile

mkdir -p .mr/41
cat > .mr/41/threads.json <<'JSON'
[
  {
    "id": "d1",
    "resolved": false,
    "file": "api/app/users.py",
    "line": 4,
    "notes": [
      { "author": "jlindqvist", "body": "Rename fetch_user to fetch_user_by_id - we are about to add a lookup by email next to it and the bare name will read as ambiguous." }
    ]
  },
  {
    "id": "d2",
    "resolved": false,
    "file": "api/app/profile.py",
    "line": 8,
    "notes": [
      { "author": "jlindqvist", "body": "The 404 branch has no test. Please add one." }
    ]
  },
  {
    "id": "d3",
    "resolved": true,
    "file": "api/app/server.py",
    "line": 30,
    "notes": [
      { "author": "jlindqvist", "body": "Route order looks right now, thanks." }
    ]
  }
]
JSON
