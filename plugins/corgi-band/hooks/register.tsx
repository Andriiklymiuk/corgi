import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Action, Snapshot, Waiting } from '../types'
import {
  actionsIn, budgetOf, cardsOf, failureOf, fiveHourOf, glyph, readBoard, sessionsOf, settle, signature, stackOf, statusText, waitingOf,
} from './board'

const waiting = atom({ plugin: 'corgi-band', key: 'waiting' } as const, [] as Waiting[])
const actions = atom({ plugin: 'corgi-band', key: 'actions' } as const, [] as Action[])
const hidden = atom({ plugin: 'corgi-band', key: 'hidden' } as const, '')
const snapshot = atom({ plugin: 'corgi-band', key: 'snapshot' } as const, null as Snapshot | null)

const PANE = 'corgi-board'

const POLL_MS = 20_000

const me = { id: '', configDir: '', wantsPane: false }

async function runJSON($: EngineInterface, argv: string[]): Promise<string | undefined> {
  try {
    const out = await $.process.run(argv, { timeoutMs: 15_000 })
    return out.exitCode === 0 ? out.stdout : undefined
  } catch {
    return undefined
  }
}

async function refreshPane($: EngineInterface): Promise<void> {
  const [boardOut, kanbanOut, stackOut] = await Promise.all([
    runJSON($, ['corgi', '--json', 'agent', 'sessions']),
    runJSON($, ['corgi', '--json', 'agent', 'kanban']),
    runJSON($, ['corgi', '--json', 'status']),
  ])
  const board = boardOut === undefined ? undefined : readBoard(boardOut)
  const now = await $.clock.now()
  const next: Snapshot = {
    sessions: board ? sessionsOf(board) : [],
    cards: kanbanOut ? cardsOf(kanbanOut).slice(0, 8) : [],
    stack: stackOut ? stackOf(stackOut) : [],
    budget: board ? budgetOf(board, me.configDir, now) : undefined,
    at: now,
  }
  await update($, snapshot, () => next)
}

async function refresh($: EngineInterface): Promise<boolean> {
  let out
  try {
    out = await $.process.run(['corgi', '--json', 'agent', 'sessions'], { timeoutMs: 10_000 })
  } catch {
    return false
  }
  const board = out.exitCode === 0 ? readBoard(out.stdout) : undefined
  if (board === undefined) return true
  const now = await $.clock.now()
  const list = waitingOf(board, me.id)
  await update($, waiting, () => list)
  $.ui.status(statusText(list.length, fiveHourOf(board, me.configDir, now)))
  if (me.wantsPane) await refreshPane($)
  return true
}

export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    me.id = await $.session.id()
    me.configDir = (await $.env.get('CLAUDE_CONFIG_DIR')) ?? ''
    if (!e.isInteractive) return next(e)
    await $.command.register({ name: 'corgi', description: 'corgi board: sessions waiting on you, tickets, the stack, the budget' })
    void refresh($).then(found => {
      if (found) $.clock.every(POLL_MS, () => void refresh($))
    })
    return next(e)
  })

  on('command.run', { command: 'corgi' }, async $ => {
    me.wantsPane = true
    await $.ui.open({ id: PANE, title: 'corgi' })
    await refreshPane($)
    return { text: 'corgi board opened.' }
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Button, Text } = $.ui.resolve(e)
    const snap = await read($, snapshot)
    if (snap === null) return <Text dimColor>Reading corgi…</Text>
    const down = snap.stack.filter(s => !s.isHealthy)

    return (
      <Box flexDirection="column">
        <Box>
          <Text bold>Sessions </Text>
          <Button key="refresh" label="Refresh" plain dimColor onPress={() => refreshPane($)} />
        </Box>
        {snap.sessions.length === 0 && <Text dimColor>  none running</Text>}
        {snap.sessions.map(s => (
          <Box key={`s-${s.id}`}>
            <Text color={s.status === 'needs_input' ? 'yellow' : s.status === 'working' ? 'green' : undefined}>{glyph(s.status)} </Text>
            <Text wrap="truncate-end" dimColor={s.id === me.id}>
              {s.name}
              {s.what ? ` · ${s.what}` : ''}
              {s.context !== undefined ? ` · ${s.context}%` : ''}
              {s.id === me.id ? ' (here)' : ''}{' '}
            </Text>
            {s.id !== me.id && (
              <Button key={`focus-${s.id}`} label="Focus" onPress={() => void runJSON($, ['corgi', 'agent', 'focus', s.id])} />
            )}
          </Box>
        ))}
        <Text> </Text>
        <Text bold>Tickets</Text>
        {snap.cards.length === 0 && <Text dimColor>  nothing waiting</Text>}
        {snap.cards.map(c => (
          <Box key={`c-${c.ref}`}>
            <Text dimColor>{c.column.padEnd(8)}</Text>
            <Text wrap="truncate-end">
              {c.ref} {c.title}{' '}
            </Text>
            {c.column !== 'Running' && (
              <Button key={`work-${c.ref}`} label="Work on it" onPress={() => void runJSON($, ['corgi', 'agent', 'watch', 'work', c.ref])} />
            )}
          </Box>
        ))}
        {snap.stack.length > 0 && (
          <Box flexDirection="column">
            <Text> </Text>
            <Text bold>Stack</Text>
            <Text wrap="wrap">
              {snap.stack.map(s => `${s.isHealthy ? '✓' : '✗'} ${s.label}${s.port ? ` :${s.port}` : ''}`).join('   ')}
            </Text>
            {down.length > 0 && <Text color="red">{down.length} down - ask me to debug it, or run corgi run</Text>}
          </Box>
        )}
        {snap.budget !== undefined && (
          <Box flexDirection="column">
            <Text> </Text>
            <Text dimColor>budget {snap.budget}</Text>
          </Box>
        )}
      </Box>
    )
  })

  on('prompt.submit', async ($, e, next) => {
    await update($, actions, () => [])
    return next(e)
  }).catch(($, e, next) => next(e))

  on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
    const labels = actionsIn(e.command)
    if (labels.length === 0) return next(e)
    const ran = await next(e)
    const why = ran.deny ?? failureOf(ran.isError === true, ran.text ?? '')
    await update($, actions, list => [...list, ...settle(labels, why)].slice(-12))
    if (why !== undefined) {
      $.ui.toast(`corgi: ${labels.join(', ')} failed - ${why}`, { timeoutMs: 8000 })
    }
    return ran
  }).catch(($, e, next) => next(e))

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const list = await read($, waiting)
    const done = e.props.isWorking ? [] : await read($, actions)
    const isHidden = (await read($, hidden)) === signature(list, done)
    if (e.props.hasSurvey || isHidden || (list.length === 0 && done.length === 0)) {
      return next(e)
    }
    const { Box, Button, Text } = $.ui.resolve(e)
    const shown = list.slice(0, 3)

    return (
      <Box flexDirection="column">
        {shown.map(w => (
          <Box key={`w-${w.id}`}>
            <Text color="yellow">◐ </Text>
            <Text wrap="truncate-end">
              {w.name} waits on you: {w.what}{' '}
            </Text>
            <Button
              key={`focus-${w.id}`}
              label="Focus"
              onPress={() => void $.process.run(['corgi', 'agent', 'focus', w.id]).catch(() => undefined)}
            />
          </Box>
        ))}
        {list.length > shown.length && <Text dimColor>+{list.length - shown.length} more waiting</Text>}
        {done.length > 0 && (
          <Box>
            <Text dimColor>this turn: </Text>
            <Text wrap="truncate-end">
              {done.map(a => (a.isDone ? `✓ ${a.label}` : `✗ ${a.label} (${a.why})`)).join(' · ')}{' '}
            </Text>
          </Box>
        )}
        <Box>
          <Button
            key="hide"
            label="Hide"
            plain
            dimColor
            onPress={() => update($, hidden, () => signature(list, done))}
          />
        </Box>
      </Box>
    )
  })
}
