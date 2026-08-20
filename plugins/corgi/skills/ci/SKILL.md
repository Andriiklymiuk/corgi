---
name: ci
description: Use when the user wants the whole corgi stack running inside CI, or end-to-end tests that span several repos — "set up CI for this workspace", "add CI", "init the pipeline", "wire this up to GitHub Actions/GitLab CI", "run the stack in CI", "e2e across repos", "test the api + web branches together", "full-stack e2e on the PR", "why does each repo's CI pass but the combination break", "cross-repo integration test". Asking in chat is the whole interface: this scaffolds with `corgi ci init` and then does the workspace-specific wiring the generated file cannot know. Generates the pipeline (GitHub Actions or GitLab CI), wires per-repo PRs into one full-stack run via a shared branch name, gates on health, and always uploads logs + screenshots. NOT for authoring corgi-compose.yml (corgi skill), starting a stack locally (run skill), diagnosing an already-broken stack (debug skill), or reviewing PRs (review skill).
---

# Corgi in CI

Each repo's own pipeline proves that repo. A change spanning repos — a schema
field, a new event, a template the frontend reads — leaves every pipeline green
while the combination is broken. This skill builds the job that boots the whole
stack from the branches under review and drives real e2e against it.

## Setting it up from chat

"Set up CI for this workspace" is the whole request. Do this, in order:

1. **`corgi ci init`** — scaffolds the pipeline for the forge in the git remote
   (`--provider github|gitlab` to force it, `--force` to replace an existing
   file). GitHub gets `.github/workflows/stack-e2e.yml`; GitLab gets
   `.gitlab-ci.yml` plus the generated `.gitlab/corgi-cache.yml`. It prints what
   the workspace still owes.
2. **Then do the wiring it cannot know**, which is the rest of this skill:
   - where the secrets come from (`copyEnvFromFilePath` — see below, it is the
     usual reason a first run never boots)
   - which repos take part, and the branch-name convention
   - `cacheKey:` on each install step — run `corgi cache paths`, it names the
     steps that could opt in and the lockfile for each
   - an `e2e:` block if the compose has none, or drop the `corgi test --e2e` step
   - GitLab only: a shell or VM-backed runner tag, and job-token permissions
     between the projects
3. **Show the generated files and the edits before committing**, and say what a
   run will cost (wall clock, and that every participating PR triggers it).

If the workspace predates `corgi ci init` (< 1.20.32), write the files by hand
from `references/github-actions.md` / `references/gitlab-ci.md` instead.

## Before writing anything

1. **`corgi run --help | grep -E 'feature|wait'`** and **`corgi init --help | grep depth`**.
   Missing → the installed corgi predates these flags. Do **not** invent them; either
   bump corgi, or fall back to the shell equivalents in `references/fallbacks.md`.
   Also check `corgi test --help | grep e2e` and `corgi cache --help` — recent corgi
   adds `corgi test --e2e` (runs the compose's `e2e:` block against the live stack)
   and `corgi cache paths` (derives the CI cache plan from the compose file).
   `corgi env check --help` working means the env-drift gate below is available too.
2. **Read `corgi-compose.yml`.** Count `db_services` (each is containers + disk) and
   services. Note every `required:` tool and which are human-only. Check for a
   top-level `e2e:` block — if there is one, the e2e step is `corgi test --e2e`,
   not a hand-written npm invocation; if there isn't, offer to add one so local
   runs and CI share the same entry point.
3. **Find where secrets come from.** `copyEnvFromFilePath:` points at files that are
   almost always gitignored. CI has none of them. This is the single most common
   reason a first attempt never boots — settle it before writing YAML.
   A CI env file usually needs no real secrets at all: placeholders plus the
   third-party feature flags off (an empty key is often the off switch), and a
   local sink for anything like mail — which lets each repo commit its CI env
   file instead of holding it in the forge's secret store. `corgi env check`
   makes completeness a failing step, not a first-request surprise: it diffs
   each service's env file against the `.env-example` its repo commits,
   excluding every key corgi generates; `--file .env.ci` validates a committed
   CI env file before anything copies it into place.
4. **Ask which repos participate** if the workspace has more than a handful, and
   whether the job blocks merge or only reports.

## The shape

One implementation, living in the workspace repo (the one holding
`corgi-compose.yml`); each service repo calls it from its own PR.

```
service repo PR ──► reusable workflow in the workspace repo
                      1. checkout workspace + install corgi
                         (GitHub: uses: Andriiklymiuk/corgi@v1 — verified install
                          + cache-paths/cache-key/cache-groups outputs)
                      2. restore caches (from the action outputs / corgi cache paths)
                      3. corgi init --depth 1
                      4. corgi run --feature "$BRANCH" --detach --wait --wait-timeout
                      5. corgi status --json          (gate)
                      6. corgi test --e2e             (or the suite's own command)
                      7. ALWAYS: corgi logs --dump, upload artifacts
```

`--feature` is the cross-repo hinge: pass the PR's branch name once and every repo
that carries that branch joins the run from a worktree, while the rest stay on
their default checkout. A repo without the branch is not an error. This assumes
the team shares one branch name per change (the usual tracker-key convention) —
**confirm that before relying on it**; if branch names differ per repo, use
explicit `--service-branch <svc>=<branch>` pairs resolved from an explicit
manifest instead.

## The failures that actually happen

Every one of these was found the expensive way — a 20-30 minute cycle each, some
several. Check them before writing a line, not after a red run.

**A health check that is polled must never do work.** This is the big one. An
Expo dev server builds a bundle per request and Vite pre-bundles dependencies on
first hit, so polling `/` queues dozens of concurrent builds on a 4-core runner:
single-module bundles took two minutes and the stack never converged. The probing
*causes* the unhealthiness. Give such services a **port probe** for readiness
(omit `healthCheck:`) and check that they really serve exactly once, with one
patient request, before the tests run — the server holds the connection until it
is ready, so retrying only queues more work.

**Run `corgi doctor` between `corgi init` and `corgi run`.** In CI it adds two
checks it stays silent about locally: the job running inside a container, and
any `copyEnvFromFilePath` the runner does not have — the latter otherwise boots
the service against a committed `.env-example` whose placeholder values fail at
the first request, thousands of lines from the cause. Both cost twenty minutes
each time they are found the hard way.

**A failed `beforeStart` fails the run — do not grep the logs for it.** corgi
still lets the rest of the stack come up, but `corgi run --wait` returns the
failure immediately instead of waiting out the readiness timeout, and a run
without `--wait` now exits non-zero. Older pipelines carry a
`grep -rh "aborting beforeStart" corgi_services/.logs/ && exit 1` step from
when that was not true; it can never fire after `--wait` and should be deleted.
Requires corgi ≥ 1.20.10 for the `--wait` half, ≥ 1.20.32 for the exit status.

**A compose that works on macOS can die on Linux.** `/bin/sh` is bash-like on
macOS and dash on Linux (fixed in corgi 1.20.9, which prefers bash — but a repo
pinning an older corgi still needs POSIX `.` rather than `source`).

**Env vars validated at construction crash the service, not the request.** An
empty credential threw inside a mail transport's constructor, so the service
never started and readiness polled forever — an empty value is not the same as
an unused one. Before trusting a CI env file, grep the services for startup
validation: `grep -rn "is required" <service>/src`.

**Node version decides npm version.** Node 22 ships npm 10.x; a repo requiring
`npm >= 11.5.0` fails `npm ci` with `EBADENGINE`. Node 24 bundles npm 11.

**`set -e` applies inside a trap.** `wait` on a child you just killed returns 143,
which aborts the handler — so a *successful* run exits 143. Every cleanup command
needs its own `|| true`. Background a tail directly, never in a subshell, or the
pid you recorded is the subshell's and the real tail keeps the step's stdout open
until the step times out.

**The dependency cache only saves on a successful job.** Until one run goes green
you pay every install every time — do not read early runs as the steady state.

**When every service goes silent at once, it is memory, not the tests.** A full
stack of containers next to several dev servers can exceed a hosted runner's few
GB, and the failure reads as the runner losing contact or a container `Killed` —
never as an OOM message. Print free memory before the boot, cap any test
runner's worker count (a per-CPU default can OOM a shared runner by itself),
and move to a larger runner before chasing ghost failures.

**A headless browser-driving runner can silently drop its screenshots.** One
suite's in-flow screenshot command is a documented no-op under its headless
mode — proven only by counting zero images after a red run. Verify one artifact
actually lands before trusting the pipeline's evidence; collect the runner's
own debug directory as a fallback, or run headed under a virtual display
(xvfb) when the screenshots are the point.

## Non-negotiables

- **Never run the job inside a container** (`jobs.<id>.container:` on GitHub,
  `image:` with dind on GitLab — the shipped `.corgi-setup` fails fast on this,
  but the runner still has to be replaced). The database containers publish to `localhost`,
  which is exactly what every generated connection string assumes; a containerised
  job no longer shares that. Run steps on the VM/shell runner.
- **Assert a corgi version floor**, not the presence of individual flags:
  `corgi version --json | jq -r .version`, compared with `sort -V`. Naive string
  comparison gets `1.20.9` vs `1.20.10` backwards.
- **`corgi logs --dump` in an always-executed step** (`if: always()` /
  `when: always`). The logs matter precisely when the job failed.
- **Bound the wait, and the step.** `--wait-timeout` only covers the readiness
  wait — `beforeStart` (installs, migrations, builds) runs before that timer
  starts and is unbounded. Put a `timeout-minutes` on the step too, or a slow
  boot quietly eats the whole job.
- **`corgi doctor` checks disk headroom in CI** — it compares free space with a
  rough estimate from the database and service counts, because running out
  mid-boot surfaces as a random service failing to build, never as a disk
  message. Free disk before booting on hosted runners. A full stack is several GB of
  images plus every service's dependencies; hosted runners are provisioned tighter
  than that, and the failure mode when it runs out is unrecognisable as a disk
  problem.
- **Pin tool versions** — the corgi version, and any CLI a driver shells out to
  (the supabase CLI, for one). Drift silently changes ports and generated keys.

## Caching

Do not hand-write the path list — `corgi cache paths` derives it from the compose
file (every `beforeStart` cacheKey's dependency dir + `corgi_services/.cache/`),
so it cannot drift as services come and go. `--key` prints the matching cache key,
`--json` splits the plan per ecosystem (`groups: [{id, key, paths, pathsText}]`)
so one lockfile change doesn't evict every other language's packages. On GitHub
the `Andriiklymiuk/corgi@v1` action exposes all of this as step outputs
(`cache-paths`, `cache-key`, `cache-groups`) ready to feed `actions/cache`, plus
four fixed slots (`cache-1-key`/`cache-1-paths`/`cache-1-restore-keys` …
`cache-4-*`) so a workflow writes four plain cache steps instead of
`fromJSON(...)[i]` indexing. The `restore-keys` slot is the group's
`corgi-deps-<ecosystem>-` prefix, so a lockfile change restores the previous
packages instead of starting empty; the markers slot stays exact-match on
purpose. GitHub also scopes caches per ref — a fresh PR restores only what its
base branch saved, so a scheduled default-branch run is what keeps new PRs
warm. Neither a
workflow expression nor a composite action can loop, so the copies stay — but
the action warns by itself when an ecosystem does not fit, which used to be a
hand-written step.

**`corgi cache paths` now says when caching is off.** A workspace where no
service declares a `cacheKey` gets a list of the install steps that could opt
in, on stderr, naming the lockfile for each. Silence used to be the only signal
that every CI run was reinstalling everything.

GitLab cannot read the plan mid-run — its cache config is static YAML — so
generate it, commit it, and guard it:

```bash
corgi cache paths --gitlab --out .gitlab/corgi-cache.yml
corgi cache paths --gitlab --check .gitlab/corgi-cache.yml   # a job in the pipeline
```

The generator handles what GitLab will not: home caches are redirected into the
project (`cache:paths` cannot leave it), the entries are capped at four, and the
keys are branch-scoped rather than hashed from lockfiles that corgi only clones
mid-job. Details in `references/gitlab-ci.md`.

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

**`corgi ci init` writes the starting point.** It picks the forge from the git
remote (`--provider github|gitlab` to force it), writes
`.github/workflows/stack-e2e.yml` or `.gitlab-ci.yml` +
`.gitlab/corgi-cache.yml`, refuses to clobber an existing file without
`--force`, and prints what the workspace still has to supply — runner tags,
the clone token, the env files, and an `e2e:` block when the compose has none.
Start there, then adapt; the references below are what it generates.

Generate into the workspace repo, then a thin caller per service repo. Templates:
`references/github-actions.md`, `references/gitlab-ci.md`. On GitHub the install
and cache plan come from the `Andriiklymiuk/corgi@v1` action; on GitLab from
`include: remote: .../gitlab/corgi.yml`, which provides `.corgi-setup` and
`.corgi-stack-e2e` — extend those rather than rewriting the steps. Both are starting
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

Two suites, one entry point: the merge gate runs against the stack built from
the branches under review; a scheduled run points the *same* suite — same
command, same env names, only the base URL differs — at the deployed
environment. Keep one runner script for local and CI so they cannot drift, and
make it print every scenario as pass/fail/skip: a quarantined test hidden
inside a green pass count is coverage you no longer have. A deployed
environment can lag the default branch — before debugging a red scheduled run,
check the change under test has actually shipped there.

## Verify before claiming it works

A pipeline that has never run is not done. Push the branch, watch one real run,
and read `corgi status --json` plus the dumped logs from that run. Report what
actually happened — including which stages were skipped and why — rather than the
intent of the YAML.
