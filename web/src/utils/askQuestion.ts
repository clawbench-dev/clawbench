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
/**
 * The span uses the pre-rename `<ask-question>` tag. Such a span is always
 * `parsed === null` — the old format is no longer read as a card — and its
 * `fallback` is the payload degraded to readable Markdown.
 */
export const ReasonLegacyFormat = 'legacy_format'

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

/**
 * Split an option on its first separator, preferring the em dash.
 *
 * A separator inside a `**bold**` run does not split: the bold run is part of
 * the label. Models routinely bold the whole "label — short gloss" phrase and
 * then add the explanation after it —
 *
 *   - **A — 回合结束时失效负缓存（推荐，最小改动）** — 在 ContentBlocks.vue …
 *
 * — so splitting on the inner dash truncated the label to `**A` and left the
 * unmatched `**` visible in the card (production message 52484). Splitting on
 * the dash after the bold run keeps the phrase intact and still separates the
 * description.
 */
function splitMarkdownOption(s: string): { label: string; desc: string } {
  const bold = boldSpans(s)
  for (const sep of ['\u2014', '\u2013', ' - ']) {
    const i = indexOutsideSpans(s, sep, bold)
    if (i >= 0) return { label: s.slice(0, i), desc: s.slice(i + sep.length) }
  }
  return { label: s, desc: '' }
}

/** [start, end) ranges of every `**bold**` run in s. */
function boldSpans(s: string): Array<[number, number]> {
  const spans: Array<[number, number]> = []
  RE_MD_BOLD.lastIndex = 0
  let m: RegExpExecArray | null
  while ((m = RE_MD_BOLD.exec(s)) !== null) {
    spans.push([m.index, m.index + m[0].length])
    if (m[0].length === 0) RE_MD_BOLD.lastIndex++
  }
  return spans
}

/**
 * First index of sep that is not inside one of the spans, or -1 when every
 * occurrence lies inside one.
 */
function indexOutsideSpans(s: string, sep: string, spans: Array<[number, number]>): number {
  let from = 0
  for (;;) {
    const i = s.indexOf(sep, from)
    if (i < 0) return -1
    if (!spans.some(([start, end]) => i >= start && i < end)) return i
    from = i + 1
  }
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
 * Locate every ask-question span outside a code context.
 *
 * Two tag names are recognized: the current `<clawbench-ask-question>` (whose
 * Markdown payload becomes a card) and the pre-rename `<ask-question>`, which
 * is no longer read as a card and instead degrades to readable Markdown — see
 * the legacy section below. Matches are returned in source order, which
 * `stripAskMatches` requires.
 *
 * A returned match with `parsed === null` could not be understood; its `raw`
 * must be kept visible.
 */
export function extractAskMatches(text: string): AskMatch[] {
  const code = codeSpans(text)
  const current = extractCurrentMatches(text, code)
  const legacy = extractLegacyMatches(text, code)
  if (legacy.length === 0) return current
  if (current.length === 0) return legacy
  return mergeByStart(current, legacy)
}

/** Merge two start-ordered slices into one start-ordered slice. */
function mergeByStart(a: AskMatch[], b: AskMatch[]): AskMatch[] {
  const out: AskMatch[] = []
  let i = 0
  let j = 0
  while (i < a.length && j < b.length) {
    if (a[i].start <= b[j].start) out.push(a[i++])
    else out.push(b[j++])
  }
  while (i < a.length) out.push(a[i++])
  while (j < b.length) out.push(b[j++])
  return out
}

/** Locate every `<clawbench-ask-question>` span. */
function extractCurrentMatches(text: string, code: Span[]): AskMatch[] {
  if (!text.includes('<' + TAG_NAME)) return []
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

// ────────────────────────────────────────────────────────────
// Legacy tag degradation (pre-rename <ask-question>)
//
// The tag was renamed to <clawbench-ask-question> and its payload changed from
// bespoke XML (later also JSON) to native Markdown. The rename deliberately
// dropped every compatibility reader: an old payload no longer becomes a card.
//
// That left a rendering defect. An old payload is *not* markup a renderer
// ignores — the wrapper is stripped and the inner text is handed to the
// Markdown renderer, where DOMPurify removes the unknown XML elements but
// keeps their text nodes. So the field values survive as prose:
//
//   <header>下一步</header><multi-select>false</multi-select>
//   <question>…</question><option><label>只修本地能用</label>…
//
// renders as "下一步 false … 只修本地能用 先保证自己 iOS 上传恢复" — the
// parser-only field `false` is shown to the user, and a label runs into its
// description because the tags that separated them are gone.
//
// This section degrades an old span to readable Markdown instead: the header
// becomes a bold line, the question a paragraph, each option a list item, and
// the parser-only multi-select flag is dropped. It never produces a card —
// `allMatchItems` only reports parsed spans, and a legacy span is never parsed.
//
// Measured on the production database: of 431 structured legacy spans, the old
// behaviour leaked `false`/`true` in 68.5% and lost the label/description
// separator in 62.0%; this degradation renders 100% of them with no field
// leakage.
// ────────────────────────────────────────────────────────────

/** The pre-rename tag, without the angle brackets. */
const LEGACY_TAG_NAME = 'ask-question'

const RE_LEGACY_OPEN = /<ask-question\b[^>]*>/g
const RE_LEGACY_CLOSE = /<\/ask-question\s*>/
/** An open tag at the start of a line — a sibling payload, not a mention. */
const RE_LEGACY_SIBLING_OPEN = /(?:^|\n)[ \t]*<ask-question\b/g
/**
 * A payload that lost its wrapper close ends at its last child element close.
 */
const RE_LEGACY_CHILD_CLOSE =
  /<\/(?:item|options|option|label|description|question|header|multi[_-]?select)\s*>/gi
/**
 * A payload that starts immediately with a structural element. It separates a
 * real payload from prose that merely mentions the tag ("…stripped
 * <ask-question> tags from e.blocks").
 */
const RE_LEGACY_STRUCT_HEAD =
  /^\s*(?:<(?:item|options|header|question|option|label|description|multi[_-]?select)\b|[[{])/i
/** Whether a payload carries any child element. */
const RE_LEGACY_CHILD_OPEN =
  /<(item|header|question|option|options|label|description|multi[_-]?select)\b/i
/** A JSON payload parked inside the tag. */
const RE_LEGACY_JSON_BODY = /^\s*[[{]/

// Element readers. Every one tolerates the malformations seen in production:
// unclosed elements (24% of payloads), attributes instead of child elements
// (`<option value="A">`, 12%), a plural <options> wrapper, bare text inside
// <option>, and raw & / < / > characters.
const RE_LEGACY_HEADER_EL = /<header\b[^>]*>([\s\S]*?)<\/header>/i
const RE_LEGACY_QUESTION_EL = /<question\b[^>]*>([\s\S]*?)<\/question>/i
const RE_LEGACY_LABEL_EL = /<label\b[^>]*>([\s\S]*?)<\/label>/i
const RE_LEGACY_DESC_EL = /<description\b[^>]*>([\s\S]*?)<\/description>/i
const RE_LEGACY_ITEM_OPEN = /<item\b[^>]*>/gi
const RE_LEGACY_OPTION_OPEN = /<option\b([^>]*)>/gi
/** Attribute text of one `<option …>` open tag (non-global: no lastIndex state). */
const RE_LEGACY_OPTION_ATTRS = /<option\b([^>]*)>/i
const RE_LEGACY_OPTIONS_WRAP = /<\/?options\s*>/gi
const RE_LEGACY_ATTR_VALUE = /\b(?:value|label)\s*=\s*["']([^"']*)["']/i
const RE_LEGACY_ANY_TAG = /<\/?[a-zA-Z][^>]*>/g
/**
 * Leaked model-harness markup: a closing tag whose name was mangled with
 * the DSML sentinel, and a bare `</>`. 12297 of these occur in the
 * production database, always as garbage inside a payload — never as
 * prose a reader would want — so they are dropped rather than shown. The
 * fullwidth vertical bar is the sentinel’s delimiter.
 */
const RE_LEGACY_ARTIFACT = /<\/?[^<>\n]*\uFF5C\uFF5C[^<>\n]*>|<\/>/g

/**
 * Locate every pre-rename `<ask-question>` span outside a code context and
 * render each to readable Markdown.
 *
 * The returned matches always have `parsed: null`: an old payload degrades to
 * text and never becomes a card.
 */
function extractLegacyMatches(text: string, code: Span[]): AskMatch[] {
  if (!text.includes('<' + LEGACY_TAG_NAME)) return []
  const opens = allMatches(text, RE_LEGACY_OPEN)
  const matches: AskMatch[] = []
  let consumedTo = 0
  for (const open of opens) {
    if (open.index < consumedTo) continue
    if (inAnySpan(code, open.index)) continue

    const bound = boundLegacy(text, open.end)
    if (bound === null) continue
    const inner = text.slice(open.end, bound.payloadEnd)
    if (!isLegacyPayload(inner)) continue

    matches.push({
      start: open.index,
      end: bound.spanEnd,
      raw: text.slice(open.index, bound.spanEnd),
      parsed: null,
      reason: ReasonLegacyFormat,
      fallback: fallbackText(renderLegacy(inner)),
    })
    consumedTo = bound.spanEnd
  }
  return matches
}

interface LegacyBound { payloadEnd: number; spanEnd: number }

/**
 * Find where the payload opened at `openEnd` ends, or null when no payload is
 * present.
 *
 * The span is clamped at the next line-start sibling open tag, so it can never
 * reach past its own payload. Within that region the payload ends at:
 *
 *  - the standard `</ask-question>`, when present — the close tag itself is
 *    part of the span (so stripping removes it) but not of the payload;
 *  - else the last child element close (an unclosed wrapper, 13% of real
 *    payloads);
 *  - else the whole region, when it starts with a structural element (a payload
 *    truncated mid-stream).
 *
 * A region that starts with ordinary prose is not a payload: the tag was
 * mentioned in a sentence, so it is left untouched in the visible text.
 */
function boundLegacy(text: string, openEnd: number): LegacyBound | null {
  let region = text.slice(openEnd)
  RE_LEGACY_SIBLING_OPEN.lastIndex = 0
  const sib = RE_LEGACY_SIBLING_OPEN.exec(region)
  if (sib) region = region.slice(0, sib.index)
  if (region.trim() === '') return null

  const close = RE_LEGACY_CLOSE.exec(region)
  if (close) {
    // The close tag is removed with the span, so it must not be parsed as part
    // of the payload: keeping it would feed a stray "</ask-question>" into the
    // JSON decoder and defeat the strict parse.
    return { payloadEnd: openEnd + close.index, spanEnd: openEnd + close.index + close[0].length }
  }

  RE_LEGACY_CHILD_CLOSE.lastIndex = 0
  let last: RegExpExecArray | null
  let lastEnd = -1
  while ((last = RE_LEGACY_CHILD_CLOSE.exec(region)) !== null) {
    lastEnd = last.index + last[0].length
  }
  if (lastEnd >= 0) {
    const end = openEnd + lastEnd
    return { payloadEnd: end, spanEnd: end }
  }

  if (RE_LEGACY_STRUCT_HEAD.test(region)) {
    const end = openEnd + region.length
    return { payloadEnd: end, spanEnd: end }
  }
  return null
}

/**
 * Whether `inner` carries a structured old-format payload (XML children or
 * JSON) rather than ordinary prose.
 */
function isLegacyPayload(inner: string): boolean {
  return RE_LEGACY_CHILD_OPEN.test(inner) || RE_LEGACY_JSON_BODY.test(inner.trim())
}

/**
 * Convert an old payload into readable Markdown.
 *
 * Nothing user-visible is discarded: the header, question, option labels and
 * descriptions all survive, and the parser-only multi-select flag is dropped
 * because it has no meaning in prose.
 */
function renderLegacy(inner: string): string {
  if (RE_LEGACY_JSON_BODY.test(inner.trim())) return renderLegacyJSON(inner)
  if (!RE_LEGACY_ANY_TAG.test(inner)) {
    // Plain text payload (an old tag wrapped around ordinary prose).
    return inner.trim()
  }

  const stripped = inner.replace(RE_LEGACY_OPTIONS_WRAP, '')
  const items = splitLegacyItems(stripped)
  let out = ''
  for (const item of items) out += renderLegacyItem(item)
  if (out.trim() === '') {
    // No element carried text (e.g. a payload that is only a stray tag): fall
    // back to the tag-stripped text so nothing disappears.
    return cleanLegacyText(inner)
  }
  return out.trim()
}

/**
 * Split a payload on `<item>` opens. An unclosed `<item>` is bounded by the
 * next `<item>` open rather than swallowing the rest.
 */
function splitLegacyItems(payload: string): string[] {
  const opens = allMatches(payload, RE_LEGACY_ITEM_OPEN)
  if (opens.length === 0) return [payload]
  const items: string[] = []
  for (let i = 0; i < opens.length; i++) {
    const end = i + 1 < opens.length ? opens[i + 1].index : payload.length
    items.push(payload.slice(opens[i].end, end))
  }
  return items
}

/** Render one `<item>` body: header, question, then options. */
function renderLegacyItem(item: string): string {
  let out = ''
  const header = legacyElementText(RE_LEGACY_HEADER_EL, item)
  if (header !== '') out += `**${header}**\n`
  // <multi-select> carries no user-facing content, so it is dropped: it is the
  // field whose raw `false` used to leak into the message.
  const question = legacyElementText(RE_LEGACY_QUESTION_EL, item)
  if (question !== '') out += `${question}\n`
  for (const opt of legacyOptions(item)) out += `- ${opt}\n`
  return out
}

/**
 * Read every `<option>` in an item body. Like `<item>`, an unclosed `<option>`
 * is bounded by the next `<option>` open or the item's end.
 */
function legacyOptions(body: string): string[] {
  const opens = allMatches(body, RE_LEGACY_OPTION_OPEN)
  if (opens.length === 0) return []
  const out: string[] = []
  for (let i = 0; i < opens.length; i++) {
    const end = i + 1 < opens.length ? opens[i + 1].index : body.length
    const openTag = opens[i].text
    const content = body.slice(opens[i].end, end)
    const attrs = RE_LEGACY_OPTION_ATTRS.exec(openTag)?.[1] ?? ''

    // Precedence: an explicit <label> child, then the element’s own body
    // text, then the attribute. Production data has 320 options in the
    // `<option value="A">A. …</option>` shape, where the attribute carries
    // only the key and the body carries the real label — preferring the
    // attribute would show "A" and discard the sentence.
    let label = legacyElementText(RE_LEGACY_LABEL_EL, content)
    if (label === '') label = cleanLegacyText(content)
    if (label === '') label = RE_LEGACY_ATTR_VALUE.exec(attrs)?.[1]?.trim() ?? ''
    // Nothing is invented: an option with no text is dropped.
    if (label === '') continue

    const desc = legacyElementText(RE_LEGACY_DESC_EL, content)
    // A description identical to the label adds nothing.
    out.push(desc !== '' && desc !== label ? `${label} — ${desc}` : label)
  }
  return out
}

/** Trimmed text of the first matching element, with nested tags removed. */
function legacyElementText(re: RegExp, s: string): string {
  const m = re.exec(s)
  return m ? cleanLegacyText(m[1]) : ''
}

/**
 * Strip nested tags, decode entities, and collapse whitespace. Only
 * tag-shaped constructs are removed, so a literal "< 5" in question text
 * survives.
 */
function cleanLegacyText(s: string): string {
  const stripped = s.replace(RE_LEGACY_ARTIFACT, ' ').replace(RE_LEGACY_ANY_TAG, ' ')
  return unescapeEntities(stripped).replace(GO_SPACE, ' ').trim()
}

/**
 * Render a JSON payload as Markdown.
 *
 * JSON inside the tag was never the documented format, but models emitted it
 * and the field values are perfectly readable. The normalizer is reused so the
 * same key synonyms and malformations are tolerated as on the tool-call path; a
 * payload that does not decode falls back to a regex salvage, because the
 * alternative is leaking raw JSON braces into the message.
 */
function renderLegacyJSON(inner: string): string {
  const items = normalizeAskInput(parseLegacyJSONObject(inner))
  if (items.length === 0) return salvageLegacyJSON(inner)
  let out = ''
  for (const it of items) {
    if (it.header) out += `**${it.header}**\n`
    if (it.question) out += `${it.question}\n`
    for (const o of it.options) {
      out += o.description && o.description !== o.label
        ? `- ${o.label} — ${o.description}\n`
        : `- ${o.label}\n`
    }
  }
  return out.trim()
}

/**
 * Decode the payload, accepting both the object shape (`{"questions":[…]}`) and
 * a bare array (`[{…}]`).
 *
 * A payload that does not decode is repaired once (see `repairLegacyJSON`) and
 * retried; a payload that still does not decode yields `{}`, which sends the
 * caller to the salvage path.
 */
function parseLegacyJSONObject(inner: string): Record<string, unknown> {
  const trimmed = inner.trim()
  if (trimmed === '') return {}
  const direct = decodeLegacyJSON(trimmed)
  if (direct !== null) return direct
  const repaired = repairLegacyJSON(trimmed)
  if (repaired !== trimmed) {
    const retried = decodeLegacyJSON(repaired)
    if (retried !== null) return retried
  }
  return {}
}

/** Strict decode of either accepted top-level shape; null when it does not decode. */
function decodeLegacyJSON(s: string): Record<string, unknown> | null {
  try {
    const parsed: unknown = JSON.parse(s)
    if (Array.isArray(parsed)) return { questions: parsed }
    return isRecord(parsed) ? parsed : null
  } catch {
    return null
  }
}

/**
 * Repair the two quote defects seen in production. Both make `JSON.parse`
 * reject a payload whose field values are otherwise intact, and both were
 * losing user-visible content before this repair existed:
 *
 *  - a key that lost its opening quote — `{label":"方向三为主"}`. Without the
 *    repair only the first option decoded, so a four-option question rendered
 *    as one option (real: #14752).
 *  - a quote used as ordinary punctuation inside a value —
 *    `"question":"…最多可以被"临时分裂"多久？"`. The value was cut short at the
 *    first interior quote, truncating the question mid-sentence (real: #15064,
 *    #15066, #22538, #22544).
 *
 * This is a repair, not a parser: it only ever adds a missing opening quote or
 * an escape, never structure. If the result still does not decode the caller
 * falls back to `salvageLegacyJSON`, so a failed repair costs nothing.
 *
 * It is deliberately not a general JSON repairer — it targets exactly these two
 * shapes, which is what the production corpus contains.
 */
function repairLegacyJSON(s: string): string {
  let out = ''
  let inString = false
  let changed = false

  for (let i = 0; i < s.length; ) {
    const c = s[i]

    if (!inString) {
      // An object boundary may open a key that lost its opening quote.
      if (c === '{' || c === ',') {
        const bare = quoteBareLegacyKey(s, i)
        if (bare) {
          out += bare.prefix
          i = bare.next
          changed = true
          continue
        }
      }
      out += c
      if (c === '"') inString = true
      i++
      continue
    }

    if (c === '\\') {
      // Copy the escape pair verbatim.
      out += c
      if (i + 1 < s.length) {
        out += s[i + 1]
        i += 2
      } else {
        i++
      }
      continue
    }
    if (c === '"') {
      if (legacyQuoteClosesString(s, i)) {
        out += c
        inString = false
      } else {
        // Interior quote: escape it and stay in the string.
        out += '\\"'
        changed = true
      }
      i++
      continue
    }
    out += c
    i++
  }

  return changed ? out : s
}

/**
 * Whether the object boundary at `s[i]` (`{` or `,`) opens a key whose opening
 * quote is missing, as in `{label":"x"`. On a match returns the repaired prefix
 * (the boundary, the quoted name, and the whitespace that separated them) plus
 * the offset just past the name's closing quote, so the caller resumes at `:`.
 */
function quoteBareLegacyKey(s: string, i: number): { prefix: string; next: number } | null {
  let j = i + 1
  // Whitespace, then — only when the boundary was a comma — one brace: an array
  // element boundary reads `,{label":`, so the key follows the brace rather
  // than the comma. A `{` boundary is itself the brace.
  while (j < s.length && isLegacyJSONSpace(s[j])) j++
  if (s[i] === ',' && j < s.length && s[j] === '{') j++
  const braceEnd = j
  while (j < s.length && isLegacyJSONSpace(s[j])) j++
  if (j >= s.length || !isLegacyKeyStart(s[j])) return null
  const start = j
  while (j < s.length && isLegacyKeyChar(s[j])) j++
  const name = s.slice(start, j)

  // The key lost its opening quote. Two shapes occur: the closing quote
  // survives (`label":`) or it was lost too (`label:`). Both are repaired by
  // quoting the name; in the first case the stray closing quote is consumed.
  const quoteStart = j
  while (j < s.length && isLegacyJSONSpace(s[j])) j++
  let next = j
  if (j < s.length && s[j] === '"') {
    next = j + 1
  } else if (j >= s.length || s[j] !== ':') {
    return null
  }

  // s[i:braceEnd] is the boundary plus any brace; s[braceEnd:start] the
  // whitespace before the name; s[quoteStart:j] the whitespace after it.
  const prefix = s.slice(i, braceEnd) + s.slice(braceEnd, start) + '"' + name + '"' + s.slice(quoteStart, j)
  return { prefix, next }
}

/**
 * Whether the quote at `s[i]` ends the string it is in. It ends the string when
 * the next non-space byte is structural — a colon (the value just closed was an
 * object key), a comma, or a closing brace/bracket. Anything else means the
 * quote was interior text.
 */
function legacyQuoteClosesString(s: string, i: number): boolean {
  for (let j = i + 1; j < s.length; j++) {
    const c = s[j]
    if (c === ' ' || c === '\t' || c === '\n' || c === '\r') continue
    return c === ':' || c === ',' || c === '}' || c === ']'
  }
  // A quote at end-of-input closes the string.
  return true
}

/** Whether `c` is whitespace JSON permits around tokens. */
function isLegacyJSONSpace(c: string): boolean {
  return c === ' ' || c === '\t' || c === '\n' || c === '\r'
}

/** Whether `c` may begin an unquoted JSON key. */
function isLegacyKeyStart(c: string): boolean {
  return c === '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

/** Whether `c` may continue an unquoted JSON key. */
function isLegacyKeyChar(c: string): boolean {
  return isLegacyKeyStart(c) || (c >= '0' && c <= '9')
}

const RE_LEGACY_JSON_QUESTION = /"question"\s*:\s*"([^"]*)"/
const RE_LEGACY_JSON_HEADER = /"header"\s*:\s*"([^"]*)"/
const RE_LEGACY_JSON_LABEL = /"label"\s*:\s*"([^"]*)"/g
const RE_LEGACY_JSON_DESC = /"description"\s*:\s*"([^"]*)"/g

/**
 * Render a JSON payload that does not decode (production data contains a stray
 * `}` and a bare token). Its field values are still intact, so they are pulled
 * out by name rather than left as raw JSON.
 *
 * Returns '' when no field could be recovered.
 */
function salvageLegacyJSON(inner: string): string {
  const labels = [...inner.matchAll(RE_LEGACY_JSON_LABEL)].map(m => cleanLegacyText(m[1]))
  const descs = [...inner.matchAll(RE_LEGACY_JSON_DESC)].map(m => cleanLegacyText(m[1]))
  const question = firstLegacySubmatch(RE_LEGACY_JSON_QUESTION, inner)
  const header = firstLegacySubmatch(RE_LEGACY_JSON_HEADER, inner)
  if (question === '' && labels.length === 0) return ''

  let out = ''
  if (header !== '') out += `**${header}**\n`
  if (question !== '') out += `${question}\n`
  for (let i = 0; i < labels.length; i++) {
    const label = labels[i]
    if (label === '') continue
    const desc = descs[i] ?? ''
    out += desc !== '' && desc !== label ? `- ${label} — ${desc}\n` : `- ${label}\n`
  }
  return out.trim()
}

/** Cleaned first capture group of `re`, or ''. */
function firstLegacySubmatch(re: RegExp, s: string): string {
  const m = re.exec(s)
  return m ? cleanLegacyText(m[1]) : ''
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
