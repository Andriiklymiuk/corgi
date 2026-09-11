#!/usr/bin/env bash
# Does the right corgi skill fire on its own? Each line: expected skill (or
# "none") and a prompt. Runs one turn with only the Skill tool allowed and
# reads what was invoked. Needs a logged-in Claude Code; ~1 cent a line.
set -euo pipefail
cd "$(dirname "$0")/.."
plugin="$(pwd)/plugins/corgi"
pass=0; fail=0
while IFS='|' read -r want prompt; do
  [[ -z "$want" || "$want" == \#* ]] && continue
  out=$(claude -p "$prompt" --max-turns 1 --allowedTools Skill --output-format json --plugin-dir "$plugin" 2>/dev/null || true)
  got=$(printf '%s' "$out" | grep -oE '"skill"\s*:\s*"[^"]+"' | head -1 | sed -E 's/.*"([^"]+)"$/\1/' || true)
  got=${got:-none}
  if [[ "$got" == *"$want"* ]] || { [[ "$want" == none ]] && [[ "$got" == none ]]; }; then
    pass=$((pass+1)); printf '✓ %-10s %s\n' "$got" "$prompt"
  else
    fail=$((fail+1)); printf '✗ want %-10s got %-10s %s\n' "$want" "$got" "$prompt"
  fi
done <<'CASES'
stories|do ABC-123
stories|implement https://linear.app/acme/issue/ABC-9/limits and https://linear.app/acme/issue/ABC-10/banner
review|review https://github.com/acme/api/pull/42
run|run the stack with the tunnel and logs
debug|the api is down, what is wrong
setup|I just installed corgi, set everything up
agent|leave a handoff for ABC-7, the web side is not done
none|what is the capital of France
CASES
echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
