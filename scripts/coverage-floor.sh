#!/usr/bin/env sh
# Fails if total statement coverage in the given profile drops below the floor.
# Usage: scripts/coverage-floor.sh [coverage.out]   (env: COVERAGE_FLOOR)
set -eu

PROFILE="${1:-coverage.out}"
# Floor sits a couple points under the current ~68.5% total so it enforces
# "don't regress" without being brittle. Override with COVERAGE_FLOOR to raise.
FLOOR="${COVERAGE_FLOOR:-67.0}"

if [ ! -f "$PROFILE" ]; then
  echo "coverage-floor: profile $PROFILE not found" >&2
  exit 1
fi

total=$(go tool cover -func="$PROFILE" | awk '/^total:/ {gsub(/%/,"",$3); print $3}')
if [ -z "$total" ]; then
  echo "coverage-floor: could not parse total coverage from $PROFILE" >&2
  exit 1
fi

# POSIX-safe float compare via awk.
if awk "BEGIN { exit !($total < $FLOOR) }"; then
  echo "coverage-floor: total $total% is below floor $FLOOR%" >&2
  exit 1
fi
echo "coverage-floor: total $total% >= floor $FLOOR% — ok"
