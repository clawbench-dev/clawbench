/**
 * Canonical <clawbench-ask-question> payload handling.
 *
 * This module is the TypeScript mirror of `internal/askquestion` (Go). Both are
 * pinned to identical results by `internal/askquestion/testdata/parity_corpus.json`
 * — the same convention `web/src/utils/version.ts` uses with
 * `internal/version/compare.go`.
 *
 * Before this module there were three independent TS parsers
 * (streamPerf/xmlParser/chatRenderUtils) that disagreed on closing-tag
 * tolerance and code-fence exclusion. The worst consequence was silent
 * content loss: `detectAskQuestion` accepted a payload that the strict
 * DOMParser then rejected, and the caller stripped the tag anyway — so the
 * question vanished from the conversation with no card and no text.
 *
 * Two payload shapes reach this module:
 *  - Path A: a native tool call whose input is JSON. See normalizeAskInput.
 *  - Path B: a <clawbench-ask-question> tag embedded in assistant text, whose
 *    payload is native Markdown. See extractAskMatches.
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
 * One located <clawbench-ask-question> span.
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
  /**
   * Text to show in place of an unparsed span: the payload with its
   * tag wrapper removed, so it renders as ordinary Markdown. Set only
   * when `parsed` is null, and never empty for a non-empty span.
   *
   * Showing the inner text (rather than the raw tag) means a parse failure
   * degrades to readable prose instead of exposing markup. No content is lost
   * either way — the fallback contains everything the span held.
   */
  fallback?: string
}

/** Reason codes for an unparsed span (mirrors the Go constants). */
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

/**
 * Fold a raw key to its comparison form: strip the stray quote/space characters
 * some models emit (real data contains a literal `"question` key) and remove
 * separators, so `multiSelect`, `multi_select` and `multi-select` all collapse
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
 * `options`, `message`/`title` instead of `question`, and string-typed booleans
 * and option arrays.
 *
 * Objects carrying no question and no option — hallucinated shapes such as
 * {type:"ask-question"}, {askUserQuestion:true}, {taskId:""} or {schema:[...]}
 * — yield no items rather than an invented question.
 */
export function normalizeAskInput(input: unknown): AskItem[] {
  if (!isRecord(input) || Object.keys(input).length === 0) return []
  const arr = findQuestionArray(input, 0)
  if (arr) return itemsFromArray(arr)
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
// L2 — payload parsing (Path B)

/**
 * Understand the inner payload of a clawbench-ask-question block. Returns []
 * when nothing renderable was found.
 *
 * The only accepted payload is native Markdown (see parseMarkdownItems).
 * There is deliberately no fallback reader: an earlier version tolerated a
 * bespoke XML shape and recovered JSON, and the extra acceptance hid malformed
 * output instead of surfacing it. A payload that does not parse is shown as
 * text, which is the signal to fix the prompt rather than a reason to guess.
 */
export function parseItems(inner: string): AskItem[] {
  if (inner.trim() === '') return []
  return parseMarkdownItems(inner)
}

// ────────────────────────────────────────────────────────────
// Native Markdown payload
// ────────────────────────────────────────────────────────────

const RE_MD_FENCE = /^\s*```/
const RE_MD_BOLD = /\*\*(.+?)\*\*/g
const RE_MD_ITALIC = /\*([^*\n]+)\*/g
const RE_ATX_HEADING = /^#{1,6}\s+(.*?)\s*#*$/
const RE_BOLD_LINE = /^(?:\*\*(.+?)\*\*|__(.+?)__)$/
/**
 * An ordered-list marker: 1. / 1) / 1、 and the CJK numerals (一、二、…). The
 * trailing content is captured separately so the "must be followed by a space"
 * rule can differ per marker.
 */
const RE_ORDERED_MARKER = /^(\d+[.)、]|[一二三四五六七八九十]+[、.)])(.*)$/
/**
 * A checkbox at the start of a list item's content. Models emit ASCII,
 * fullwidth and CJK brackets, with or without inner spacing, and with or
 * without a check mark.
 */
const RE_CHECKBOX = /^(?:\[\s*[xX]?\s*\]|［\s*[xX]?\s*］|【\s*[xX]?\s*】)\s*(.*)$/

/** U+3000, which models use as a fullwidth space. */
const IDEOGRAPHIC_SPACE = '\u3000'
const TRIM_SET = ` \t\r\n${IDEOGRAPHIC_SPACE}`

/** Remove leading/trailing ASCII whitespace and the ideographic space. */
function trimListSpace(s: string): string {
  let start = 0
  let end = s.length
  while (start < end && TRIM_SET.includes(s[start])) start++
  while (end > start && TRIM_SET.includes(s[end - 1])) end--
  return s.slice(start, end)
}

/**
 * Whether a code point is a decimal digit (Unicode Nd), matching Go's
 * unicode.IsDigit. The ASCII-only /\d/ would diverge on fullwidth and
 * Arabic-Indic digits, which the parity corpus does not cover.
 */
function isDigit(cp: number): boolean {
  return /\p{Nd}/u.test(String.fromCodePoint(cp))
}

/**
 * Return the content of a list item when `line` starts with a list marker.
 *
 * Beyond CommonMark's "-", "*", "+" and "1.", this accepts the forms models
 * actually emit: the fullwidth hyphen, CJK ordinal dots (1、), CJK numerals
 * (一、), and a bullet with no following space (-甲). Those relaxations are
 * guarded so ordinary prose is not mistaken for a list:
 *
 *   - "*" must be followed by a space, so "**bold**" and "*italic*" are not
 *     list items;
 *   - "-"/"+"/"－" must not be followed by a digit or hyphen, so "-5" and "---"
 *     are not list items;
 *   - "1."/"1)" must be followed by a space, so "1.5" is not a list item.
 */
function splitListMarker(line: string): { content: string; found: boolean } {
  const trimmed = line.replace(new RegExp(`^[ \t${IDEOGRAPHIC_SPACE}]+`), '')
  if (trimmed === '') return { content: '', found: false }

  const first = trimmed[0]
  if (first === '-' || first === '+' || first === '*' || first === '－') {
    const rest = trimmed.slice(1)
    if (first === '*') {
      if (!rest.startsWith(' ') && !rest.startsWith('\t')) return { content: '', found: false }
    } else if (rest !== '' && (isDigit(rest.codePointAt(0)!) || rest[0] === '-')) {
      return { content: '', found: false }
    }
    return { content: trimListSpace(rest), found: true }
  }

  const m = RE_ORDERED_MARKER.exec(trimmed)
  if (m) {
    const marker = m[1]
    const rest = m[2]
    if ((marker.endsWith('.') || marker.endsWith(')')) &&
        !rest.startsWith(' ') && !rest.startsWith('\t')) {
      return { content: '', found: false }
    }
    return { content: trimListSpace(rest), found: true }
  }
  return { content: '', found: false }
}

/** Return the header text when the whole line is an ATX heading or is bold. */
function markdownHeader(line: string): string | null {
  const trimmed = trimListSpace(line)
  const atx = RE_ATX_HEADING.exec(trimmed)
  if (atx) return cleanInline(atx[1])
  const bold = RE_BOLD_LINE.exec(trimmed)
  if (bold) {
    if (bold[1]) return cleanInline(bold[1])
    if (bold[2]) return cleanInline(bold[2])
  }
  return null
}

/**
 * Parse a Markdown payload. At most one item is returned: the format defines
 * one question per tag, and multiple questions are written as multiple tags.
 */
function parseMarkdownItems(inner: string): AskItem[] {
  let header = ''
  const qLines: string[] = []
  const options: AskOption[] = []
  let multi = false
  let inFence = false

  for (const raw of inner.split('\n')) {
    const line = raw.replace(/[ \t\r]+$/, '')

    // A fenced block is content, not structure: keep every line verbatim as
    // question text so nothing inside it is mistaken for an option.
    if (RE_MD_FENCE.test(line)) {
      inFence = !inFence
      qLines.push(line)
      continue
    }
    if (inFence) {
      qLines.push(line)
      continue
    }

    // A header line is never a list item, so it is checked first. The title
    // must precede the options; a bold line among the options is an option
    // label, which splitListMarker already handles.
    const h = markdownHeader(line)
    if (h !== null) {
      if (header === '' && options.length === 0) header = h
      else qLines.push(h)
      continue
    }

    const marker = splitListMarker(line)
    if (marker.found) {
      const cb = RE_CHECKBOX.exec(marker.content)
      if (cb) multi = true
      const opt = markdownOption(cb ? cb[1] : marker.content)
      if (!opt) {
        // A marked list item that yields no option is malformed. Fail the whole
        // payload rather than dropping the line: the caller then strips only the
        // wrapper and renders the text, so nothing is silently discarded.
        // Removing just this entry would delete it from the card AND from the
        // text (the parsed span is removed wholesale).
        return []
      }
      options.push(opt)
      continue
    }

    const t = trimListSpace(line)
    if (t !== '') qLines.push(t)
  }

  const question = cleanInline(qLines.join(' '))
  // A Markdown payload is a question only when it carries a list. Prose with no
  // list is not a card: the assistant discusses the tag format in ordinary
  // sentences, and turning every such mention into a card would be noise. This
  // is also what gives "parse failure" a precise meaning — the caller then
  // strips the wrapper and renders the text as Markdown.
  if (options.length === 0) return []
  return [{ header, multiSelect: multi, question, options }]
}

/**
 * Read one list entry. The label and description are separated by an em dash
 * (the documented form) or a spaced hyphen.
 */
function markdownOption(s: string): AskOption | null {
  const { label: rawLabel, desc: rawDesc } = splitMarkdownOption(s.trim())
  const label = cleanInline(rawLabel)
  let desc = cleanInline(rawDesc)
  if (label === '') return null
  // A description identical to the label adds nothing to the card.
  if (desc === label) desc = ''
  return desc === '' ? { label } : { label, description: desc }
}

/** Split an option on its first separator, preferring the em dash. */
function splitMarkdownOption(s: string): { label: string; desc: string } {
  for (const sep of ['\u2014', '\u2013', ' - ']) {
    const i = s.indexOf(sep)
    if (i >= 0) return { label: s.slice(0, i), desc: s.slice(i + sep.length) }
  }
  return { label: s, desc: '' }
}

/**
 * Whitespace matching Go's unicode.IsSpace, which is what the Go mirror's
 * strings.Fields splits on. JavaScript's \s additionally matches U+FEFF, so
 * using it here would fold a BOM away and diverge from the Go side.
 */
const GO_SPACE = /[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+/g

/**
 * Remove inline Markdown emphasis markers and decode entities. The card renders
 * plain text, so `**bold**` must not display its asterisks.
 */
function cleanInline(s: string): string {
  const stripped = s
    .replace(RE_MD_BOLD, '$1')
    .replace(RE_MD_ITALIC, '$1')
    .replace(/`/g, '')
  // Entity decoding last, matching the Go mirror's cleanInline.
  return unescapeEntities(stripped).replace(GO_SPACE, ' ').trim()
}

/**
 * Remove the tag wrapper, leaving
 * everything else — including unknown tags and all text — untouched.
 *
 * This is what a failed parse degrades to: the wrapper disappears and the
 * content falls through to the Markdown renderer. It never discards content,
 * so the failure mode is "renders as plain text", not "question vanishes".
 */
const RE_ASK_WRAPPER_TAGS = /<\/?clawbench-ask-question\b[^>]*>/gi

export function stripAskTags(s: string): string {
  return s.replace(RE_ASK_WRAPPER_TAGS, '')
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

// ────────────────────────────────────────────────────────────
// L3/L4 — span location and safe stripping
// ────────────────────────────────────────────────────────────

/** The tag this module understands, without the angle brackets. */
const TAG_NAME = 'clawbench-ask-question'

const RE_OPEN_TAG = /<clawbench-ask-question\b[^>]*>/g
const RE_STD_CLOSE = /<\/clawbench-ask-question\s*>/
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
 * Locate every clawbench-ask-question span outside a code context.
 *
 * A returned match with `parsed === null` could not be understood; its `raw`
 * must be kept visible.
 */
export function extractAskMatches(text: string): AskMatch[] {
  if (!text.includes('<' + TAG_NAME)) return []
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
  const bound = boundSpan(text, openEnd)
  if (bound.closeStart < 0) {
    // No close tag: there is no payload to parse.
    const raw = text.slice(openStart, openEnd)
    return {
      start: openStart,
      end: openEnd,
      raw,
      parsed: null,
      reason: bound.reason,
      fallback: fallbackText(raw),
    }
  }
  const inner = text.slice(openEnd, bound.closeStart)
  const items = parseItems(inner)
  if (items.length > 0) {
    return {
      start: openStart,
      end: bound.closeEnd,
      raw: text.slice(openStart, bound.closeEnd),
      parsed: items,
    }
  }
  const raw = text.slice(openStart, bound.closeEnd)
  return {
    start: openStart,
    end: bound.closeEnd,
    raw,
    parsed: null,
    reason: bound.reason,
    fallback: fallbackText(raw),
  }
}

/**
 * Render an unparsed span as plain text: the tag wrapper is removed, everything
 * else is kept.
 *
 * If stripping the wrapper would leave nothing (an empty tag), the raw span is
 * returned unchanged — an empty fallback would silently erase the span.
 */
function fallbackText(raw: string): string {
  const stripped = stripAskTags(raw).trim()
  return stripped === '' ? raw : stripped
}

interface Bound { closeStart: number; closeEnd: number; reason: string }

/**
 * Find the close tag that belongs to the payload opened at openEnd.
 *
 * A span may never reach past its own payload, so a candidate close is rejected
 * when another payload starts before it — that close belongs to the later tag.
 */
function boundSpan(text: string, openEnd: number): Bound {
  const m = RE_STD_CLOSE.exec(text.slice(openEnd))
  if (!m) return { closeStart: -1, closeEnd: -1, reason: ReasonNoStandardClose }
  const closeStart = openEnd + m.index
  const closeEnd = closeStart + m[0].length
  if (hasSiblingPayload(text, openEnd, closeStart)) {
    return { closeStart: -1, closeEnd: -1, reason: ReasonNoStandardClose }
  }
  return { closeStart, closeEnd, reason: ReasonParseFailed }
}

/** An open tag at the start of a line. */
const RE_NESTED_OPEN = /(?:^|\n)[ \t]*<clawbench-ask-question\b/g

/**
 * Whether text[from:closeStart] contains the start of a genuine sibling
 * payload rather than a mere mention of the tag.
 *
 * A payload may legitimately mention the tag in its own text — inline in a
 * sentence, inside a fenced block, or in an indented example. Treating such a
 * mention as a sibling both leaks the real payload and splits it.
 *
 * A candidate mention is a genuine sibling only when both hold:
 *
 *  - the enclosing tag does NOT already form a payload of its own, so the close
 *    cannot belong to it, and
 *  - the candidate DOES form a payload ending at this close.
 *
 * Both tests ask whether the text actually parses, which is what distinguishes
 * a real payload from a mention: a fenced or indented example leaves the
 * enclosing region without a list, while a mention that opens a real payload
 * parses. Line-start is checked first only to skip the common inline case
 * cheaply.
 */
function hasSiblingPayload(text: string, from: number, closeStart: number): boolean {
  RE_NESTED_OPEN.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = RE_NESTED_OPEN.exec(text.slice(from, closeStart))) !== null) {
    const sibOpenEnd = from + m.index + m[0].length
    if (parseItems(text.slice(from, sibOpenEnd)).length > 0) {
      // The enclosing tag is itself a payload; the close is its own.
      return false
    }
    if (parseItems(text.slice(sibOpenEnd, closeStart)).length > 0) return true
    if (m[0].length === 0) RE_NESTED_OPEN.lastIndex++
  }
  return false
}
/**
 * Replace every located span with what should be shown in its place.
 *
 * - A parsed span is removed entirely (the card renders it).
 * - An unparsed span is replaced by its `fallback`: the payload with the
 *   tag wrapper removed, so it renders as ordinary Markdown instead of
 *   exposing raw markup.
 *
 * No content is ever discarded. An unparsed span's fallback holds everything
 * the span contained, so the failure mode is "renders as plain text" — never
 * the silent loss this module exists to prevent.
 */
export function stripAskMatches(text: string, matches: AskMatch[]): string {
  if (matches.length === 0) return text
  const applicable = matches
    .filter(m => m.start >= 0 && m.end <= text.length && m.end > m.start)
    .sort((a, b) => a.start - b.start)
  let out = ''
  let prev = 0
  for (const m of applicable) {
    if (m.start < prev) continue
    out += text.slice(prev, m.start)
    if (m.parsed !== null) {
      // The card renders it; nothing goes into the text stream.
    } else if (m.fallback) {
      out += m.fallback
    } else {
      // Defensive: a fallback is always set for an unparsed span, but an empty
      // one would erase content, so keep the raw text.
      out += m.raw
    }
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
      // In the Markdown format the bold title is often the whole question
      // ("**Which features?**" with only a checkbox list below), so the header
      // leads when there is no separate question text. Otherwise it is a
      // parenthetical label after the question.
      let s = it.question
      if (!it.question && it.header) s = it.header
      else if (it.header) s += ` (${it.header})`
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
