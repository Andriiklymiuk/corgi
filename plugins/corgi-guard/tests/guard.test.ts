import { describe, expect, test } from 'claude-code/testing'

describe('corgi-guard', () => {
  test('refuses set -- $pair before it runs, with the fix', async ($, on) => {
    let ran = 0
    on('tool.call', { tool: 'Bash' }, () => {
      ran += 1
      return { result: { stdout: '', stderr: '', interrupted: false }, text: '' }
    })
    const got = await $.tool.call({ tool: 'Bash', command: 'for r in "api 541" "web 479"; do set -- $r; gh pr view $2 -R acme/$1; done' })
    expect(ran).toBe(0)
    expect(String(got.deny ?? got.text)).toContain('read -r repo n')
  })

  test('refuses $PIPESTATUS in zsh and leaves a bash script alone', async ($, on) => {
    on('tool.call', { tool: 'Bash' }, () => ({ result: { stdout: 'ok', stderr: '', interrupted: false }, text: 'ok' }))
    const zsh = await $.tool.call({ tool: 'Bash', command: 'make test | tail -5; echo "exit ${PIPESTATUS[0]}"' })
    expect(String(zsh.deny ?? zsh.text)).toContain('pipestatus')
    const inBash = await $.tool.call({ tool: 'Bash', command: "bash -c 'make | tail; echo ${PIPESTATUS[0]}'" })
    expect(inBash.deny).toBe(undefined)
  })

  test('refuses a correction posted on top of a wrong comment, and only that', async ($, on) => {
    let ran = 0
    on('tool.call', { tool: 'Bash' }, () => {
      ran += 1
      return { result: { stdout: 'ok', stderr: '', interrupted: false }, text: 'ok' }
    })
    for (const command of [
      `gh pr comment 7 --body "Correction: ignore my earlier comment, it does search both"`,
      `glab mr note create 9 --message 'please disregard my last comment'`,
      `corgi agent watch comment ABC-1 "my previous comment was wrong"`,
      `gh api repos/a/b/issues/7/comments -f body='ignore my comments above'`,
    ]) {
      const got = await $.tool.call({ tool: 'Bash', command })
      expect(String(got.deny ?? got.text)).toContain('true answer')
    }
    expect(ran).toBe(0)
    for (const command of [
      `gh pr comment 7 --body "Search looks at both lines, tested on staging"`,
      `git commit -m "ignore my earlier comment in the parser"`,
      `gh pr comment 7 --body "the correction factor is 1.2"`,
    ]) {
      expect((await $.tool.call({ tool: 'Bash', command })).deny).toBe(undefined)
    }
  })

  test('refuses the same correction through a tracker tool, and leaves other tools alone', async ($, on) => {
    on('tool.call', { tool: 'mcp__linear-server__save_comment' }, () => ({ result: {}, text: 'ok' }))
    on('tool.call', { tool: 'mcp__linear-server__delete_comment' }, () => ({ result: {}, text: 'ok' }))
    const wrong = await $.tool.call({ tool: 'mcp__linear-server__save_comment', body: 'Correction: ignore my earlier comments' })
    expect(String(wrong.deny ?? wrong.text)).toContain('true answer')
    const fine = await $.tool.call({ tool: 'mcp__linear-server__save_comment', body: 'Search reads both lines, tested on staging' })
    expect(fine.deny).toBe(undefined)
    const removes = await $.tool.call({ tool: 'mcp__linear-server__delete_comment', body: 'ignore my earlier comment' })
    expect(removes.deny).toBe(undefined)
  })

  test('refuses rm -rf on a corgi worktree and leaves other rm alone', async ($, on) => {
    on('tool.call', { tool: 'Bash' }, () => ({ result: { stdout: 'ok', stderr: '', interrupted: false }, text: 'ok' }))
    const tree = await $.tool.call({ tool: 'Bash', command: 'git worktree prune && rm -rf /tmp/corgi-wt/ABC-1-api && git worktree add' })
    expect(String(tree.deny ?? tree.text)).toContain('fresh path')
    const other = await $.tool.call({ tool: 'Bash', command: 'rm -rf build/out' })
    expect(other.deny).toBe(undefined)
    expect(other.text).toBe('ok')
  })

  test('adds the corgi fix to an empty GraphQL variable and to no checks yet', async ($, on) => {
    on('tool.call', { tool: 'Bash' }, (_$, e) => ({
      isError: true as const,
      result: 'failed',
      text: e.tool === 'Bash' && e.command.includes('graphql')
        ? 'Exit code 1 {"errors":[{"message":"Variable $n of type Int! was provided invalid value"}]}'
        : "Exit code 1 no checks reported on the 'feature/x' branch",
    }))
    const gql = await $.tool.call({ tool: 'Bash', command: "gh api graphql -F n=$N -f query='query($n:Int!){x}'" })
    expect((gql.context ?? []).join(' ')).toContain('corgi agent watch pr threads')
    const checks = await $.tool.call({ tool: 'Bash', command: 'gh pr checks 12 --watch' })
    expect((checks.context ?? []).join(' ')).toContain('until [')
  })

  test('words in a heredoc or single quotes are data, not shell', async ($, on) => {
    on('tool.call', { tool: 'Bash' }, () => ({ result: { stdout: 'ok', stderr: '', interrupted: false }, text: 'ok' }))
    const doc = "python3 - <<'EOF'\nreadme = '''- `set -- $pair` in zsh leaves the word empty'''\nEOF\ngrep -n mods README.md"
    expect((await $.tool.call({ tool: 'Bash', command: doc })).deny).toBe(undefined)
    expect((await $.tool.call({ tool: 'Bash', command: "echo 'never rm -rf /tmp/corgi-wt/x'" })).deny).toBe(undefined)
    const real = "cat <<EOF > notes\nhi\nEOF\nfor r in a b; do set -- $r; done"
    expect(String((await $.tool.call({ tool: 'Bash', command: real })).deny)).toContain('read -r')
  })

  test('quotes a word starting with = after echo, and nothing else', async ($, on) => {
    const seen: string[] = []
    on('tool.call', { tool: 'Bash' }, (_$, e) => {
      if (e.tool === 'Bash') seen.push(e.command)
      return { result: { stdout: '', stderr: '', interrupted: false }, text: '' }
    })
    await $.tool.call({ tool: 'Bash', command: 'cat a; echo =====LOG; git log; echo === done' })
    await $.tool.call({ tool: 'Bash', command: 'echo "a = b" && [ "$a" = b ] && echo x=1' })
    expect(seen[0]).toBe("cat a; echo '=====LOG'; git log; echo '===' done")
    expect(seen[1]).toBe('echo "a = b" && [ "$a" = b ] && echo x=1')
  })

  test('quotes a glob in a --flag= value, which zsh would refuse as no matches found', async ($, on) => {
    const seen: string[] = []
    on('tool.call', { tool: 'Bash' }, (_$, e) => {
      if (e.tool === 'Bash') seen.push(e.command)
      return { result: { stdout: '', stderr: '', interrupted: false }, text: '' }
    })
    await $.tool.call({ tool: 'Bash', command: 'grep -rn "a\\|b" src --include=*.ts --exclude-dir=node_* | head' })
    await $.tool.call({ tool: 'Bash', command: 'echo "see --include=*.ts" && grep -r x --include=*.{ts,tsx} . && grep -r y --include=\'*.go\' .' })
    expect(seen[0]).toBe('grep -rn "a\\|b" src --include=\'*.ts\' --exclude-dir=\'node_*\' | head')
    expect(seen[1]).toBe('echo "see --include=*.ts" && grep -r x --include=*.{ts,tsx} . && grep -r y --include=\'*.go\' .')
  })

  test('a plain successful command passes untouched', async ($, on) => {
    on('tool.call', { tool: 'Bash' }, () => ({ result: { stdout: 'main', stderr: '', interrupted: false }, text: 'main' }))
    const got = await $.tool.call({ tool: 'Bash', command: 'git branch --show-current' })
    expect(got.text).toBe('main')
    expect(got.context ?? []).toEqual([])
  })

  test('a log query that timed out gets told to narrow it, and other tools are left alone', async ($, on) => {
    on('tool.call', { tool: 'mcp__logs-prod__query_dataprime' }, () => ({ result: {}, text: 'The operation timed out.' }))
    on('tool.call', { tool: 'mcp__logs-prod__list_indexes' }, () => ({ result: {}, text: 'The operation timed out.' }))
    on('tool.call', { tool: 'mcp__logs-prod__query_logs' }, () => ({ result: {}, text: '3 rows' }))
    const slow = await $.tool.call({ tool: 'mcp__logs-prod__query_dataprime', query: 'source logs | limit 100000' })
    expect(String((slow.context ?? []).join(' '))).toContain('shorter time range')
    const other = await $.tool.call({ tool: 'mcp__logs-prod__list_indexes' })
    expect(other.context).toBe(undefined)
    const fine = await $.tool.call({ tool: 'mcp__logs-prod__query_logs', query: 'x' })
    expect(fine.context).toBe(undefined)
    expect(fine.text).toBe('3 rows')
  })
})
