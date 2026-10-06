import { expect, test } from 'claude-code/testing'

const board = JSON.stringify({
  sessions: [
    { id: 'a1', status: 'needs_input', title: 'ABC-7 stories', pending: { tool: 'Bash', subject: 'git push' } },
    { id: 'b2', status: 'working', title: 'web polish', detail: 'Edit', context: { percent: 41 } },
    { id: 'c3', status: 'stale', title: 'old one' },
  ],
  accounts: [{ configDir: '', limits: { fiveHour: { percent: 30, resetsAt: '2999-01-01T00:00:00Z' }, sevenDay: { percent: 12, resetsAt: '2999-01-02T00:00:00Z' } } }],
})
const kanban = JSON.stringify({
  cards: [
    { ref: 'ABC-9', title: 'Phone field', column: 'Inbox', workspace: 'api' },
    { ref: 'ABC-3', title: 'Old', column: 'Done', workspace: 'api' },
    { ref: 'ABC-5', title: 'Needs the key', column: 'Blocked', workspace: 'api' },
  ],
})
const stack = JSON.stringify([
  { label: 'services.api', port: 3000, healthy: true },
  { label: 'db_services.postgres', port: 5432, healthy: false },
])

for (const surface of ['terminal', 'desktop'] as const) {
  test(`/corgi shows sessions, tickets, stack and budget, and Focus asks corgi (${surface})`, async ($, on) => {
    const ran: string[] = []
    on('process.run', (_$, e) => {
      const line = e.argv.join(' ')
      ran.push(line)
      const stdout = line.includes('agent sessions') ? board : line.includes('kanban') ? kanban : line.endsWith('status') ? stack : ''
      return { value: { exitCode: 0, stdout, stderr: '', isStdoutTruncated: false, isStderrTruncated: false } }
    })
    on('ui.open', () => ({ value: { isPlaced: true as const } }))
    on('clock.now', () => ({ value: Date.parse('2026-10-06T12:00:00Z') }))

    await $.command.run({ command: 'corgi', args: '', origin: { kind: 'composer' }, presentation: { isFullscreen: true, columns: 160 } })
    const ui = await $.ui.mount({
      plugin: 'corgi-band',
      surface,
      component: 'Pane',
      requestId: 'corgi-board',
      props: { title: 'corgi', isFocused: true, bodyColumns: 100, placement: 'dock', scroll: { offset: 0, bodyRows: 30 }, view: {} },
    })

    expect((await ui.find({ type: 'Text', text: /ABC-7 stories · Bash git push/ })) !== undefined).toBe(true)
    expect(await ui.find({ type: 'Text', text: /old one/ })).toBe(undefined)
    const tickets = (await ui.findAll({ type: 'Text', text: /ABC-/ })).map(t => t.text).join('\n')
    expect(tickets.indexOf('ABC-5')).toBeLessThan(tickets.indexOf('ABC-9'))
    expect(tickets).not.toContain('ABC-3')
    expect((await ui.find({ type: 'Text', text: /✗ postgres :5432/ })) !== undefined).toBe(true)
    expect((await ui.find({ type: 'Text', text: /budget 5h 30% · week 12%/ })) !== undefined).toBe(true)

    await ui.press({ key: 'focus-a1' })
    expect(ran).toContain('corgi agent focus a1')
    await ui.press({ key: 'work-ABC-9' })
    expect(ran).toContain('corgi agent watch work ABC-9')
  })
}
