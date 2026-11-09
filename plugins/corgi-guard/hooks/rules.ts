const unsplitSet = /\bset\s+--\s+"?\$(?!\{=)[A-Za-z_]\w*/
const bashPipeStatus = /\$\{?PIPESTATUS\b/
const rmCorgiTree = /\brm\s+-(?:[a-zA-Z]*r[a-zA-Z]*f|[a-zA-Z]*f[a-zA-Z]*r)[a-zA-Z]*\s+[^;&|]*\/tmp\/corgi-(?:wt|review)\b/

const postsComment = /\b(?:gh\s+(?:pr|issue)\s+(?:comment|review)|gh\s+api\b[^\n]*comments|glab\s+(?:mr|issue)\s+note|corgi\s+agent\s+watch\s+(?:pr\s+)?comment)\b/
const correctionOnTop = /\b(?:ignore|disregard)\s+my\s+(?:(?:last|earlier|previous|above)\s+)?(?:comment|message|reply|note)s?\b|(?:^|["'\n])\s*correction\s*[:-]|\bmy\s+(?:last|earlier|previous)\s+(?:comment|message|reply|note)\s+was\s+wrong\b/i

const heredocBody = /<<-?\s*(['"]?)(\w+)\1([^\n]*)\n[\s\S]*?\n\s*\2[ \t]*(?=\n|$)/g
const singleQuoted = /'[^']*'/g

export function shellOnly(command: string): string {
  return command.replace(heredocBody, '<<$2$3').replace(singleQuoted, "''")
}

const correctionFix = 'A correction on top leaves the wrong comment standing. Edit it into the true answer, or delete it: ' +
  '`gh api -X PATCH repos/<repo>/issues/comments/<id> -f body=...`, `glab api -X PUT projects/<id>/merge_requests/<n>/notes/<note> -f body=...`, ' +
  'or the tracker tool that updates or deletes a comment. Someone replied to it: edit, do not delete.'

// A tracker's own tool posts comments too (mcp__<server>__save_comment).
export function mcpCommentRefusal(tool: string, input: unknown): string | undefined {
  if (!tool.startsWith('mcp__')) {
    return undefined
  }
  const name = tool.split('__').pop() ?? ''
  const writes = /save|create|add|post|reply/i.test(name) && /comment|note|reply/i.test(name) && !/list|get|delete/i.test(name)
  return writes && correctionOnTop.test(JSON.stringify(input)) ? correctionFix : undefined
}

export function refusal(raw: string): string | undefined {
  if (postsComment.test(raw) && correctionOnTop.test(raw)) {
    return correctionFix
  }
  const command = shellOnly(raw)
  if (unsplitSet.test(command)) {
    return 'zsh does not split `$var` in `set -- $var`, so the second word arrives empty. ' +
      'Use `read -r repo n <<< "$pair"` (bash and zsh), or loop over `repo:n` and take `${p%%:*}` / `${p##*:}`.'
  }
  if (bashPipeStatus.test(command)) {
    return '`$PIPESTATUS` is blank in zsh, so the check reads nothing and passes. ' +
      'Use `$pipestatus[1]` here, or run the script with `bash`.'
  }
  if (rmCorgiTree.test(command)) {
    return '`rm -rf` is refused in a hardened workspace. Take a fresh path instead: ' +
      '`W=/tmp/corgi-wt/<id>; [ -e "$W" ] && W="$W-$(date +%s)"`, and `git -C <repo> worktree remove "$W"` (no --force) when done.'
  }
  return undefined
}

const flagGlob = /(?<=\s)(--[A-Za-z][\w-]*=)([^\s'"`$;&|(){}<>]*[*?[][^\s'"`$;&|(){}<>]*)(?=[\s;&|)]|$)/g

const echoEquals = /\b(?:echo|printf)\b[^;&|\n]*/g
const equalsWord = /(?<=\s)=[^\s'"`$;&|(){}<>]*(?=\s|$)/g

function bareMask(command: string): boolean[] {
  const bare: boolean[] = []
  let quote = ''
  for (let i = 0; i < command.length; i++) {
    const c = command[i]
    bare.push(quote === '')
    if (c === '\\' && quote !== "'") {
      bare.push(false)
      i++
    } else if (quote === '' && (c === "'" || c === '"')) {
      quote = c
    } else if (c === quote) {
      quote = ''
    }
  }
  return bare
}

export function quoteFlagGlobs(command: string): string {
  if (command.includes('<<')) {
    return command
  }
  const bare = bareMask(command)
  return command.replace(flagGlob, (word, flag: string, glob: string, at: number) =>
    bare[at] ? `${flag}'${glob}'` : word)
}

// zsh reads a word starting with `=` as a command lookup: `echo ====` dies with "==== not found".
export function quoteEchoEquals(command: string): string {
  if (command.includes('<<')) {
    return command
  }
  const bare = bareMask(command)
  return command.replace(echoEquals, (part, start: number) =>
    part.replace(equalsWord, (word, at: number) => (bare[start + at] ? `'${word}'` : word)))
}

export function mcpHint(tool: string, text: string): string | undefined {
  if (!tool.startsWith('mcp__')) {
    return undefined
  }
  const name = tool.split('__').pop() ?? ''
  if (/query|logs?|search|traces?|metrics?|dataprime|sql/i.test(name) && /timed out|timeout|deadline exceeded/i.test(text)) {
    return 'The query timed out. Narrow it before the next call: a shorter time range (15 minutes), one service or index, a limit, ' +
      'then widen step by step. Sending the same query again times out again.'
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
