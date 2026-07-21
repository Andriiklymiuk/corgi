---
description: Build (or fix) a CI job that boots the whole corgi stack and runs cross-repo e2e against the branches under review. Pass what you want in plain words (e.g. "GitHub Actions for the full stack", "e2e across api + web on the PR branch", "why does each repo pass but the combination break"); no args = generate the pipeline for this workspace.
---

Run the corgi **ci** flow for the request in `$ARGUMENTS`.

- `$ARGUMENTS` = plain-words description (which CI provider, which repos take part,
  blocking gate or report-only, an existing job that misbehaves). Empty → generate
  a full-stack e2e pipeline for this workspace.
- Must run **inside the stack folder** (the one with `corgi-compose.yml`). Not
  there → tell the user to open it first.

Follow the `ci` skill (`plugins/corgi/skills/ci/SKILL.md`) end to end: probe the
installed corgi for the flags you intend to emit, read `corgi-compose.yml` for the
real service and `required:` lists, settle **where CI gets its env files** before
writing any YAML, then generate the workspace-repo implementation plus the thin
per-service-repo caller from `references/github-actions.md` or
`references/gitlab-ci.md`.

Honor every guardrail: never emit a job that runs inside a container, always dump
logs in an always-executed step, always bound the health wait with a timeout, and
never invent a corgi flag the installed binary does not have — fall back per
`references/fallbacks.md` or bump corgi instead.

State the per-run cost (wall clock, and that every participating PR triggers it)
and show the generated YAML before committing. A pipeline that has not been run
once is not finished — say so plainly rather than reporting it as done.
