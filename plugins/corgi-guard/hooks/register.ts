import type { Register } from 'claude-code'

import { hint, mcpCommentRefusal, mcpHint, quoteEchoEquals, quoteFlagGlobs, refusal } from './rules'

export const register: Register = on => {
  on('tool.call', async ($, e, next) => {
    const why = mcpCommentRefusal(e.tool, e)
    if (why !== undefined) {
      return { deny: `corgi-guard: ${why}` }
    }
    const ran = await next(e)
    const extra = ran.deny === undefined ? mcpHint(e.tool, ran.text ?? '') : undefined
    return extra === undefined ? ran : { ...ran, context: [...(ran.context ?? []), `corgi-guard: ${extra}`] }
  }).catch(($, e, next) => next(e))
  on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
    const why = refusal(e.command)
    if (why !== undefined) {
      return { deny: `corgi-guard: ${why}` }
    }
    const command = quoteEchoEquals(quoteFlagGlobs(e.command))
    const ran = await next(command === e.command ? e : { ...e, command })
    if (ran.deny !== undefined) {
      return ran
    }
    const extra = hint(e.command, ran.text ?? '')
    if (extra === undefined) {
      return ran
    }
    return { ...ran, context: [...(ran.context ?? []), `corgi-guard: ${extra}`] }
  }).catch(($, e, next) => next(e))
}
