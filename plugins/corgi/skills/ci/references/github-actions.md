# GitHub Actions

A starting point, not a drop-in. Replace the service list, the secrets source, and
the e2e command with the workspace's real ones.

## Workspace repo — the implementation

`.github/workflows/stack-e2e.yml` in the repo that holds `corgi-compose.yml`:

```yaml
name: Stack e2e

on:
  workflow_call:
    inputs:
      branch:
        description: Branch name to look for in every service repo
        required: true
        type: string
      corgi-version:
        required: false
        type: string
        default: "1.20.17"   # ≥1.20.13 for test --e2e / cache paths; ≥1.20.17 for cache-groups
    secrets:
      REPO_TOKEN:
        required: true

jobs:
  stack-e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@v4

      # Hosted runners ship far less free disk than a full stack needs.
      - name: Free disk space
        uses: jlumbroso/free-disk-space@main
        with:
          tool-cache: true
          android: true
          dotnet: true
          haskell: true

      # Checksum-verified install + a cache plan derived from the compose file
      # (cache-paths / cache-key / cache-groups outputs).
      - name: Install corgi
        uses: Andriiklymiuk/corgi@v1
        id: corgi
        with:
          version: ${{ inputs.corgi-version }}

      # Every service repo is private: let git use the token for all of them.
      - name: Authenticate git
        run: |
          git config --global url."https://x-access-token:${{ secrets.REPO_TOKEN }}@github.com/".insteadOf "https://github.com/"

      # The plan covers each service's dependency dir + corgi_services/.cache
      # (the beforeStart skip markers). On a polyglot stack, prefer one
      # actions/cache step per entry of steps.corgi.outputs.cache-groups so one
      # lockfile change doesn't evict every other language's packages.
      - name: Restore dependency caches
        uses: actions/cache@v4
        with:
          path: ${{ steps.corgi.outputs.cache-paths }}
          key: ${{ steps.corgi.outputs.cache-key }}
          restore-keys: corgi-deps-

      # Package-manager caches are outside the compose-derived plan but cheap.
      - name: Restore package-manager caches
        uses: actions/cache@v4
        with:
          path: |
            ~/.npm
            ~/.bun/install/cache
            ~/.cache/uv
          key: pm-${{ runner.os }}-${{ hashFiles('*/package-lock.json', '*/bun.lock', '*/uv.lock') }}
          restore-keys: pm-${{ runner.os }}-

      - name: Materialise env files
        env:
          # One secret per service is usually simpler than a secrets manager here.
          API_ENV: ${{ secrets.API_ENV }}
        run: |
          mkdir -p env/source
          printf '%s' "$API_ENV" > env/source/api.env

      - name: Clone service repos
        run: corgi init --depth 1

      - name: Boot the stack
        run: corgi run --feature "${{ inputs.branch }}" --detach --wait --timeout 20m

      - name: Health gate
        run: corgi status --json

      # Runs the compose file's e2e: block (workdir/install/run) against the
      # live stack. No e2e: block in the workspace? Fall back to the suite's own
      # command, e.g. `npm --prefix e2e ci && npm --prefix e2e test`.
      - name: e2e
        run: corgi test --e2e

      - name: Collect logs
        if: always()
        run: corgi logs --dump ./ci-logs || true

      - name: Upload artifacts
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: stack-e2e-${{ github.run_id }}
          path: |
            ci-logs/
            e2e/**/artifacts/
          retention-days: 7

      - name: Tear down
        if: always()
        run: corgi stop || true
```

## Service repo — the caller

`.github/workflows/stack-e2e.yml` in each participating repo:

```yaml
name: Stack e2e

on:
  pull_request:
    branches: [main]

concurrency:
  group: stack-e2e-${{ github.ref }}
  cancel-in-progress: true

jobs:
  stack-e2e:
    uses: your-org/your-workspace-repo/.github/workflows/stack-e2e.yml@main
    with:
      branch: ${{ github.head_ref }}
    secrets: inherit
```

## Notes

- `secrets: inherit` needs the secrets defined at the org (or each repo). A
  reusable workflow cannot read the *called* repo's secrets otherwise.
- The default `GITHUB_TOKEN` is scoped to the calling repo only. Cloning sibling
  private repos needs a GitHub App token or a PAT with org read — that is
  `REPO_TOKEN` above.
- Cache scope belongs to the **calling** repo, so each service repo warms its own.
  If that hit rate is too low, invert the design: fire `repository_dispatch` into
  the workspace repo so every run shares one cache, and report status back with the
  commit-status API.
- `concurrency` on the caller cancels superseded runs; without it every push to a
  PR starts another full stack.
