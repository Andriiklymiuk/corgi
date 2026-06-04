# GitHub / GitLab commands — gh / glab incantations

Copy-pasteable commands for fetching PR/MR data and posting review output.
Cited by SKILL.md phases P0 (sibling enum), P1 (fetch) and P5 (post). **Both
forges are first-class and kept at parity** — every GitHub command has a GitLab
equivalent below; resolve the forge per ref (P0) and use the matching block.

GitLab repo selector `<repo>` = `<host>/<group>/<proj>` (or `OWNER/REPO`), passed
with `-R` — no URL-encoding needed. The raw-API fallback (§3b) needs the
URL-encoded project path `<group>%2F<proj>` plus `--hostname <host>` instead.

## 0. Token efficiency (rtk) — without degrading review quality

`rtk` (the user's token-killer proxy) filters/compresses command output. The Claude
Code hook auto-rewrites `git`/`gh`/`glab` calls through it, so **metadata, status,
and list calls get the savings for free** — safe, the review doesn't need those
verbatim.

**One hard exception: the diff that gets reviewed must be full-fidelity.** A
filtered/truncated diff = a bad review. Fetch the reviewable diff **raw**:

```bash
rtk proxy gh pr diff <n> --repo <owner>/<repo> --patch        # raw, unfiltered
rtk proxy glab mr diff <n> --repo <repo> --color=never
```

Rule of thumb: **rtk-filtered for everything except the diff content under review**
(and any file body you must read in full). When in doubt about fidelity,
`rtk proxy` it.

---

## 1. Fetch (read-only, no checkout)

Fetch metadata + anchoring SHAs in **one call per PR/MR** (don't make a second call
just for the SHA):

```bash
# GitHub — metadata + head/base SHAs in one --json call
gh pr view <n> --repo <owner>/<repo> \
  --json title,body,author,baseRefName,headRefName,state,isDraft,files,url,headRefOid,baseRefOid,commits

# GitLab — metadata INCLUDING diff_refs (base_sha/head_sha/start_sha) + commits in one call
glab mr view <n> -R <repo> -F json    # read .diff_refs, .state, .draft, .source_branch, .commits from the JSON
```

Reviewable diff (raw, see §0):
```bash
rtk proxy gh pr diff <n> --repo <owner>/<repo> --patch
rtk proxy glab mr diff <n> -R <repo> --color=never
```

**Sibling enumeration** (P0 auto-detect — find same-branch PRs/MRs in other repos):
```bash
gh pr list --head <branch> --repo <owner>/<repo> --json number,title,url,state,isDraft
glab mr list --source-branch <branch> -R <repo> -F json
```

Note: `gh pr diff`/`glab mr diff` diff each PR/MR against **its own base branch**.
For a stacked PR (B's commits contain A's) isolate B's own commits with the
compare API — **no checkout** (never `git diff A..B` against a tree you didn't
fetch):
```bash
gh api repos/<owner>/<repo>/compare/<A-headRefOid>...<B-headRefOid> -q '.files[].filename'
```
GitLab: read `.commits` from `glab mr view -F json` and diff per-commit if needed.
If it can't be isolated cleanly, review the full diff and note the double-review.

---

## 2. GitHub — post summary + inline suggestions in one review

Build a JSON payload, pipe via `--input -`. Each suggestion is a fenced
` ```suggestion ` block inside the comment body. `line` = line in the **new** file
(from the hunk header); `side: RIGHT` = added/context, `side: LEFT` = removed.
Each inline body carries a deterministic dedup marker (§4).

```bash
gh api --method POST "repos/<owner>/<repo>/pulls/<n>/reviews" --input - <<'JSON'
{
  "event": "COMMENT",
  "body": "<!-- corgi-review -->\n<human summary>",
  "comments": [
    {
      "path": "src/foo.ts",
      "line": 42,
      "side": "RIGHT",
      "body": "<!-- corgi-review:src/foo.ts:42 -->\nOff-by-one: stop at `len-1`.\n```suggestion\n  for (let i = 0; i < len - 1; i++) {\n```"
    }
  ]
}
JSON
```
Multi-line suggestion target: add `"start_line": <m>, "start_side": "RIGHT"` (range m..line).

**Update on re-run** (don't PATCH a review body — that route doesn't exist):
```bash
# edit an existing INLINE comment (note: pulls/comments, NOT pulls/<n>/comments):
gh api --method PATCH "repos/<owner>/<repo>/pulls/comments/<comment_id>" -f body='...'
# replace the SUMMARY: submit a fresh review, or update the review body:
gh api --method PUT "repos/<owner>/<repo>/pulls/<n>/reviews/<review_id>" -f body='...'
```

---

## 3. GitLab — post summary + inline suggestions

### 3a. Native flags (primary — simplest, no diff_refs/position needed)

`glab mr note create` posts diff comments directly. `-m` omitted → **reads the body
from stdin** (so a multi-line summary pipes in cleanly; `--message -` would post a
literal `-`). `--unique` skips posting if an identical body already exists.

```bash
# summary (stdin body; --unique = built-in idempotency)
printf '%s\n%s' '<!-- corgi-review -->' '<human summary>' \
  | glab mr note create <n> -R <repo> --unique

# inline on a NEW-side line (suggestion uses ```suggestion:-0+0 for the Apply button)
glab mr note create <n> -R <repo> --file src/foo.py --line 42 \
  -m '<!-- corgi-review:src/foo.py:42 -->
Off-by-one here.
```suggestion:-0+0
  for i in range(len - 1):
```'

# inline on a REMOVED line → --old-line ; multi-line range → --line 10:15
glab mr note create <n> -R <repo> --file src/foo.py --old-line 7 -m '<!-- corgi-review:src/foo.py:7 -->
Why was this removed?'
```
`--file/--line` are EXPERIMENTAL (may be unstable). If they error, use §3b.

### 3b. Raw discussions API (fallback)

Needs the URL-encoded project path in the URL (glab does **not** fill `:id` from
`-F id=`; it resolves placeholders from the cwd repo, which is wrong here) and
`--hostname` for self-hosted. `diff_refs` come from §1's `glab mr view -F json`.

```bash
glab api --method POST \
  "projects/<group>%2F<proj>/merge_requests/<n>/discussions" \
  --hostname <host> --input - <<'JSON'
{
  "body": "<!-- corgi-review:src/foo.py:42 -->\nOff-by-one here.\n```suggestion:-0+0\n  for i in range(len - 1):\n```",
  "position": {
    "position_type": "text",
    "base_sha": "<base_sha>", "head_sha": "<head_sha>", "start_sha": "<start_sha>",
    "new_path": "src/foo.py", "new_line": 42
  }
}
JSON
```
Removed-line: use `"old_path"` + `"old_line"` instead of `new_path`/`new_line`.

---

## 4. Idempotency + posting fallbacks

- **Deterministic dedup marker.** Every inline body starts with
  `<!-- corgi-review:<file>:<line> -->`; the summary with `<!-- corgi-review -->`.
  Dedup on the MARKER, never on the (LLM-generated, unstable) finding title.
- **Skip duplicates before posting.** List existing comments and skip any whose
  body already carries the matching marker:
  ```bash
  gh api repos/<owner>/<repo>/pulls/<n>/comments -q '.[].body'
  glab api "projects/<group>%2F<proj>/merge_requests/<n>/discussions" --hostname <host> -q '.[].notes[].body'
  ```
  GitLab native posting can also pass `--unique` as a backstop.
- **Finding not in the diff** → can't inline → append to the summary body.
- **Suggestion can't apply** (pure deletion, non-contiguous range, lines don't line
  up) → post a normal inline comment with the proposed code in a **plain fenced
  block** (NOT ` ```suggestion `) so there's no broken Apply button.
- **Head moved:** re-fetch metadata (§1) before posting; if `headRefOid`/`head_sha`
  changed, re-fetch the new diff and relocate each finding by its **anchored source
  line text + surrounding hunk context** → take the new line number/side; if that
  exact line content is absent from every new-head hunk, treat it as vanished and
  fold into the summary. Never post inline against a stale SHA.
- **Permission/size/rate limits:** `403` (not a collaborator) → print the review
  locally instead of posting; GitHub review body caps ~65k chars → split overflow
  into a follow-up comment; GitLab posts one note per discussion → throttle with
  backoff.

---

## Context

- Cited by review SKILL.md P0 (sibling enum), P1 (fetch), P5 (post). Command-focused.
- The hidden markers `<!-- corgi-review -->` (summary) and
  `<!-- corgi-review:<file>:<line> -->` (inline) MUST appear exactly — they are the
  idempotency keys.
