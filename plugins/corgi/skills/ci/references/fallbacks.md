# When the installed corgi predates the CI flags

Check first, always:

```bash
corgi run  --help | grep -E -- '--feature|--wait'
corgi init --help | grep -- '--depth'
corgi logs --help | grep -- '--dump'
```

Prefer bumping corgi over the fallbacks below — they are more code in the
pipeline, and each one loses something.

## No `--feature`

Resolve the branch per repo in the job and build `--service-branch` pairs. Needs
the service repos already cloned (`corgi init`), and the remotes reachable.

```bash
ARGS=()
for svc in api web notifications; do
  if git -C "$svc" ls-remote --exit-code --heads origin "$BRANCH" >/dev/null 2>&1; then
    git -C "$svc" fetch --no-tags --depth 1 origin "+refs/heads/$BRANCH:refs/remotes/origin/$BRANCH"
    git -C "$svc" branch -f "$BRANCH" "origin/$BRANCH"
    ARGS+=(--service-branch "$svc=$BRANCH")
    echo "$svc joins on $BRANCH"
  else
    echo "$svc stays on its default branch"
  fi
done
corgi run "${ARGS[@]}" --detach --wait --timeout 20m
```

Loses: the service list is now hardcoded in the pipeline and drifts from
`corgi-compose.yml`. Derive it from `corgi list --json` if that flag exists.

## No `--depth` on init

`corgi init` full-clones. On a big history that is minutes per repo. There is no
clean interception point — either accept it, or pre-clone each repo shallow into
its compose `path:` before calling `corgi init`, which then finds the dirs present
and skips cloning.

## No `logs --dump`

```bash
mkdir -p ci-logs
for d in corgi_services/.logs/*/; do
  svc=$(basename "$d")
  newest=$(ls -1t "$d" | head -1)
  [ -n "$newest" ] && cp "$d$newest" "ci-logs/$svc.log"
done
```

## No `skipInCi`

Preflight will fail on a tool CI has no use for. Either install a stub earlier in
the job, or drop the tool from `required:` and document why — do not disable
preflight wholesale, it is the check that produces a readable error instead of a
mysterious boot failure.

## No `--wait`

```bash
corgi run --detach
corgi status --ready --timeout 20m
```

`--ready` predates `--wait` and is equivalent for gating purposes.
