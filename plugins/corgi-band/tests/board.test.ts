import { describe, expect, test } from 'claude-code/testing'

import { actionsIn, failureOf, fiveHourOf, statusText, waitingOf } from '../hooks/board'

describe('board helpers', () => {
  test('names the outward actions in a command', () => {
    expect(actionsIn('corgi agent chat announce "[ABC-1] Phone" https://github.com/a/b/pull/5 2>&1 | head -2')).toEqual(['Slack post'])
    expect(actionsIn('corgi agent watch move ABC-1 "Code review" && git -C api push -u origin feature/x')).toEqual(['moved ABC-1 → Code review', 'git push'])
    expect(actionsIn('corgi --json agent watch pr ready https://github.com/acme/api/pull/42')).toEqual(['PR ready api#42'])
    expect(actionsIn('git push --dry-run origin x')).toEqual([])
    expect(actionsIn('corgi agent watch board --refresh')).toEqual([])
  })

  test('reads a failure even when a pipe hid the exit code', () => {
    expect(failureOf(false, 'Error: slack conversations.list: token_revoked\nUsage:')).toBe('Error: slack conversations.list: token_revoked')
    expect(failureOf(true, 'Exit code 1\n ! [rejected] main -> main (fetch first)')).toContain('[rejected]')
    expect(failureOf(false, 'posted as bot: https://x.slack.com/p1')).toBe(undefined)
  })

  test('lists other sessions that wait, not this one', () => {
    const board = {
      sessions: [
        { id: 'me', status: 'needs_input', title: 'this one' },
        { id: 'a1', status: 'needs_input', title: 'ABC-8 stories', pending: { tool: 'Bash', subject: 'rm -rf build' } },
        { id: 'b2', status: 'working', title: 'busy' },
      ],
    }
    expect(waitingOf(board, 'me')).toEqual([{ id: 'a1', name: 'ABC-8 stories', what: 'Bash rm -rf build' }])
  })

  test('says the budget only while its window is the current one and high', () => {
    const now = Date.parse('2026-10-06T12:00:00Z')
    const board = { accounts: [{ configDir: '', limits: { fiveHour: { percent: 91, resetsAt: '2026-10-06T15:00:00Z' } } }] }
    expect(fiveHourOf(board, '', now)).toBe(91)
    expect(fiveHourOf(board, '', Date.parse('2026-10-07T00:00:00Z'))).toBe(undefined)
    expect(statusText(2, 91)).toBe('corgi · 2 waiting on you · 5h 91%')
    expect(statusText(0, 40)).toBe(undefined)
  })
})
