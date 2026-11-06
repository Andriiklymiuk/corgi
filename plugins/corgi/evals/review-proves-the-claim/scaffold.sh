#!/usr/bin/env bash
source "$(dirname "$0")/../_fixtures/common.sh"

start_repo
printf '.mr/\n' >> .git/info/exclude
write_base
commit_all "Initial api"
add_origin

git checkout -q -b feature/ABC-43/user-profile
cat > api/app/profile.py <<'PY'
from app.http import response
from app.users import fetch_user


def get_user(user_id):
    user = fetch_user(user_id)
    if user is None:
        return response(404, {"error": "no such user"})
    return response(200, {"id": user["id"], "email": user["email"]})
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
commit_all "ABC-43 add the user profile endpoint"
git push -q -u origin feature/ABC-43/user-profile

mkdir -p .mr/43
cat > .mr/43/threads.json <<'JSON'
[
  {
    "id": "d1",
    "resolved": false,
    "file": "api/app/profile.py",
    "line": 9,
    "notes": [
      { "author": "jlindqvist", "body": "Bug: accounts made through signup can have no email, so user[\"email\"] crashes here with a KeyError. Please guard it with .get()." }
    ]
  },
  {
    "id": "d2",
    "resolved": false,
    "file": "api/app/profile.py",
    "line": 9,
    "notes": [
      { "author": "jlindqvist", "body": "Not sure anyone decided this: should the profile show the email to every signed-in user? One line to drop it if you think not." }
    ]
  },
  {
    "id": "d3",
    "resolved": false,
    "file": "api/app/profile.py",
    "line": 8,
    "notes": [
      { "author": "jlindqvist", "body": "The 404 branch has no test. Please add one." }
    ]
  }
]
JSON
