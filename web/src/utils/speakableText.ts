/**
 * extractSpeakableText — pull the human-readable text out of a message's blocks.
 *
 * A LEAF module: it imports nothing. It lives apart from `useAutoSpeech.ts`
 * (which owns the TTS state machine) because `useAutoSpeech.ts` has module-level
 * side effects — `useToast()` and the `i18n` singleton are created at import
 * time. Any module that merely wants the TEXT would drag those in, and tests
 * that mock `vue-i18n` narrowly (e.g. QuoteCard.test.ts) would then fail at
 * import with "No createI18n export is defined on the mock".
 *
 * Includes text blocks and AskUserQuestion tool_use blocks (structured
 * questions) so TTS can read the question and options.
 */
export function extractSpeakableText(blocks: Array<Record<string, unknown>>): string {
  const parts: string[] = []
  for (const b of blocks) {
    if (b.type === 'text') {
      const t = ((b.text as string) || '').trim()
      if (t) parts.push(t)
    } else if (b.type === 'tool_use' && b.name === 'AskUserQuestion' && (b.input as Record<string, unknown>)?.questions) {
      const questions = (b.input as Record<string, unknown>).questions as Array<Record<string, unknown>>
      for (const q of questions) {
        let s = (q.question as string) || ''
        if (q.header) s += ` (${q.header})`
        const opts = Array.isArray(q.options) ? q.options : []
        if (opts.length > 0) {
          s += ': ' + opts.map((o: unknown) => {
            const label = typeof o === 'string' ? o : ((o as Record<string, unknown>)?.label || '')
            const desc = typeof o === 'object' ? ((o as Record<string, unknown>)?.description || '') : ''
            return desc && desc !== label ? `${label} — ${desc}` : label
          }).join(', ')
        }
        if (s) parts.push(s)
      }
    }
  }
  return parts.join('\n').trim()
}
