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

export interface BccEntry {
  /** Named members the note is addressed to, in order, trimmed, empties dropped. */
  targets: string[]
  /** The note's inner text, trimmed. */
  content: string
}

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
   * Well-formed BCC spans are removed first, so a private note placed ahead of
   * the tag never leaks into the shared background.
   */
  before: string
  /**
   * The host's private notes to individual members, in the order they appeared.
   * A note is recognised only when it is well-formed (a `targets` attribute
   * with at least one non-empty name); a malformed one is NOT parsed and NOT
   * stripped (detect-then-parse / never-lose-content). Independent of `found`:
   * a message may carry only a note and no speaker tag.
   */
  bcc: BccEntry[]
  end: boolean
  raw: string
}

const RE_SPEAKER = /<clawbench-speaker>([\s\S]*?)<\/clawbench-speaker>/
const RE_END = /<clawbench-group-end[\s\p{Z}]*\/>/gu
// Well-formed private note (the DISPLAY contract). Double-quoted targets only;
// single-quoted / attribute-less forms are malformed and left untouched (never
// stripped). `[\s\p{Z}]` mirrors Go's class: JS `\s` alone already includes
// Unicode spaces, but writing `\p{Z}` explicitly keeps the two sides visibly
// aligned (Go needs it because its `\s` is ASCII-only).
const RE_BCC = /<clawbench-bcc[\s\p{Z}]+targets[\s\p{Z}]*=[\s\p{Z}]*"([^"]*)"[\s\p{Z}]*>([\s\S]*?)<\/clawbench-bcc>/gu
// Fail-closed: ANY bcc-like span (see stripGroupBccSpans).
const RE_BCC_SPAN_ANY = /<clawbench-bcc\b[^>]*>[\s\S]*?<\/clawbench-bcc>/g
const RE_BCC_CLOSE_ANY = /<\/clawbench-bcc>/g
// Non-global mirror of RE_END for a one-shot test(). RE_END carries /g, whose
// lastIndex would otherwise make .test() alternate true/false across calls.
const RE_END_TEST = /<clawbench-group-end[\s\p{Z}]*\/>/u

/** splitSpeakers splits a comma-separated list, trimming and dropping empties. */
function splitSpeakers(s: string): string[] {
  const out: string[] = []
  for (const p of s.split(',')) {
    const name = p.trim()
    if (name !== '') out.push(name)
  }
  return out
}

/**
 * extractBcc scans text for well-formed private notes, returning the text with
 * those spans removed and the parsed entries in order. The removal is a plain
 * deletion (like stripEndTag): surrounding whitespace is preserved as-is.
 *
 * A malformed note (no targets attribute, empty target list) is neither parsed
 * nor removed — an unrecognised tag is never stripped, so its content stays
 * visible rather than being silently dropped.
 */
function extractBcc(text: string): { cleaned: string; entries: BccEntry[] } {
  const entries: BccEntry[] = []
  let cleaned = ''
  let last = 0
  RE_BCC.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = RE_BCC.exec(text)) !== null) {
    const targets = splitSpeakers(m[1])
    if (targets.length === 0) continue // malformed: leave the span in place
    cleaned += text.slice(last, m.index)
    last = m.index + m[0].length
    entries.push({ targets, content: m[2].trim() })
  }
  if (entries.length === 0) return { cleaned: text, entries }
  cleaned += text.slice(last)
  return { cleaned, entries }
}

/** parseGroupRouting locates the host's routing decision in a message. */
export function parseGroupRouting(text: string): GroupRouting {
  const res: GroupRouting = { found: false, speakers: [], instruction: '', before: '', bcc: [], end: false, raw: '' }

  // 1. Well-formed notes are extracted for DISPLAY (fail-open: a malformed note
  //    is kept verbatim so nothing is lost).
  res.bcc = extractBcc(text).entries

  // 2. Everything else runs on the FAIL-CLOSED text: every bcc-like span is
  //    removed regardless of shape, so a note can never leak into instruction
  //    (the public directive) or before (the shared background). This mirrors
  //    the Go Parse.
  const cleaned = stripGroupBccSpans(text)

  const endMatch = cleaned.match(RE_END)
  if (endMatch) {
    res.end = true
    res.raw = endMatch[0]
  }

  const m = cleaned.match(RE_SPEAKER)
  if (!m || m.index === undefined) {
    return res
  }
  res.raw = m[0]
  // Before = the text ahead of the tag (positional slice, always available
  // once the tag is located — even for a malformed payload).
  res.before = cleaned.slice(0, m.index).trim()

  const speakers = splitSpeakers(m[1])
  if (speakers.length === 0) {
    // Malformed (empty payload): do not claim found; keep raw for logs.
    // A fresh non-global regex avoids RE_END's /g lastIndex state.
    res.end = RE_END_TEST.test(cleaned)
    return res
  }
  res.found = true
  res.speakers = speakers

  // Instruction = text after the closing speaker tag, with any end tag removed.
  const after = cleaned.slice(m.index + m[0].length)
  res.instruction = after.replace(RE_END, '').trim()
  return res
}

/**
 * stripGroupBccTags removes well-formed private notes from text, returning the
 * text unchanged when there is none. Mirrors the Go StripBccTags: it keeps a
 * note out of a member's injected context (and the rendered body) even when no
 * speaker tag was found. Malformed notes are left untouched.
 */
export function stripGroupBccTags(text: string): string {
  const { cleaned, entries } = extractBcc(text)
  if (entries.length === 0) return text
  return cleaned.trim()
}

/**
 * stripGroupBccSpans removes EVERY bcc-like span from text — well-formed,
 * malformed (single quotes, extra/absent attributes), nested, or unclosed —
 * and returns the remainder trimmed. This is the fail-closed primitive for the
 * INJECTION boundary: a private note must never reach a non-target member, and
 * the host is an LLM whose tag formatting cannot be trusted. Mirrors Go's
 * StripBccSpans.
 *
 * Two passes: closed spans are peeled in a loop (a nested
 * <bcc>…<bcc>inner</bcc>…</bcc> needs another pass), then any unclosed opening
 * tag — and everything after it, since a truncated note's tail is just as
 * private — is dropped, and any stray close tag is removed.
 */
export function stripGroupBccSpans(text: string): string {
  if (!text.includes('<clawbench-bcc')) return text.trim()
  let cleaned = text
  for (;;) {
    RE_BCC_SPAN_ANY.lastIndex = 0
    const next = cleaned.replace(RE_BCC_SPAN_ANY, '')
    if (next === cleaned) break
    cleaned = next
  }
  const idx = cleaned.indexOf('<clawbench-bcc')
  if (idx >= 0) cleaned = cleaned.slice(0, idx)
  RE_BCC_CLOSE_ANY.lastIndex = 0
  cleaned = cleaned.replace(RE_BCC_CLOSE_ANY, '')
  return cleaned.trim()
}
