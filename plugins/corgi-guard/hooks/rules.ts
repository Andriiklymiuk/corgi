// Each rule comes from a run that lost turns to it.

const unsplitSet = /\bset\s+--\s+"?\$(?!\{=)[A-Za-z_]\w*/
const rmCorgiTree = /\brm\s+-(?:[a-zA-Z]*r[a-zA-Z]*f|[a-zA-Z]*f[a-zA-Z]*r)[a-zA-Z]*\s+[^;&|]*\/tmp\/corgi-(?:wt|review)\b/

export function refusal(command: string): string | undefined {
  if (unsplitSet.test(command)) {
    return 'zsh does not split `$var` in `set -- $var`, so the second word arrives empty. ' +
      'Use `read -r repo n <<< "$pair"` (bash and zsh), or loop over `repo:n` and take `${p%%:*}` / `${p##*:}`.'
  }
  if (rmCorgiTree.test(command)) {
    return '`rm -rf` is refused in a hardened workspace. Take a fresh path instead: ' +
      '`W=/tmp/corgi-wt/<id>; [ -e "$W" ] && W="$W-$(date +%s)"`, and `git -C <repo> worktree remove "$W"` (no --force) when done.'
  }
  return undefined
}

export function hint(command: string, text: string): string | undefined {
  if (/graphql/.test(command) && /Could not coerce value|provided invalid value/.test(text)) {
    return 'A GraphQL variable arrived empty. For review threads, `corgi agent watch pr threads <url> <url>...` ' +
      'reads every open thread of every PR/MR with the thread id and reply-to id, GitHub or GitLab, in one call.'
  }
  if (/\bgh\s+pr\s+checks\b/.test(command) && /no checks reported/.test(text)) {
    return 'The checks are not registered yet right after a push. Wait for them inside one background command: ' +
      '`until [ "$(gh pr checks <n> --json name -q length 2>/dev/null)" -gt 0 ] 2>/dev/null; do sleep 10; done; gh pr checks <n> --watch --fail-fast`.'
  }
  if (/\bcorgi\b.*\bchat\b/.test(command) && /token_revoked|invalid_auth|account_inactive/.test(text)) {
    return 'The stored Slack token is dead, so nothing was posted. Tell the user in one line, with the text to paste themselves, ' +
      'and the fix: `corgi agent watch auth slack --token <new token>` (`corgi agent doctor` names which token).'
  }
  return undefined
}
