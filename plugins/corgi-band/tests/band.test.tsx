import { describe, expect, test } from 'claude-code/testing'

const band = { hasSurvey: false, isWorking: false, maxRows: 10, bodyColumns: 100, scroll: { offset: 0, bodyRows: 9 }, view: {} }

describe('corgi-band', () => {
  for (const surface of ['terminal', 'desktop'] as const) {
    test(`a failed Slack post shows in the band with its error (${surface})`, async ($, on) => {
      const toasts: string[] = []
      on('ui.toast', (_$, e) => {
        toasts.push(e.text)
        return { value: undefined }
      })
      on('ui.render', { component: 'AbovePrompt' }, ($, e) => {
        const { Box } = $.ui.resolve(e)
        return <Box key="engine" />
      })
      on('tool.call', { tool: 'Bash' }, () => ({
        result: { stdout: '', stderr: '', interrupted: false },
        text: 'Error: slack conversations.list: token_revoked\nUsage:',
      }))
      await $.tool.call({ tool: 'Bash', command: 'corgi agent chat announce "[ABC-1] x" https://github.com/a/b/pull/1 2>&1 | head -2' })
      const ui = await $.ui.mount({ plugin: 'corgi-band', surface, component: 'AbovePrompt', props: band })
      const row = await ui.find({ type: 'Text', text: /Slack post failed/ })
      expect(row?.text ?? '').toContain('token_revoked')
      expect(toasts.join(' ')).toContain('Slack post failed')
      await ui.press({ key: 'hide' })
      expect(await ui.find({ type: 'Text', text: /Slack post/ })).toBe(undefined)
    })
  }

  test('a push that went through says nothing', async ($, on) => {
    on('ui.render', { component: 'AbovePrompt' }, ($, e) => {
      const { Box } = $.ui.resolve(e)
      return <Box key="engine" />
    })
    on('tool.call', { tool: 'Bash' }, () => ({ result: { stdout: '', stderr: '', interrupted: false }, text: 'To github.com:a/b.git\n   1..2  main -> main' }))
    await $.tool.call({ tool: 'Bash', command: 'git push' })
    const ui = await $.ui.mount({ plugin: 'corgi-band', surface: 'terminal', component: 'AbovePrompt', props: band })
    expect(await ui.find({ type: 'Text', text: /git push/ })).toBe(undefined)
  })

  test('this branch conflicting shows with Fix it, which asks this session to rebase', async ($, on) => {
    const board = JSON.stringify({
      sessions: [{ id: 'me', status: 'done', standing: { word: 'conflicts', why: 'main moved 1 · conflicts in Makefile' }, context: { percent: 82 } }],
    })
    const asked: string[] = []
    on('session.id', () => ({ value: 'me' }))
    on('env.get', () => ({ value: undefined }))
    on('command.register', () => ({ value: { command: 'corgi' } }))
    on('clock.now', () => ({ value: Date.parse('2026-10-23T12:00:00Z') }))
    on('clock.every', () => ({ value: undefined }))
    on('ui.status', () => ({ value: undefined }))
    on('session.start', (_, e) => ({ cwd: e.cwd }))
    on('process.run', () => ({ value: { exitCode: 0, stdout: board, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }))
    on('prompt.submit', (_$, e) => {
      asked.push(e.text)
      return { text: e.text }
    })
    on('ui.render', { component: 'AbovePrompt' }, ($, e) => {
      const { Box } = $.ui.resolve(e)
      return <Box key="engine" />
    })
    await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
    const ui = await $.ui.mount({ plugin: 'corgi-band', surface: 'terminal', component: 'AbovePrompt', props: band })
    const row = await ui.find({ type: 'Text', text: /this branch: conflicts/ })
    expect(row?.text ?? '').toContain('conflicts in Makefile')
    expect(row?.text ?? '').toContain('context 82%')
    await ui.press({ key: 'fix' })
    expect(asked.join(' ')).toContain('Rebase it')
  })

  test('nothing to say: the band stays the engine’s', async ($, on) => {
      on('ui.render', { component: 'AbovePrompt' }, ($, e) => {
        const { Box } = $.ui.resolve(e)
        return <Box key="engine" />
      })
    const ui = await $.ui.mount({ plugin: 'corgi-band', surface: 'terminal', component: 'AbovePrompt', props: band })
    expect(await ui.find({ type: 'Button', key: 'hide' })).toBe(undefined)
  })
})
