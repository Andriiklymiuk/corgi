#!/usr/bin/env bash
# Does the right corgi skill fire on its own? Each line: expected skill (or
# "none") and a prompt. Runs one turn with only the Skill tool allowed and
# reads the Skill call from the event stream (the json result has no tool
# calls). Needs a logged-in Claude Code.
set -euo pipefail
cd "$(dirname "$0")/.."
plugin="$(pwd)/plugins/corgi"
pass=0; fail=0
while IFS='|' read -r want prompt; do
  [[ -z "$want" || "$want" == \#* ]] && continue
  out=$(claude -p "$prompt" --max-turns 1 --allowedTools Skill --output-format stream-json --verbose --plugin-dir "$plugin" 2>/dev/null || true)
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
idea|ok we brainstormed the spec in docs/specs/checkout.md, give me a link for the team
idea|дай ссылку на эту спеку для команды, пусть комментируют
idea|Ok, give me link to doc we created
idea|give me a link so my friends can comment on the hld
idea|дай ссылку на документ, который мы сделали
idea|share this hld with my team for review
unslop|this app looks like AI slop, too many colours and cards, clean up the UI
unslop|this README reads like ChatGPT wrote it, make it sound human
quality|improve the code quality of this repo, it keeps breaking
improve-skill|retro: what went wrong in my last 30 sessions, fix what caused it
improve-skill|the stories skill should have caught that, fix the skill
summary|what's left, which PRs are still open
budget|how much budget do I have left
resume|where was I
tracker|sort the new issues in Linear and tell me what's blocked on the sprint
complexity|this function is a jungle, reduce the complexity
prep-pr|prep my PR
before-after|attach before and after screenshots to the PR
align|let's align first, this request could mean two things
factory|how do we get to a software factory here, what is our next step to run this unattended
none|what is the capital of France
CASES
echo "$pass passed, $fail failed"
[[ $fail -eq 0 ]]
