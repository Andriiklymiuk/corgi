import type { Action, Here, PaneCard, PaneService, PaneSession, Waiting } from '../types'

type BoardSession = {
  id: string
  status?: string
  title?: string
  display?: string
  label?: string
  detail?: string
  configDir?: string
  pending?: { tool?: string; subject?: string } | null
  standing?: { word?: string; why?: string } | null
  context?: { percent?: number } | null
}
type Window = { percent?: number; resetsAt?: string }
type Board = {
  sessions?: BoardSession[]
  accounts?: { configDir?: string; limits?: { fiveHour?: Window; sevenDay?: Window } }[]
}

export function readBoard(stdout: string): Board | undefined {
  try {
    return JSON.parse(stdout) as Board
  } catch {
    return undefined
  }
}

export function waitingOf(board: Board, me: string): Waiting[] {
  return (board.sessions ?? [])
    .filter(s => s.status === 'needs_input' && s.id !== me)
    .map(s => {
      const pending = s.pending?.tool ? [s.pending.tool, s.pending.subject].filter(Boolean).join(' ') : ''
      return {
        id: s.id,
        name: s.title || s.display || s.label || s.id.slice(0, 8),
        what: clip(pending || s.detail || 'a question', 60),
      }
    })
}

const fixes: Record<string, (why: string) => string> = {
  conflicts: why =>
    `This branch conflicts with its base (${why}). Rebase it, resolve every conflict keeping both sides' intent, run the tests, and push.`,
  'checks failing': why =>
    `The checks on this branch's pull request are failing (${why}). Read the failing job's log, fix the cause, run it locally, and push.`,
  'tests failing': () => 'The last test run on this branch failed. Find why, fix it, and run the tests again.',
  'changes requested': () =>
    "This branch's pull request has changes requested. Read the open threads with `corgi agent watch pr threads`, fix each valid one, reply, and push.",
}

const worthSaying = new Set([...Object.keys(fixes), 'ready to merge', 'approved', 'merged', 'closed', 'stuck', 'blocked', 'handoff waits'])

const contextWorthSaying = 70

export function hereOf(board: Board, me: string): Here | undefined {
  const s = (board.sessions ?? []).find(x => x.id === me)
  if (s === undefined) return undefined
  const word = s.standing?.word ?? ''
  const why = s.standing?.why ?? ''
  const context = s.context?.percent
  const showWord = worthSaying.has(word)
  const showContext = context !== undefined && context >= contextWorthSaying
  if (!showWord && !showContext) return undefined
  return {
    word: showWord ? word : '',
    why: showWord ? clip(why, 70) : '',
    context: showContext ? context : undefined,
    fix: showWord ? fixes[word]?.(why) : undefined,
  }
}

export function hereLine(h: Here): string {
  const parts = [h.word && (h.why ? `${h.word} · ${h.why}` : h.word), h.context !== undefined && `context ${h.context}%`].filter(Boolean)
  return parts.join(' · ')
}

export function fiveHourOf(board: Board, configDir: string, now: number): number | undefined {
  const account = (board.accounts ?? []).find(a => (a.configDir ?? '') === configDir)
  const w = account?.limits?.fiveHour
  if (w?.percent === undefined || (w.resetsAt !== undefined && Date.parse(w.resetsAt) < now)) {
    return undefined
  }
  return w.percent
}

export function statusText(waiting: number, fiveHour: number | undefined): string | undefined {
  const parts: string[] = []
  if (waiting > 0) parts.push(`${waiting} waiting on you`)
  if (fiveHour !== undefined && fiveHour >= 80) parts.push(`5h ${fiveHour}%`)
  return parts.length ? `corgi · ${parts.join(' · ')}` : undefined
}

const flags = String.raw`(?:\s+--?\S+)*`
const corgiAgent = String.raw`\bcorgi${flags}\s+agent${flags}`
const arg = String.raw`("[^"]*"|'[^']*'|\S+)`
const patterns: [RegExp, (m: RegExpExecArray) => string][] = [
  [new RegExp(`${corgiAgent}\\s+chat\\s+(announce|post)\\b`, 'g'), () => 'Slack post'],
  [new RegExp(`${corgiAgent}\\s+watch\\s+move\\s+${arg}\\s+${arg}`, 'g'), m => `moved ${bare(m[1])} → ${bare(m[2])}`],
  [new RegExp(`${corgiAgent}\\s+watch\\s+(comment|assign|workpad)\\s+${arg}`, 'g'), m => `${m[1]} ${bare(m[2])}`],
  [new RegExp(`${corgiAgent}\\s+watch\\s+pr\\s+(ready|merge|close|approve|request|comment)\\s+${arg}`, 'g'), m => `PR ${m[1]} ${short(bare(m[2]))}`],
  [/\bgh\s+pr\s+(create|ready|merge|comment|review)\b/g, m => `gh pr ${m[1]}`],
  [/\bglab\s+mr\s+(create|merge|note|update|approve)\b/g, m => `glab mr ${m[1]}`],
  [/\bgit(?:\s+-C\s+\S+)?\s+push\b(?![^;&|]*--dry-run)/g, () => 'git push'],
]

export function actionsIn(command: string): string[] {
  const found: string[] = []
  for (const [re, label] of patterns) {
    re.lastIndex = 0
    for (let m = re.exec(command); m; m = re.exec(command)) {
      found.push(label(m))
    }
  }
  return found
}

export function failureOf(isError: boolean, text: string): string | undefined {
  const line = text.split('\n').map(l => l.trim()).find(l => /^(error|fatal|refused|rejected|denied)\b|token_revoked|invalid_auth|! \[rejected\]/i.test(l))
  if (line !== undefined) return clip(line.replace(/^Exit code \d+\s*/, ''), 80)
  if (!isError) return undefined
  const first = text.split('\n').map(l => l.replace(/^Exit code \d+\s*/, '').trim()).find(Boolean)
  return clip(first ?? 'failed', 80)
}

export function settle(labels: string[], why: string | undefined): Action[] {
  return labels.map(label => (why === undefined ? { label, isDone: true } : { label, isDone: false, why }))
}

export function signature(waiting: Waiting[], actions: Action[], here?: Here): string {
  return JSON.stringify([waiting.map(w => w.id + w.what), actions, here ? hereLine(here) : ''])
}

function bare(s: string | undefined): string {
  return (s ?? '').replace(/^["']|["']$/g, '')
}

function short(s: string): string {
  const m = /\/([^/]+)\/(?:pull|-\/merge_requests)\/(\d+)/.exec(s)
  return m ? `${m[1]}#${m[2]}` : s
}

function clip(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + '…' : s
}

const statusOrder: Record<string, number> = { needs_input: 0, limited: 1, working: 2, done: 3, unknown: 4 }

export function sessionsOf(board: Board): PaneSession[] {
  return (board.sessions ?? [])
    .filter(s => s.status !== 'gone' && s.status !== 'stale')
    .map(s => ({
      id: s.id,
      name: clip(s.title || s.display || s.label || s.id.slice(0, 8), 40),
      status: s.status ?? 'unknown',
      what: clip((s.pending?.tool ? [s.pending.tool, s.pending.subject].filter(Boolean).join(' ') : s.detail) || '', 40),
      context: (s as { context?: { percent?: number } }).context?.percent,
    }))
    .sort((a, b) => (statusOrder[a.status] ?? 9) - (statusOrder[b.status] ?? 9))
}

const columnOrder: Record<string, number> = { Blocked: 0, Review: 1, Inbox: 2, Ready: 3, Running: 4 }

export function cardsOf(stdout: string): PaneCard[] {
  let parsed: { cards?: { ref?: string; title?: string; column?: string; workspace?: string; why?: string }[] }
  try {
    parsed = JSON.parse(stdout)
  } catch {
    return []
  }
  return (parsed.cards ?? [])
    .filter(c => c.ref && c.column && c.column !== 'Done' && !c.ref.startsWith('slack-'))
    .map(c => ({ ref: c.ref!, title: clip(c.title ?? '', 50), column: c.column!, workspace: c.workspace ?? '', why: c.why }))
    .sort((a, b) => (columnOrder[a.column] ?? 9) - (columnOrder[b.column] ?? 9))
}

export function stackOf(stdout: string): PaneService[] {
  let parsed: { label?: string; port?: number; healthy?: boolean }[]
  try {
    parsed = JSON.parse(stdout)
  } catch {
    return []
  }
  if (!Array.isArray(parsed)) return []
  return parsed.map(s => ({ label: (s.label ?? '?').replace(/^(services|db_services)\./, ''), port: s.port, isHealthy: s.healthy === true }))
}

export function budgetOf(board: Board, configDir: string, now: number): string | undefined {
  const account = (board.accounts ?? []).find(a => (a.configDir ?? '') === configDir)
  const limits = account?.limits as { fiveHour?: Window; sevenDay?: Window } | undefined
  const part = (name: string, w?: Window) =>
    w?.percent === undefined || (w.resetsAt !== undefined && Date.parse(w.resetsAt) < now) ? undefined : `${name} ${w.percent}%`
  const parts = [part('5h', limits?.fiveHour), part('week', limits?.sevenDay)].filter(Boolean)
  return parts.length ? parts.join(' · ') : undefined
}

export function glyph(status: string): string {
  return { needs_input: '◐', working: '●', limited: '⏸', done: '✓' }[status] ?? '◌'
}
