# GitLab CI

corgi ships a GitLab include, so most of this file is wiring rather than YAML to
copy. Replace the runner tags, the secrets source, and the participating repos
with the workspace's real ones.

## Workspace repo — the implementation

`stack-e2e.yml` in the repo that holds `corgi-compose.yml`, included by the
service repos:

```yaml
spec:
  inputs:
    branch:
      description: Branch name to look for in every service repo
---

include:
  # Pin a tag, not main: an include is fetched fresh on every pipeline.
  - remote: https://raw.githubusercontent.com/Andriiklymiuk/corgi/v1.20.17/gitlab/corgi.yml
    inputs:
      corgi_version: "1.20.17"
      branch: $[[ inputs.branch ]]
      runner_tags: [vm]          # the workspace's shell / docker+machine runner
      wait_timeout: 20m
      job_timeout: 45m
  # Generated: corgi cache paths --gitlab --out .gitlab/corgi-cache.yml
  - local: .gitlab/corgi-cache.yml

stack-e2e:
  extends: [.corgi-stack-e2e, .corgi-cache]
  before_script:
    - !reference [.corgi-setup, before_script]
    # Whatever the stack needs that corgi does not provision: env files, extra
    # CLIs (supabase, maestro), a language runtime.
    - mkdir -p env/source && printf '%s' "$API_ENV" > env/source/api.env

# Fails the pipeline the moment the committed cache plan stops matching the
# compose file. Without it the generated file quietly goes stale.
corgi-cache-drift:
  extends: .corgi-setup
  script:
    - corgi cache paths --gitlab --check .gitlab/corgi-cache.yml
```

`.corgi-stack-e2e` already does: `corgi init --depth 1 --feature`, `corgi run
--detach --wait --follow`, the `aborting beforeStart` grep, `corgi status
--json`, `corgi test --e2e --artifacts-dir`, and an always-executed `corgi logs
--dump` + artifact upload. Do not re-write those steps; override an input.

## Service repo — the caller

`.gitlab-ci.yml` in each participating repo:

```yaml
include:
  - project: your-group/your-workspace-repo
    ref: main
    file: stack-e2e.yml
    inputs:
      branch: $CI_COMMIT_REF_NAME

stack-e2e:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
```

## Notes

- **Runner executor matters more than on GitHub.** The `docker` executor runs
  the job inside a container; the stack's database containers then publish to a
  *different* localhost than the job sees. This is the number one reason a
  GitLab port fails in a way that looks like "the api can't reach postgres".
  `.corgi-setup` now detects this and fails immediately with that explanation,
  but the fix is still yours: a `shell` runner on a VM, or `docker+machine`.
  `allow_container: true` overrides the guard — only when the runner genuinely
  shares the namespace.
- `CI_JOB_TOKEN` can clone sibling projects when each grants the calling project
  under **Settings → CI/CD → Job token permissions**. Otherwise use a group
  access token.
- `interruptible: true` (already set) plus the project's auto-cancel setting is
  GitLab's equivalent of GitHub's `concurrency` group.
- `GIT_DEPTH: "1"` shallow-clones the *caller*; `corgi init --depth 1` handles
  the service repos.

## Caching

Never hand-write the path list. Generate it, commit it, and guard it:

```bash
corgi cache paths --gitlab --out .gitlab/corgi-cache.yml     # regenerate after any service change
corgi cache paths --gitlab --check .gitlab/corgi-cache.yml   # in the pipeline
```

Three GitLab constraints are already handled by the generator, and are worth
knowing before someone "fixes" the output by hand:

- **Nothing outside the project directory can be cached.** `~/.npm`,
  `~/.cache/pip` and friends are redirected into `$CI_PROJECT_DIR/.corgi-cache/`
  together with the environment variable that puts them there — which is why the
  job that *installs* must extend `.corgi-cache`, not just a warming job.
- **Four caches per job.** Past three ecosystems the tail is merged into one
  entry; the markers entry always survives.
- **Keys are branch-scoped, not hashed from lockfiles.** corgi clones the
  service repos during the job, so no lockfile exists when GitLab computes a
  `key:files`. `fallback_keys` start a new branch from the default branch's
  cache. A warm-but-stale restore is safe: corgi re-hashes each `cacheKey` and
  checks the dependency directory is really there before skipping an install.

If the job clones the workspace repo into a subdirectory, generate with
`--path-prefix <dir>` — GitLab resolves cache paths against the project root
only.
