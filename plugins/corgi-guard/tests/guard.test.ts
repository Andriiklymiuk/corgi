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

  test('a plain successful command passes untouched', async ($, on) => {
    on('tool.call', { tool: 'Bash' }, () => ({ result: { stdout: 'main', stderr: '', interrupted: false }, text: 'main' }))
    const got = await $.tool.call({ tool: 'Bash', command: 'git branch --show-current' })
    expect(got.text).toBe('main')
    expect(got.context ?? []).toEqual([])
  })
})
