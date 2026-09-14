#!/usr/bin/env bash
# Turns one `corgi cache paths --json` into the outputs both composite actions
# publish: cache-paths, cache-key, cache-groups, four fixed cache-N-* slots,
# cache-overflow and cache-complete. Shared by action.yml (install, runs before
# `corgi init`) and cache/action.yml (runs after it, with --strict).
#
# Env: GITHUB_OUTPUT (required); CORGI_CACHE_STRICT=true adds --strict, so a
# plan hashed from cacheKey files that do not exist yet fails the step.
set -euo pipefail

args=(cache paths --json)
if [ "${CORGI_CACHE_STRICT:-}" = "true" ]; then
  args+=(--strict)
fi

# corgi's own stderr (the "using compose file" line, and the --strict failure
# naming the missing files) goes to the job log; only the JSON is captured.
plan="$(corgi "${args[@]}")"

{
  echo "cache-paths<<CORGI_ACTION_EOF"
  jq -r '.paths // [] | join("\n")' <<<"$plan"
  echo "CORGI_ACTION_EOF"
} >> "$GITHUB_OUTPUT"
echo "cache-key=$(jq -r '.key // ""' <<<"$plan")" >> "$GITHUB_OUTPUT"

groups="$(jq -c '.groups // []' <<<"$plan")"
echo "cache-groups=$groups" >> "$GITHUB_OUTPUT"

# Four fixed slots as well as the JSON. An Actions expression cannot loop, and
# neither can a composite action, so every workflow used to copy the same
# cache step four times with fromJSON(...)[i] indexing plus a hand-written
# "ran out of slots" warning. The slots keep the copies but make each one
# plain, and the warning moves in here.
total="$(jq 'length' <<<"$groups")"
for i in 1 2 3 4; do
  key="$(jq -r --argjson i "$i" '.[$i-1].key // ""' <<<"$groups")"
  echo "cache-$i-key=$key" >> "$GITHUB_OUTPUT"
  # `// ""` keeps this working against a corgi predating restorePrefix.
  restore="$(jq -r --argjson i "$i" '.[$i-1].restorePrefix // ""' <<<"$groups")"
  echo "cache-$i-restore-keys=$restore" >> "$GITHUB_OUTPUT"
  {
    echo "cache-$i-paths<<CORGI_ACTION_EOF"
    jq -r --argjson i "$i" '.[$i-1].pathsText // ""' <<<"$groups"
    echo "CORGI_ACTION_EOF"
  } >> "$GITHUB_OUTPUT"
done

overflow=$(( total > 4 ? total - 4 : 0 ))
echo "cache-overflow=$overflow" >> "$GITHUB_OUTPUT"
if [ "$overflow" -gt 0 ]; then
  echo "::warning::corgi found $total cache groups but the action publishes four slots; $overflow ecosystem(s) will not be cached. Use the cache-groups JSON if you need all of them."
fi

# `// true` would read a literal false as missing, so test for the field.
# A corgi predating the field reports nothing, and nothing is what it knew.
complete="$(jq -r 'if has("complete") then .complete else true end' <<<"$plan")"
echo "cache-complete=$complete" >> "$GITHUB_OUTPUT"
if [ "$complete" != "true" ]; then
  missing="$(jq -r '.missingFiles // [] | join(", ")' <<<"$plan")"
  echo "::warning::corgi cache keys were computed before the cacheKey files exist (missing: $missing). The key will not change when they do, so the cache never invalidates. Add \`uses: Andriiklymiuk/corgi/cache@v1\` after \`corgi init\` and feed the cache steps from its outputs, or run \`corgi cache paths --json\` there yourself."
fi
