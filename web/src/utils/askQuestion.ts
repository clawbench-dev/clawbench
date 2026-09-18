/**
 * Canonical <ask-question> payload handling.
 *
 * This module is the TypeScript mirror of `internal/askquestion` (Go). Both are
 * pinned to identical results by `internal/askquestion/testdata/parity_corpus.json`
 * — the same convention `web/src/utils/version.ts` uses with
 * `internal/version/compare.go`.
 *
 * Before this module there were three independent TS parsers
 * (streamPerf/xmlParser/chatRenderUtils) that disagreed on multi-select
 * spelling, options without a <label>, items without options, closing-tag
 * tolerance, and code-fence exclusion. The worst consequence was silent
 * content loss: `detectAskQuestion` accepted a payload that the strict
 * DOMParser then rejected, and the caller stripped the tag anyway — so the
 * question vanished from the conversation with no card and no text.
 *
 * Two payload shapes reach this module:
 *  - Path A: a native tool call whose input is JSON. See normalizeAskInput.
 *  - Path B: <ask-question> XML embedded in assistant text. See extractAskMatches.
 *
 * Design rules:
 *  - An unparseable span is reported with `parsed: null` and MUST be retained
 *    in the visible text. Never strip it.
 *  - JSON inside the tag is deliberately unsupported (removed in d189374e1).
 *  - Nothing is invented: an option with no text is dropped, not given a label.
 */

export interface AskOption {
  label: string
  description?: string
}

export interface AskItem {
  header: string
  multiSelect: boolean
  question: string
  options: AskOption[]
}

/**
 * One located <ask-question> span.
 *
 * `parsed === null` means the span could not be understood; its `raw` must be
 * kept visible.
 */
export interface AskMatch {
  /** Span bounds in the source text. Advisory when `parsed` is null. */
  start: number
  end: number
  /** The matched source text (text.slice(start, end)). */
  raw: string
  /** Non-null only when the payload was understood. */
  parsed: AskItem[] | null
  /** Why an unparsed span was left alone. */
  reason?: string
}

/** Reason codes for an unparsed span (mirrors the Go constants). */
export const ReasonNoChildClose = 'no_child_close'
export const ReasonNoStandardClose = 'no_standard_close'
export const ReasonParseFailed = 'parse_failed'

// ────────────────────────────────────────────────────────────
// Key normalization (Path A)
// ────────────────────────────────────────────────────────────

const QUESTION_ARRAY_KEYS = ['questions', 'items', 'parameters']
const WRAPPER_KEYS = ['params', 'parameters', 'data', 'input', 'args']
const QUESTION_KEYS = ['question', 'message', 'title', 'text', 'prompt']
const OPTION_KEYS = ['options', 'choices', 'answers', 'values']
const LABEL_KEYS = ['label', 'value', 'text', 'title']
const DESC_KEYS = ['description', 'desc', 'detail']
const MULTI_KEYS = ['multiselect', 'multiple', 'multi']
const XML_STRING_KEYS = ['ask', 'prompt', 'questions', 'content']

/**
 * Fold a raw key to its comparison form: strip the stray quote/space characters
 * some models emit (real data contains a literal `"question` key) and remove
 * separators, so `multi-select`, `multi_select` and `multiSelect` all collapse
 * to `multiselect`.
 */
function canonicalKey(k: string): string {
  return k
    .trim()
    .replace(/^["'\s]+|["'\s]+$/g, '')
    .toLowerCase()
    // Strip separators only, matching the Go mirror exactly (see canonicalKey).
    .replace(/[-_ \t\n\r]/g, '')
}

/**
 * Find the value whose canonical key equals `canonicalName`.
 *
 * A payload can carry two keys folding to the same canonical form (production
 * data contains both `question` and the stray-quoted `"question`). The exact
 * canonical key wins; ties break on raw key order. The Go mirror applies the
 * identical rule, so both sides resolve such payloads the same way.
 */
function lookup(m: Record<string, unknown>, canonicalName: string): unknown {
  let bestKey: string | undefined
  for (const k of Object.keys(m)) {
    if (canonicalKey(k) !== canonicalName) continue
    if (bestKey === undefined || preferKey(k, bestKey, canonicalName)) bestKey = k
  }
  return bestKey === undefined ? undefined : m[bestKey]
}

/** Whether `candidate` is a better key match than `current`. */
function preferKey(candidate: string, current: string, canonicalName: string): boolean {
  const candExact = candidate === canonicalName
  const curExact = current === canonicalName
  if (candExact !== curExact) return candExact
  return candidate < current
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

// ────────────────────────────────────────────────────────────
// L1 — Path A normalization
// ────────────────────────────────────────────────────────────

/**
 * Convert a Path-A tool input into canonical items.
 *
 * Accepts every malformed shape observed in production: a flat
 * {question, options} object without the `questions` wrapper, `{items:[...]}`,
 * `{params:{items:[...]}}`, `{parameters:[...]}`, `choices` instead of
 * `options`, `message`/`title` instead of `question`, string-typed booleans and
 * option arrays, and a raw `<item>` XML payload parked in a string field.
 *
 * Objects carrying no question and no option — hallucinated shapes such as
 * {type:"ask-question"}, {askUserQuestion:true}, {taskId:""} or {schema:[...]}
 * — yield no items rather than an invented question.
 */
export function normalizeAskInput(input: unknown): AskItem[] {
  if (!isRecord(input) || Object.keys(input).length === 0) return []
  const arr = findQuestionArray(input, 0)
  if (arr) return itemsFromArray(arr)
  const xml = findXmlString(input)
  if (xml) {
    const direct = parseItems(xml)
    if (direct.length > 0) return direct
    for (const m of extractAskMatches(xml)) {
      if (m.parsed) return m.parsed
    }
    return []
  }
  const single = normalizeItem(input)
  return single ? [single] : []
}

/** Locate the questions array, unwrapping one level of parameter-style nesting. */
function findQuestionArray(m: Record<string, unknown>, depth: number): unknown[] | null {
  for (const key of QUESTION_ARRAY_KEYS) {
    const v = lookup(m, key)
    const arr = asArray(v)
    if (arr) return arr
  }
  if (depth >= 2) return null
  for (const key of WRAPPER_KEYS) {
    const v = lookup(m, key)
    if (isRecord(v)) {
      const arr = findQuestionArray(v, depth + 1)
      if (arr) return arr
    }
  }
  return null
}

/** Return a string field that looks like a raw <item> payload. */
function findXmlString(m: Record<string, unknown>): string {
  for (const key of XML_STRING_KEYS) {
    const v = lookup(m, key)
    if (typeof v === 'string' && v.includes('<item')) return v
  }
  return ''
}

/**
 * Accept a JSON array or a string holding one (models sometimes double-encode
 * the options list as a JSON string).
 */
function asArray(v: unknown): unknown[] | null {
  if (Array.isArray(v)) return v
  if (typeof v === 'string') {
    const s = v.trim()
    if (!s.startsWith('[')) return null
    try {
      const parsed: unknown = JSON.parse(s)
      return Array.isArray(parsed) ? parsed : null
    } catch {
      return null
    }
  }
  return null
}

function itemsFromArray(arr: unknown[]): AskItem[] {
  const items: AskItem[] = []
  for (const el of arr) {
    if (!isRecord(el)) continue
    const it = normalizeItem(el)
    if (it) items.push(it)
  }
  return items
}

/**
 * Map one question object onto AskItem. Returns null when the object carries
 * neither question text nor options — how hallucinated shapes are discarded.
 */
function normalizeItem(m: Record<string, unknown>): AskItem | null {
  const question = firstString(m, QUESTION_KEYS)
  const options = optionsFrom(m)
  if (question === '' && options.length === 0) return null
  return {
    header: firstString(m, ['header']),
    multiSelect: firstBool(m, MULTI_KEYS),
    question,
    options,
  }
}

function optionsFrom(m: Record<string, unknown>): AskOption[] {
  for (const key of OPTION_KEYS) {
    const arr = asArray(lookup(m, key))
    if (arr) return normalizeOptions(arr)
  }
  return []
}

function normalizeOptions(arr: unknown[]): AskOption[] {
  const opts: AskOption[] = []
  for (const el of arr) {
    if (typeof el === 'string') {
      const s = el.trim()
      if (s !== '') opts.push({ label: s })
      continue
    }
    if (!isRecord(el)) continue
    const label = firstString(el, LABEL_KEYS)
    if (label === '') continue
    const desc = firstString(el, DESC_KEYS)
    opts.push(desc === '' ? { label } : { label, description: desc })
  }
  return opts
}

function firstString(m: Record<string, unknown>, keys: string[]): string {
  for (const key of keys) {
    const v = lookup(m, key)
    if (typeof v === 'string' && v.trim() !== '') return v.trim()
  }
  return ''
}

/** Read a boolean that may have been emitted as the string "true"/"false". */
function firstBool(m: Record<string, unknown>, keys: string[]): boolean {
  for (const key of keys) {
    const v = lookup(m, key)
    if (typeof v === 'boolean') return v
    if (typeof v === 'string') return v.trim().toLowerCase() === 'true'
  }
  return false
}

// ────────────────────────────────────────────────────────────
// L2 — tolerant XML parsing (Path B)
// ────────────────────────────────────────────────────────────

const RE_ITEM_OPEN = /<item\b[^>]*>/g
const RE_OPTION_OPEN = /<option\b[^>]*>/g
const RE_HEADER_TAG = /<header\b[^>]*>([\s\S]*?)<\/header>/
const RE_QUESTION_TAG = /<question\b[^>]*>([\s\S]*?)<\/question>/
const RE_MULTI_TAG = /<multi[_-]?select\b[^>]*>([\s\S]*?)<\/multi[_-]?select>/
const RE_LABEL_TAG = /<label\b[^>]*>([\s\S]*?)<\/label>/
const RE_DESC_TAG = /<description\b[^>]*>([\s\S]*?)<\/description>/
const RE_ANY_TAG = /<\/?[a-zA-Z][^>]*>/g
const RE_CHILD_CLOSER = /<\/(?:item|option)\s*>/g
const RE_ATTR_LABEL = /\b(?:value|label)\s*=\s*["']([^"']*)["']/i
const RE_PLURAL_OPTION = /<\/?options\s*>/gi
const RE_ENTITY = /^[a-zA-Z0-9#x]+;/

/**
 * Understand the (possibly repaired) inner payload of an <ask-question> block.
 * Returns [] when nothing renderable was found — callers must then retain the
 * raw text.
 *
 * A JSON payload is intentionally not accepted: JSON support was removed on
 * purpose (commit d189374e1).
 */
export function parseItems(inner: string): AskItem[] {
  if (inner.trim() === '') return []
  if (looksLikeJson(inner)) return []
  // <options> is a plural wrapper some models emit around the real <option>
  // elements; drop the wrapper so the option scan sees its children.
  const normalized = inner.replace(RE_PLURAL_OPTION, '')
  const items = scanItems(normalized)
  if (items.length > 0) return items
  // Second attempt: escape stray entity characters and rescan. Done only after
  // a clean attempt to avoid corrupting legitimate entities.
  const repaired = escapeStrayEntities(normalized)
  return repaired === normalized ? [] : scanItems(repaired)
}

function looksLikeJson(s: string): boolean {
  const t = s.trim().replace(/^`+/, '').trim()
  return t.startsWith('{') || t.startsWith('[')
}

/**
 * Split the payload into item bodies and parse each.
 *
 * A body runs from the end of one <item> open tag to the next <item> open tag
 * (or end of payload), so an unclosed <item> is bounded by its sibling rather
 * than swallowing the rest of the message.
 */
function scanItems(payload: string): AskItem[] {
  const opens = allMatches(payload, RE_ITEM_OPEN)
  if (opens.length === 0) return []
  const items: AskItem[] = []
  for (let i = 0; i < opens.length; i++) {
    const bodyEnd = i + 1 < opens.length ? opens[i + 1].index : payload.length
    const body = payload.slice(opens[i].end, bodyEnd)
    const it = parseItem(body)
    if (it) items.push(it)
  }
  return items
}

function parseItem(body: string): AskItem | null {
  const header = tagText(RE_HEADER_TAG, body)
  const question = tagText(RE_QUESTION_TAG, body)
  const options = parseOptions(body)
  if (question === '' && options.length === 0) return null
  return {
    header,
    multiSelect: tagText(RE_MULTI_TAG, body).toLowerCase() === 'true',
    question,
    options,
  }
}

/** Read every <option>, bounding an unclosed one by the next option open. */
function parseOptions(body: string): AskOption[] {
  const opens = allMatches(body, RE_OPTION_OPEN)
  if (opens.length === 0) return []
  const opts: AskOption[] = []
  for (let i = 0; i < opens.length; i++) {
    const bodyEnd = i + 1 < opens.length ? opens[i + 1].index : body.length
    const content = body.slice(opens[i].end, bodyEnd).replace(RE_CHILD_CLOSER, '')
    const opt = parseOption(attrsOf(opens[i].text), content)
    if (opt) opts.push(opt)
  }
  return opts
}

/** The raw attribute text of an open tag (everything after the tag name). */
function attrsOf(openTag: string): string {
  const i = openTag.indexOf(' ')
  return i >= 0 ? openTag.slice(i).replace(/>$/, '') : ''
}

/**
 * Read one <option>. A label comes from <label>, else from a value=/label=
 * attribute, else from the element's own bare text.
 */
function parseOption(attrs: string, content: string): AskOption | null {
  let label = tagText(RE_LABEL_TAG, content)
  let desc = tagText(RE_DESC_TAG, content)
  if (label === '') {
    const m = attrs.match(RE_ATTR_LABEL)
    if (m) label = m[1].trim()
  }
  if (label === '') label = decodeText(content)
  if (label === '') return null
  // A description identical to the label adds nothing to the card.
  if (desc === label) desc = ''
  return desc === '' ? { label } : { label, description: desc }
}

function tagText(re: RegExp, s: string): string {
  const m = s.match(re)
  return m ? decodeText(m[1]) : ''
}

/**
 * Strip nested tags, unescape entities, and collapse whitespace. Only
 * tag-shaped constructs are removed, so literal comparison operators in
 * question text ("< 5" / "> 5") survive.
 */
function decodeText(s: string): string {
  return unescapeEntities(s.replace(RE_ANY_TAG, ''))
    .replace(/\s+/g, ' ')
    .trim()
}

/**
 * The shared Go/TS entity table. It covers the five XML entities plus the
 * common typographic and symbol entities that appear in assistant output.
 * The Go mirror holds an identical table; adding an entry here without adding
 * it there breaks the parity tests.
 */
const NAMED_ENTITIES: Record<string, string> = {
  amp: '&', lt: '<', gt: '>', quot: '"', apos: "'",
  nbsp: '\u00a0', hellip: '\u2026', mdash: '\u2014', ndash: '\u2013',
  copy: '\u00a9', reg: '\u00ae', trade: '\u2122',
  laquo: '\u00ab', raquo: '\u00bb', times: '\u00d7', divide: '\u00f7',
  deg: '\u00b0', plusmn: '\u00b1', middot: '\u00b7', bull: '\u2022',
  lsquo: '\u2018', rsquo: '\u2019', ldquo: '\u201c', rdquo: '\u201d',
}

const RE_ENTITY_REF = /&(#x?[0-9a-fA-F]+|[a-zA-Z][a-zA-Z0-9]*);/g

/**
 * Decode the shared entity set plus decimal/hex numeric references. Unknown
 * entities are left verbatim.
 */
function unescapeEntities(s: string): string {
  if (!s.includes('&')) return s
  return s.replace(RE_ENTITY_REF, (ref, body: string) => {
    if (body[0] === '#') {
      const hex = body.length > 1 && (body[1] === 'x' || body[1] === 'X')
      const digits = hex ? body.slice(2) : body.slice(1)
      const n = parseInt(digits, hex ? 16 : 10)
      if (!Number.isFinite(n) || n <= 0 || n > 0x10ffff) return ref
      return String.fromCodePoint(n)
    }
    return NAMED_ENTITIES[body] ?? ref
  })
}

const KNOWN_TAG_NAMES = [
  'ask-question', 'item', 'header', 'question', 'option', 'options',
  'label', 'description', 'multi-select', 'multi_select', 'multiSelect',
]

/**
 * Escape &, < and > that are not part of a real tag or a valid entity.
 * Last-resort repair, applied only when a clean scan found nothing.
 */
function escapeStrayEntities(s: string): string {
  let out = ''
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (c === '&') {
      if (!RE_ENTITY.test(s.slice(i + 1, i + 11))) {
        out += '&amp;'
        continue
      }
    } else if (c === '<') {
      if (!looksLikeTagAt(s, i)) {
        out += '&lt;'
        continue
      }
    } else if (c === '>') {
      if (!looksLikeTagEndAt(s, i)) {
        out += '&gt;'
        continue
      }
    }
    out += c
  }
  return out
}

/** Whether `s.slice(i)` opens a tag whose name is a known ask-question element. */
function looksLikeTagAt(s: string, i: number): boolean {
  const rest = s.slice(i).replace(/^<\/?/, '')
  return KNOWN_TAG_NAMES.some(name => {
    if (!rest.startsWith(name)) return false
    const next = rest[name.length]
    return next === undefined || '>/ \t\n\r'.includes(next)
  })
}

/** Whether `s[i]` closes a tag we recognize. */
function looksLikeTagEndAt(s: string, i: number): boolean {
  const open = s.lastIndexOf('<', i)
  if (open < 0 || /[<>]/.test(s.slice(open, i))) return false
  return looksLikeTagAt(s, open)
}

// ────────────────────────────────────────────────────────────
// L3/L4 — span location and safe stripping
// ────────────────────────────────────────────────────────────

/**
 * Bound on how much text may sit between the last child close and a standard
 * </ask-question>. A larger gap means the close belongs to something else.
 */
const GAP_LIMIT = 400

const RE_OPEN_TAG = /<ask-question\b[^>]*>/g
const RE_STD_CLOSE = /<\/ask-question\s*>/
const RE_ANY_CLOSE = /<\/[^>]+>/
const RE_CHILD_CLOSE = /<\/(?:item|option)\s*>/g
const RE_CODE_FENCE = /```[\s\S]*?```/g
// Inline code cannot span a line break. Without that restriction an orphaned
// backtick earlier in a long message pairs with a backtick inside a real
// payload and hides the question.
const RE_INLINE_CODE = /`[^`\n]+`/g

interface Span { start: number; end: number }
interface TagMatch { index: number; end: number; text: string }

/** All matches of a global regex, with explicit end offsets. */
function allMatches(s: string, re: RegExp): TagMatch[] {
  const out: TagMatch[] = []
  re.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(s)) !== null) {
    out.push({ index: m.index, end: m.index + m[0].length, text: m[0] })
    if (m[0].length === 0) re.lastIndex++
  }
  return out
}

/** Byte ranges occupied by fenced and inline code. */
function codeSpans(text: string): Span[] {
  const spans: Span[] = []
  for (const re of [RE_CODE_FENCE, RE_INLINE_CODE]) {
    re.lastIndex = 0
    let m: RegExpExecArray | null
    while ((m = re.exec(text)) !== null) {
      spans.push({ start: m.index, end: m.index + m[0].length })
    }
  }
  return spans
}

function inAnySpan(spans: Span[], idx: number): boolean {
  return spans.some(s => idx >= s.start && idx < s.end)
}

/**
 * Locate every <ask-question> span outside a code context.
 *
 * A returned match with `parsed === null` could not be understood; its `raw`
 * must be kept visible.
 */
export function extractAskMatches(text: string): AskMatch[] {
  if (!text.includes('<ask-question')) return []
  const code = codeSpans(text)
  const opens = allMatches(text, RE_OPEN_TAG)
  const matches: AskMatch[] = []
  let consumedTo = 0
  for (const open of opens) {
    if (open.index < consumedTo) continue
    if (inAnySpan(code, open.index)) continue
    const m = locate(text, open.index, open.end)
    if (m.end > consumedTo) consumedTo = m.end
    matches.push(m)
  }
  return matches
}

function locate(text: string, openStart: number, openEnd: number): AskMatch {
  const bound = boundSpan(text, openStart, openEnd)
  const inner = text.slice(openEnd, bound.innerEnd)
  const items = parseItems(inner)
  if (items.length > 0) {
    return {
      start: openStart,
      end: bound.spanEnd,
      raw: text.slice(openStart, bound.spanEnd),
      parsed: items,
    }
  }
  const end = bound.spanEnd > openStart ? bound.spanEnd : openEnd
  return {
    start: openStart,
    end,
    raw: text.slice(openStart, end),
    parsed: null,
    reason: bound.reason,
  }
}

interface Bound { innerEnd: number; spanEnd: number; reason: string }

/**
 * Decide where the payload ends.
 *
 * The over-strip defect came from accepting the next closing token blindly:
 * with an unclosed tag followed by a <details> block, that token is the details
 * close, so the whole block was deleted. Here the span is bounded at the last
 * real child close unless the gap to a standard close is provably empty.
 */
function boundSpan(text: string, openStart: number, openEnd: number): Bound {
  const close = RE_STD_CLOSE.exec(text.slice(openEnd))
  if (close) {
    const closeStart = openEnd + close.index
    const closeEnd = closeStart + close[0].length
    if (!hasNestedPayloadStart(text.slice(openEnd, closeStart))) {
      const childEnd = lastChildEnd(text, openEnd, closeStart)
      if (childEnd < 0) {
        // A standard close with no child close: hand the whole inner range to
        // the parser; it will fail and the caller retains the text.
        return { innerEnd: closeStart, spanEnd: closeEnd, reason: ReasonParseFailed }
      }
      // A mention AFTER the last child close means the close belongs to a later
      // tag: this tag's payload ended at childEnd.
      const gap = text.slice(childEnd, closeStart)
      if (isCleanGap(gap) && !gap.includes('<ask-question')) {
        return { innerEnd: closeStart, spanEnd: closeEnd, reason: '' }
      }
    }
  }
  // No usable standard close. A non-standard close is accepted only when it
  // belongs to this payload: the gap since the last child close must be
  // content-free AND the close name must not match an element opened before
  // this tag.
  const childEnd = lastChildEnd(text, openEnd, text.length)
  if (childEnd < 0) {
    return { innerEnd: openEnd, spanEnd: openEnd, reason: ReasonNoChildClose }
  }
  // Self-containment: if another payload STARTS before this child end, those
  // children belong to that later tag, so this tag has no payload of its own.
  if (hasNestedPayloadStart(text.slice(openEnd, childEnd))) {
    return { innerEnd: openEnd, spanEnd: openEnd, reason: ReasonNoStandardClose }
  }
  const anyClose = RE_ANY_CLOSE.exec(text.slice(childEnd))
  if (anyClose) {
    const closeStart = childEnd + anyClose.index
    const closeEnd = closeStart + anyClose[0].length
    const closeName = closeTagName(text.slice(closeStart, closeEnd))
    const gap = text.slice(childEnd, closeStart)
    if (isCleanGap(gap) && !gap.includes('<ask-question') &&
        closeName !== '' && !hasOuterOpen(text, openStart, closeName)) {
      return { innerEnd: childEnd, spanEnd: closeEnd, reason: '' }
    }
  }
  return { innerEnd: childEnd, spanEnd: childEnd, reason: ReasonNoStandardClose }
}

/**
 * Matches an ask-question open tag that is itself followed by an <item>, i.e.
 * the beginning of a genuine sibling payload.
 */
const RE_NESTED_PAYLOAD_START = /<ask-question\b[^>]*>\s*<item\b/s

/**
 * Whether `region` contains the start of another payload.
 *
 * A plain substring search for '<ask-question' is too blunt: a payload may
 * legitimately mention the tag in its own question text (e.g. "how should a
 * literal <ask-question> be rendered?"), and treating that mention as a sibling
 * tag both leaks the real payload and can delete part of it. Requiring the
 * mention to be followed by <item> distinguishes a sibling payload from prose.
 */
function hasNestedPayloadStart(region: string): boolean {
  return RE_NESTED_PAYLOAD_START.test(region)
}

/** Extract the element name from a closing tag, or '' when malformed. */
function closeTagName(tag: string): string {
  const inner = tag.replace(/^<\//, '').replace(/>$/, '').trim()
  if (inner === '') return ''
  return inner.split(/\s+/)[0].toLowerCase()
}

/**
 * Whether an open tag named `name` appears before openStart. When it does, a
 * matching close belongs to that outer element — the details-before-question
 * shape — and must not be consumed.
 */
function hasOuterOpen(text: string, openStart: number, name: string): boolean {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`<${escaped}[\\s>/]`, 'i').test(text.slice(0, openStart))
}

/** End offset of the last </item> or </option> in text[from:to], or -1. */
function lastChildEnd(text: string, from: number, to: number): number {
  const slice = text.slice(from, Math.min(to, text.length))
  const locs = allMatches(slice, RE_CHILD_CLOSE)
  return locs.length === 0 ? -1 : from + locs[locs.length - 1].end
}

/**
 * Whether the text between a payload and its closing token carries no content
 * — only whitespace and stray punctuation. Anything else (in particular any
 * letter, digit or CJK character) means the closing token terminates different
 * content.
 */
function isCleanGap(gap: string): boolean {
  if (gap.length > GAP_LIMIT) return false
  return [...gap].every(isGapNoise)
}

/**
 * Acceptable between a payload and its closing token: whitespace, or the
 * punctuation an obfuscated close leaves behind (fullwidth pipes, colons).
 */
function isGapNoise(r: string): boolean {
  return ' \t\n\r｜|:：/\\-_.·'.includes(r)
}

/**
 * Remove every successfully parsed span and leave the rest byte-for-byte
 * intact.
 *
 * Unparseable spans are deliberately NOT removed: their `raw` is the only
 * remaining copy of the question. Spans are removed from the end backwards so
 * earlier offsets stay valid.
 */
export function stripAskMatches(text: string, matches: AskMatch[]): string {
  if (matches.length === 0) return text
  const parsed = matches
    .filter(m => m.parsed !== null && m.start >= 0 && m.end <= text.length && m.end > m.start)
    .sort((a, b) => a.start - b.start)
  let out = ''
  let prev = 0
  for (const m of parsed) {
    if (m.start < prev) continue
    out += text.slice(prev, m.start)
    prev = m.end
  }
  return out + text.slice(prev)
}

/** Whether at least one match was understood. */
export function hasParsedMatches(matches: AskMatch[]): boolean {
  return matches.some(m => m.parsed !== null)
}

/** Flatten the items of every parsed match, preserving order. */
export function allMatchItems(matches: AskMatch[]): AskItem[] {
  const items: AskItem[] = []
  for (const m of matches) {
    if (m.parsed) items.push(...m.parsed)
  }
  return items
}

/** Reason codes of the matches that failed, for logging. */
export function unparsedReasons(matches: AskMatch[]): string[] {
  return matches.filter(m => m.parsed === null).map(m => m.reason ?? '')
}

// ────────────────────────────────────────────────────────────
// Plain-text rendering (TTS / recommendation prompt)
// ────────────────────────────────────────────────────────────

/**
 * Render items as a spoken-language summary. Never emits raw tags.
 *
 * Shape: "Question (Header): Option — description, Option2"
 */
export function askItemsToPlainText(items: AskItem[]): string {
  return items
    .map(it => {
      let s = it.question
      if (it.header) s += ` (${it.header})`
      if (it.options.length > 0) {
        s += ': ' + it.options
          .map(o => (o.description && o.description !== o.label)
            ? `${o.label} — ${o.description}`
            : o.label)
          .join(', ')
      }
      return s
    })
    .join(' ')
}

/**
 * Convert items into the `{questions: [...]}` shape stored in a tool input.
 * Field names match the Go `ToInputMap` and internal/model's JSON contract.
 */
export function askItemsToInputMap(items: AskItem[]): { questions: Array<Record<string, unknown>> } {
  return {
    questions: items.map(it => ({
      header: it.header,
      multiSelect: it.multiSelect,
      question: it.question,
      options: it.options.map(o => {
        const opt: Record<string, unknown> = { label: o.label }
        if (o.description) opt.description = o.description
        return opt
      }),
    })),
  }
}

/** True when the input carries at least one renderable question. */
export function hasRenderableAskInput(input: unknown): boolean {
  return normalizeAskInput(input).length > 0
}
