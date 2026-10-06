/**
 * TypeScript mirror of `internal/grouprouting` (Go).
 *
 * Parses the host agent's routing decision from its assistant text in a group
 * chat. Kept in sync with the Go implementation by
 * `internal/grouprouting/testdata/parity_corpus.json` (the same convention
 * `web/src/utils/askQuestion.ts` uses with `internal/askquestion`).
 *
 * Contract (mirrors the Go side): detect-then-parse; an unparseable tag is
 * NEVER stripped — the caller keeps the original text visible.
 */

export interface GroupRouting {
  found: boolean
  speakers: string[]
  instruction: string
  /**
   * Text preceding the speaker tag, trimmed. The host's background context,
   * consumed by the injection layer (design decision #68, approach A) to render
   * a member's context without repeating the routing tag or the directive. It
   * is a positional slice, not a parse result: populated whenever the tag is
   * located, even when the tag is malformed (`found === false`). Callers that
   * need a valid routing decision must gate on `found`; those that only need
   * the background may read it regardless. This function never strips.
   */
  before: string
  end: boolean
  raw: string
}

const RE_SPEAKER = /<clawbench-speaker>([\s\S]*?)<\/clawbench-speaker>/
const RE_END = /<clawbench-group-end\s*\/>/g

/** splitSpeakers splits a comma-separated list, trimming and dropping empties. */
function splitSpeakers(s: string): string[] {
  const out: string[] = []
  for (const p of s.split(',')) {
    const name = p.trim()
    if (name !== '') out.push(name)
  }
  return out
}

/** parseGroupRouting locates the host's routing decision in a message. */
export function parseGroupRouting(text: string): GroupRouting {
  const res: GroupRouting = { found: false, speakers: [], instruction: '', before: '', end: false, raw: '' }

  const endMatch = text.match(RE_END)
  if (endMatch) {
    res.end = true
    res.raw = endMatch[0]
  }

  const m = text.match(RE_SPEAKER)
  if (!m || m.index === undefined) {
    return res
  }
  res.raw = m[0]
  // Before = the text ahead of the tag (positional slice, always available
  // once the tag is located — even for a malformed payload).
  res.before = text.slice(0, m.index).trim()

  const speakers = splitSpeakers(m[1])
  if (speakers.length === 0) {
    // Malformed (empty payload): do not claim found; keep raw for logs.
    res.end = RE_END.test(text)
    return res
  }
  res.found = true
  res.speakers = speakers

  // Instruction = text after the closing speaker tag, with any end tag removed.
  const after = text.slice(m.index + m[0].length)
  res.instruction = after.replace(RE_END, '').trim()
  return res
}
