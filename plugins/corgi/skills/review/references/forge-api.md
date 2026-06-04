# Forge API Reference — gh / glab incantations

Copy-pasteable commands for fetching PR/MR data and posting review output.
Cited by SKILL.md phases P1 (fetch) and P5 (post).

---

## 1. Fetch (read-only, no checkout)

```bash
# GitHub
gh pr view <n> --repo <owner>/<repo> --json title,body,author,baseRefName,headRefName,state,isDraft,files,url
gh pr diff <n> --repo <owner>/<repo> --patch           # unified diff with hunk headers
# head SHA for anchoring inline comments:
gh pr view <n> --repo <owner>/<repo> --json headRefOid -q .headRefOid

# GitLab  (PROJECT = URL-encoded path, e.g. group%2Fsubgroup%2Fproj, or numeric id)
glab mr view <n> --repo <host>/<group>/<proj>                       # metadata incl. state, draft
glab mr diff <n> --repo <host>/<group>/<proj> --color=never        # diff
# SHAs to anchor inline discussions:
glab api "projects/:id/merge_requests/<n>" -F id=<PROJECT> -q '.diff_refs'   # base_sha/head_sha/start_sha
```

Note: `gh pr diff`/`glab mr diff` diff each PR/MR against **its own base branch**. For a stacked PR whose base is the trunk but which contains another PR's commits, compute the non-shared commit range before reviewing (see SKILL P1).

---

## 2. GitHub — post summary + inline suggestions in one review

Build a JSON payload and pipe via `--input -`. Each suggestion is a fenced ` ```suggestion ` block inside the comment body. `line` is the line in the **new** file; `side: RIGHT` = added/context line, `side: LEFT` = removed line.

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
      "body": "Off-by-one: loop should stop at `len-1`.\n```suggestion\n  for (let i = 0; i < len - 1; i++) {\n```"
    }
  ]
}
JSON
```

Multi-line suggestion target: add `"start_line": <m>, "start_side": "RIGHT"` (range m..line).

---

## 3. GitLab — summary note + inline discussions

```bash
# summary (mark with hidden tag for idempotency)
printf '%s' "<!-- corgi-review -->
<human summary>" | glab mr note create <n> --repo <host>/<group>/<proj> --message -

# inline discussion on a specific line (suggestion uses ```suggestion:-0+0 for GitLab Apply button)
glab api --method POST "projects/:id/merge_requests/<n>/discussions" -F id=<PROJECT> --input - <<'JSON'
{
  "body": "Off-by-one here.\n```suggestion:-0+0\n  for i in range(len - 1):\n```",
  "position": {
    "position_type": "text",
    "base_sha": "<base_sha>",
    "head_sha": "<head_sha>",
    "start_sha": "<start_sha>",
    "new_path": "src/foo.py",
    "new_line": 42
  }
}
JSON
```

For a line that only exists on the old side, use `"old_path"` + `"old_line"` instead of `new_path`/`new_line`.

---

## 4. Posting fallbacks + idempotency

- A finding whose line is **not in the diff** can't be inlined → append it to the summary body instead.
- A suggestion that can't apply (pure deletion, non-contiguous range) → use a **plain fenced code block** in a normal inline comment, NOT a ` ```suggestion ` block (avoids a broken Apply button).
- **Head moved:** re-fetch the head SHA (commands in section 1) right before posting; if it changed since the review, re-anchor or skip stale inline comments — never post inline against a stale SHA.
- **Permission/size/rate limits:** a `403` (not a collaborator) → print the review locally instead of posting; GitHub review body caps ~65k chars → split overflow into a follow-up comment; GitLab posts one call per discussion → throttle with backoff.
- Before posting inline, list existing review comments (`gh api repos/<o>/<r>/pulls/<n>/comments`, `glab api projects/:id/merge_requests/<n>/discussions -F id=<PROJECT>`), filter those containing `<!-- corgi-review -->` or matching `(path,line)`, and skip duplicates.
- Re-run with an existing `<!-- corgi-review -->` summary → ask update vs new (`gh api --method PATCH .../comments/<id>` / edit the GitLab note) instead of duplicating.

---

## Context

- This reference is cited by the review SKILL.md phases P1 (fetch) and P5 (post). Keep it command-focused.
- The hidden marker string `<!-- corgi-review -->` must appear EXACTLY as written (used for idempotency dedup elsewhere).
