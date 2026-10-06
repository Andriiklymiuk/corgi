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
      const row = await ui.find({ type: 'Text', text: /✗ Slack post/ })
      expect(row?.text ?? '').toContain('token_revoked')
      expect(toasts.join(' ')).toContain('Slack post failed')
      await ui.press({ key: 'hide' })
      expect(await ui.find({ type: 'Text', text: /Slack post/ })).toBe(undefined)
    })
  }

  test('nothing to say: the band stays the engine’s', async ($, on) => {
      on('ui.render', { component: 'AbovePrompt' }, ($, e) => {
        const { Box } = $.ui.resolve(e)
        return <Box key="engine" />
      })
    const ui = await $.ui.mount({ plugin: 'corgi-band', surface: 'terminal', component: 'AbovePrompt', props: band })
    expect(await ui.find({ type: 'Button', key: 'hide' })).toBe(undefined)
  })
})
