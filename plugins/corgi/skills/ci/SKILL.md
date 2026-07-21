---
name: ci
description: Use when the user wants the whole corgi stack running inside CI, or end-to-end tests that span several repos — "run the stack in CI", "e2e across repos", "test the api + web branches together", "full-stack e2e on the PR", "GitHub Actions/GitLab CI for the whole stack", "why does each repo's CI pass but the combination break", "cross-repo integration test". Generates the pipeline (GitHub Actions or GitLab CI), wires per-repo PRs into one full-stack run via a shared branch name, gates on health, and always uploads logs + screenshots. NOT for authoring corgi-compose.yml (corgi skill), starting a stack locally (run skill), diagnosing an already-broken stack (debug skill), or reviewing PRs (review skill).
---

# Corgi in CI

Each repo's own pipeline proves that repo. A change spanning repos — a schema
field, a new event, a template the frontend reads — leaves every pipeline green
while the combination is broken. This skill builds the job that boots the whole
stack from the branches under review and drives real e2e against it.

## Before writing anything

1. **`corgi run --help | grep -E 'feature|wait'`** and **`corgi init --help | grep depth`**.
   Missing → the installed corgi predates these flags. Do **not** invent them; either
   bump corgi, or fall back to the shell equivalents in `references/fallbacks.md`.
2. **Read `corgi-compose.yml`.** Count `db_services` (each is containers + disk) and
   services. Note every `required:` tool and which are human-only.
3. **Find where secrets come from.** `copyEnvFromFilePath:` points at files that are
   almost always gitignored. CI has none of them. This is the single most common
   reason a first attempt never boots — settle it before writing YAML.
4. **Ask which repos participate** if the workspace has more than a handful, and
   whether the job blocks merge or only reports.

## The shape

One implementation, living in the workspace repo (the one holding
`corgi-compose.yml`); each service repo calls it from its own PR.

```
service repo PR ──► reusable workflow in the workspace repo
                      1. checkout workspace + install corgi
                      2. restore caches
                      3. corgi init --depth 1
                      4. corgi run --feature "$BRANCH" --detach --wait --timeout
                      5. corgi status --json          (gate)
                      6. run the e2e suite
                      7. ALWAYS: corgi logs --dump, upload artifacts
```

`--feature` is the cross-repo hinge: pass the PR's branch name once and every repo
that carries that branch joins the run from a worktree, while the rest stay on
their default checkout. A repo without the branch is not an error. This assumes
the team shares one branch name per change (the usual tracker-key convention) —
**confirm that before relying on it**; if branch names differ per repo, use
explicit `--service-branch <svc>=<branch>` pairs resolved from an explicit
manifest instead.

## Non-negotiables

- **Never run the job inside a container** (`jobs.<id>.container:` on GitHub,
  `image:` with dind on GitLab). The database containers publish to `localhost`,
  which is exactly what every generated connection string assumes; a containerised
  job no longer shares that. Run steps on the VM/shell runner.
- **`corgi logs --dump` in an always-executed step** (`if: always()` /
  `when: always`). The logs matter precisely when the job failed.
- **Bound the wait** (`--wait --timeout 20m`) so a wedged service fails the job
  instead of burning the runner's whole budget.
- **Free disk before booting** on hosted runners. A full stack is several GB of
  images plus every service's dependencies; hosted runners are provisioned tighter
  than that, and the failure mode when it runs out is unrecognisable as a disk
  problem.
- **Pin tool versions** — the corgi version, and any CLI a driver shells out to
  (the supabase CLI, for one). Drift silently changes ports and generated keys.

## Caching

Both halves or neither:

| restore | why |
|---------|-----|
| each service's dependency dir (`node_modules`, `.venv`, …) | the actual saving |
| `corgi_services/.cache/` | corgi's `beforeStart` cacheKey markers |

Markers without the dependency dir make corgi skip an install that is genuinely
needed — a service then starts with nothing installed. Key both on the lockfiles.
Requires a `cacheKey:` on the install step:

```yaml
beforeStart:
  - run: npm ci
    cacheKey: [package-lock.json]
```

Worktrees from `--feature` get their own marker scope automatically, so they never
inherit the main checkout's markers.

Package-manager caches (`~/.npm`, `~/.bun/install/cache`, `~/.cache/uv`) are worth
restoring too and are cheap. Docker image tarballs usually are **not** — they eat
the whole cache budget for a saving comparable to just pulling. If image pulls
measurably dominate, mirror them to a registry near the runner instead.

## Tools a human needs but CI doesn't

Mark them in `corgi-compose.yml` rather than special-casing preflight in the job:

```yaml
required:
  ngrok:
    why: [public URL for webhooks during local development]
    skipInCi: true
```

corgi already detects `CI`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`,
`BUILDKITE`, `JENKINS_URL`, `TEAMCITY_VERSION`, `TRAVIS`, `DRONE`,
`BITBUCKET_BUILD_NUMBER`, `CODEBUILD_BUILD_ID` — no `--ci` flag needed.

## Writing the pipeline

Generate into the workspace repo, then a thin caller per service repo. Templates:
`references/github-actions.md`, `references/gitlab-ci.md`. Both are starting
points — adapt them to the workspace's real service list, secrets source, and e2e
runner rather than pasting verbatim.

Show the generated YAML and the per-repo caller before committing, and say plainly
what it will cost per run (wall clock, and that every participating PR triggers
it).

## What e2e can actually reach

Worth telling the user up front, because it decides how much the job is worth:

- **Anything the stack contains is fair game** — including mail, if a driver
  provides a local SMTP sink. A real send → real capture → real click-through is
  reachable without any external provider.
- **Anything requiring an inbound public URL is not** (webhook callbacks), unless
  the provider runs as a container in the stack or is stubbed. Tunnels are not a CI
  answer.
- **Anything costing money or rate-limited per call** (third-party model APIs)
  should be flag-disabled or stubbed, not called for real on every PR.

## Verify before claiming it works

A pipeline that has never run is not done. Push the branch, watch one real run,
and read `corgi status --json` plus the dumped logs from that run. Report what
actually happened — including which stages were skipped and why — rather than the
intent of the YAML.
