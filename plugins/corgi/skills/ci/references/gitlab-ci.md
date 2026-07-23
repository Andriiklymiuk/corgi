# GitLab CI

A starting point, not a drop-in. Replace the service list, the secrets source, and
the e2e command with the workspace's real ones.

## Workspace repo — the implementation

`stack-e2e.yml` in the repo that holds `corgi-compose.yml`, included by the others:

```yaml
spec:
  inputs:
    branch:
      description: Branch name to look for in every service repo
    corgi_version:
      default: "1.20.17"   # ≥1.20.13 for test --e2e / cache paths; ≥1.20.17 for cache-groups
    workspace_ref:
      default: main
---

stack-e2e:
  stage: test
  # A shell runner, or a VM-backed docker+machine runner. NOT a plain docker
  # executor: the db containers publish to localhost and the job must share it.
  tags: [vm]
  timeout: 45m
  interruptible: true
  variables:
    GIT_DEPTH: "1"
  before_script:
    - curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | bash -s -- v$[[ inputs.corgi_version ]]
    - corgi version
    - git config --global url."https://gitlab-ci-token:${CI_JOB_TOKEN}@${CI_SERVER_HOST}/".insteadOf "https://${CI_SERVER_HOST}/"
  script:
    - git clone --depth 1 --branch $[[ inputs.workspace_ref ]] "https://gitlab-ci-token:${CI_JOB_TOKEN}@${CI_SERVER_HOST}/${WORKSPACE_PROJECT_PATH}.git" workspace
    - cd workspace
    - mkdir -p env/source && printf '%s' "$API_ENV" > env/source/api.env
    - corgi init --depth 1
    - corgi run --feature "$[[ inputs.branch ]]" --detach --wait --wait-timeout 20m
    - corgi status --json
    # Runs the compose file's e2e: block against the live stack. No e2e: block?
    # Fall back to the suite's own command (npm --prefix e2e ci && npm --prefix e2e test).
    - corgi test --e2e
  after_script:
    - cd workspace && corgi logs --dump ../ci-logs || true
    - cd workspace && corgi stop || true
  # GitLab cache config is static YAML, so it cannot read the plan at runtime —
  # run `corgi cache paths` locally when authoring this job and mirror its list
  # here (each service's dependency dir + corgi_services/.cache).
  cache:
    key:
      files:
        - workspace/*/package-lock.json
    paths:
      - workspace/*/node_modules
      - workspace/corgi_services/.cache
      - .npm
  artifacts:
    when: always
    expire_in: 1 week
    paths:
      - ci-logs/
      - workspace/e2e/**/artifacts/
```

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

- **Runner executor matters more than on GitHub.** The `docker` executor runs the
  job inside a container; the stack's database containers then publish to a
  *different* localhost than the job sees. Use a `shell` runner on a VM, or
  `docker+machine`. This is the number one reason a GitLab port of this job fails
  in a way that looks like "the api can't reach postgres".
- `CI_JOB_TOKEN` can clone sibling projects when each grants the calling project
  under **Settings → CI/CD → Job token permissions**. Otherwise use a group access
  token.
- `interruptible: true` plus the project's auto-cancel setting is GitLab's
  equivalent of GitHub's `concurrency` group.
- `GIT_DEPTH: "1"` shallow-clones the *caller*; `corgi init --depth 1` handles the
  service repos.
- Cache keys are per-project by default. If hit rate matters more than isolation,
  set an explicit shared `key:` and accept the coupling.
