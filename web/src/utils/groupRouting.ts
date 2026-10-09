/**
 * TypeScript mirror of `internal/grouprouting` (Go).
 *
 * Both group-chat modes express themselves through ONE unified tag:
 *
 *   <clawbench-mention targets="A,B">给 A、B 的内容</clawbench-mention>
 *   <clawbench-mention targets="C" private>只有 C 能看到的密送</clawbench-mention>
 *   <clawbench-group-end/>
 *
 *   - HOST mode: the host names the next speakers (targets) and hands them a
 *     directive (the tag body).
 *   - FREE mode: any member @s the member(s) it wants to hand the floor to.
 *
 * Kept in sync with the Go implementation by
 * `internal/grouprouting/testdata/parity_corpus.json` (the same convention
 * `web/src/utils/askQuestion.ts` uses with `internal/askquestion`).
 *
 * Contract (mirrors the Go side): detect-then-parse; an unparseable tag is
 * NEVER stripped — the caller keeps the original text visible.
 */

export interface MentionEntry {
  /** Named members the tag addresses, in order, trimmed, empties dropped. */
  targets: string[]
  /** The tag body, trimmed. */
  content: string
  /** Whether the tag carried the `private` attribute. */
  private: boolean
  /** Delivery mode: 'parallel' when the tag carried `mode="parallel"`, else
   *  'sequential'. Only affects PUBLIC mentions; an unknown/absent value is
   *  'sequential' and never makes the tag malformed. */
  mode: DeliveryMode
}

/** Delivery modes for a mention tag (the `mode` attribute). */
export type DeliveryMode = 'sequential' | 'parallel'

/**
 * MentionGroup is one public mention tag flattened for the orchestrator: the
 * members it names plus how they should speak. It is the source of truth for
 * routing (`speakers` is derived from it), because it preserves the PER-TAG
 * boundary that the flat `speakers` list loses — and `mode` is a per-tag
 * property.
 */
export interface MentionGroup {
  /** The group's targets, in order, trimmed, and de-duplicated ACROSS groups. */
  members: string[]
  /** Whether this group speaks concurrently. */
  parallel: boolean
  /** This tag's body (the directive handed to its members). */
  instruction: string
}

export interface BccEntry {
  /** Named members the note is addressed to, in order, trimmed, empties dropped. */
  targets: string[]
  /** The note's inner text, trimmed. */
  content: string
}

export interface GroupRouting {
  found: boolean
  speakers: string[]
  /**
   * The ordered list of PUBLIC mention tags, each flattened to its members +
   * mode + instruction. The source of truth for routing (`speakers` is its
   * flat de-duplicated concatenation); a group left with no unclaimed members
   * is dropped.
   */
  groups: MentionGroup[]
  instruction: string
  /**
   * Text OUTSIDE the mention tags that precedes the first public mention,
   * trimmed. A positional slice, populated whenever a mention is located, even
   * when the tag itself is malformed (`found === false`). Every mention span is
   * removed from the slice, so a private note ahead of a public mention never
   * leaks into it. This function never strips the input.
   */
  before: string
  /**
   * Text OUTSIDE the mention tags that follows the last public mention,
   * trimmed.
   */
  after: string
  /**
   * Every well-formed private note, in the order it appeared. A note is
   * recognised only when it is well-formed (a `targets` attribute with at
   * least one non-empty name); a malformed one is NOT parsed and NOT stripped
   * (detect-then-parse / never-lose-content). Independent of `found`.
   */
  bcc: BccEntry[]
  /** Every well-formed mention tag, in order. */
  mentions: MentionEntry[]
  end: boolean
  raw: string
}

// A mention tag: a targets attribute plus an optional private attribute, then a
// body. The attribute soup is captured raw and parsed by parseMentionAttrs.
const RE_MENTION = /<clawbench-mention\b([^>]*)>([\s\S]*?)<\/clawbench-mention>/g
// Fail-closed: ANY mention-like span (see stripGroupProtocolTags).
const RE_MENTION_SPAN_ANY = /<clawbench-mention\b[^>]*>[\s\S]*?<\/clawbench-mention>/g
const RE_MENTION_CLOSE_ANY = /<\/clawbench-mention>/g
// The double-quoted targets attribute (case-sensitive, the DISPLAY contract).
// `\p{Z}` needs the /u flag.
// The double-quoted targets attribute (case-sensitive, the DISPLAY contract).
// The leading `(?:^|[\s\p{Z}])` anchors the attribute NAME to a real attribute
// boundary so `data-targets="x"` is not mistaken for `targets` (`\b` alone
// matches after the `-` of a hyphenated attribute name).
const RE_TARGETS = /(?:^|[\s\p{Z}])targets[\s\p{Z}]*=[\s\p{Z}]*"([^"]*)"/u
// The boolean `private` attribute (case-sensitive), anchored like RE_TARGETS.
const RE_PRIVATE = /(?:^|[\s\p{Z}])private(?:[\s\p{Z}]|$)/u
// The double-quoted `mode` attribute value (mirrors RE_TARGETS' display
// contract). An unknown value falls back to sequential; it never makes the tag
// malformed.
const RE_MODE = /(?:^|[\s\p{Z}])mode[\s\p{Z}]*=[\s\p{Z}]*"([^"]*)"/u
const RE_END = /<clawbench-group-end[\s\p{Z}]*\/>/gu
// Non-global mirror of RE_END for a one-shot test(). RE_END carries /g, whose
// lastIndex would otherwise make .test() alternate true/false across calls.
const RE_END_TEST = /<clawbench-group-end[\s\p{Z}]*\/>/u

/** splitNames splits a comma-separated list, trimming and dropping empties. */
function splitNames(s: string): string[] {
  const out: string[] = []
  for (const p of s.split(',')) {
    const name = p.trim()
    if (name !== '') out.push(name)
  }
  return out
}

/**
 * parseMentionAttrs extracts (targets, private) from a tag's raw attribute
 * string. Returns null when there is no well-formed double-quoted targets
 * attribute or it is empty — such a tag is malformed and NOT parsed (the
 * fail-open display contract: its text stays visible).
 */
function parseMentionAttrs(attrs: string): { targets: string[]; private: boolean; mode: DeliveryMode } | null {
  const m = attrs.match(RE_TARGETS)
  if (!m) return null
  const targets = splitNames(m[1])
  if (targets.length === 0) return null
  const modeMatch = attrs.match(RE_MODE)
  const mode: DeliveryMode = modeMatch && modeMatch[1].trim().toLowerCase() === 'parallel' ? 'parallel' : 'sequential'
  return { targets, private: RE_PRIVATE.test(attrs), mode }
}

/**
 * replaceMentionSpans rewrites every CLOSED mention-like span in text using fn:
 * fn receives the parsed entry (null when malformed) plus the raw span text and
 * returns the replacement. An UNCLOSED opening tag is left alone here — the
 * fail-closed callers drop it separately (see dropUnclosedMentionTail), while a
 * DISPLAY caller must keep a malformed span verbatim (never lose content).
 */
function replaceMentionSpans(text: string, fn: (entry: MentionEntry | null, raw: string) => string): string {
  if (!text.includes('<clawbench-mention')) return text
  let cleaned = text
  for (;;) {
    RE_MENTION_SPAN_ANY.lastIndex = 0
    const next = cleaned.replace(RE_MENTION_SPAN_ANY, (span) => {
      RE_MENTION.lastIndex = 0
      const m = RE_MENTION.exec(span)
      if (m) {
        const attrs = parseMentionAttrs(m[1])
        if (attrs) return fn({ targets: attrs.targets, content: m[2].trim(), private: attrs.private, mode: attrs.mode }, span)
      }
      return fn(null, span)
    })
    if (next === cleaned) break
    cleaned = next
  }
  return cleaned
}

/**
 * dropUnclosedMentionTail removes any UNCLOSED opening tag and everything after
 * it (a truncated/streaming span's tail is just as private), plus a stray close
 * tag. Call this ONLY on text whose CLOSED mention spans have already been
 * replaced by replaceMentionSpans — then any remaining opening tag is genuinely
 * unclosed. This is the fail-closed step (injection / quote / TTS / push
 * boundaries); a display caller must NOT use it (it keeps malformed spans
 * verbatim).
 */
function dropUnclosedMentionTail(text: string): string {
  let cleaned = text
  const idx = cleaned.indexOf('<clawbench-mention')
  if (idx >= 0) cleaned = cleaned.slice(0, idx)
  RE_MENTION_CLOSE_ANY.lastIndex = 0
  cleaned = cleaned.replace(RE_MENTION_CLOSE_ANY, '')
  return cleaned
}

/** stripAllMentionSpans removes every mention-like span entirely (fail-closed:
 *  unclosed spans and their tails included). */
function stripAllMentionSpans(text: string): string {
  return dropUnclosedMentionTail(replaceMentionSpans(text, () => ''))
}

/** parseGroupRouting locates every mention tag in an agent message. */
export function parseGroupRouting(text: string): GroupRouting {
  const res: GroupRouting = {
    found: false, speakers: [], groups: [], instruction: '', before: '', after: '',
    bcc: [], mentions: [], end: false, raw: '',
  }

  // Locate every well-formed mention for display, in order.
  RE_MENTION.lastIndex = 0
  let firstPublicStart = -1
  let lastPublicEnd = -1
  let m: RegExpExecArray | null
  while ((m = RE_MENTION.exec(text)) !== null) {
    const attrs = parseMentionAttrs(m[1])
    if (!attrs) continue // malformed: not a mention, leave the span in place
    res.mentions.push({ targets: attrs.targets, content: m[2].trim(), private: attrs.private, mode: attrs.mode })
    res.raw = m[0]
    if (!attrs.private) {
      if (firstPublicStart < 0) firstPublicStart = m.index
      lastPublicEnd = m.index + m[0].length
    }
    if (m.index === RE_MENTION.lastIndex) RE_MENTION.lastIndex++ // zero-width guard
  }

  // Before / After are the prose surrounding the mentions. Before slices around
  // the first PUBLIC mention, falling back to the first mention-like span when
  // there is no public one. Every mention span is stripped from the slice.
  if (firstPublicStart < 0) {
    RE_MENTION_SPAN_ANY.lastIndex = 0
    const span = RE_MENTION_SPAN_ANY.exec(text)
    if (span) firstPublicStart = span.index
  }
  if (firstPublicStart >= 0) {
    res.before = stripEndTag(stripAllMentionSpans(text.slice(0, firstPublicStart))).trim()
  }
  if (lastPublicEnd >= 0) {
    res.after = stripEndTag(stripAllMentionSpans(text.slice(lastPublicEnd))).trim()
  }

  // End is computed OUTSIDE every mention span (an end tag inside a private
  // note must not end the discussion).
  const outside = stripAllMentionSpans(text)
  if (RE_END_TEST.test(outside)) {
    res.end = true
    const em = outside.match(RE_END)
    if (em) res.raw = em[0]
  }
  if (res.mentions.length === 0) return res

  res.found = true
  // Derive the routing view. `groups` is the source of truth: it preserves the
  // PER-TAG boundary (needed because `mode` is a per-tag property) and the
  // per-tag instruction. `speakers` is the flat de-duplicated concatenation of
  // the groups' members. Cross-group de-duplication: a member named by an
  // earlier group is not re-listed in a later one (it would speak twice).
  const seen = new Set<string>()
  for (const e of res.mentions) {
    if (e.private) {
      res.bcc.push({ targets: e.targets, content: e.content })
      continue
    }
    const group: MentionGroup = { members: [], parallel: e.mode === 'parallel', instruction: e.content }
    for (const t of e.targets) {
      if (seen.has(t)) continue
      seen.add(t)
      group.members.push(t)
      res.speakers.push(t)
    }
    if (group.members.length > 0) res.groups.push(group)
    if (e.content !== '') {
      if (res.instruction !== '') res.instruction += '\n\n'
      res.instruction += e.content
    }
  }
  return res
}

/** stripEndTag removes the end-signal tag from text. */
export function stripEndTag(text: string): string {
  if (!RE_END_TEST.test(text)) return text
  RE_END.lastIndex = 0
  return text.replace(RE_END, '').trim()
}

/**
 * stripGroupProtocolTags removes every ClawBench protocol tag while keeping the
 * PUBLIC prose: public mentions are unwrapped (their body survives), private
 * mentions are removed entirely (fail-closed), and the end signal is dropped.
 *
 * It is what a member should see of the HOST's speech in host mode, and it is
 * ALSO the fail-closed primitive for the reading-summary / TTS / push / quote
 * boundaries (texts that leave the device or are spoken aloud).
 */
export function stripGroupProtocolTags(text: string): string {
  const replaced = replaceMentionSpans(text, (entry) => (entry && !entry.private ? entry.content : ''))
  // Fail-closed: an unclosed mention (and its tail) must not survive — the Go
  // side does this inline, and the two must agree (pinned by stripCases).
  return stripEndTag(dropUnclosedMentionTail(replaced)).trim()
}

/**
 * stripGroupBccSpans removes EVERY mention-like span from text — well-formed,
 * malformed (single quotes, extra/absent attributes), nested, or unclosed — and
 * returns the remainder trimmed. This is the fail-closed primitive for the
 * INJECTION boundary: a private note must never reach a non-target member, and
 * an agent's tag formatting cannot be trusted.
 */
export function stripGroupBccSpans(text: string): string {
  return stripAllMentionSpans(text).trim()
}

/** A minimal speaker shape the mention-name resolver needs. */
export interface MentionSpeaker {
  name: string
}

/**
 * resolveMentionDisplayName maps a raw mention target to the label shown in an
 * inline @chip. A target is either a member ROW id (a user's @ carries the id)
 * or a display name (an agent writes the name): resolve by id first, then by
 * name, and fall back to the raw target so an unresolvable target stays
 * visible.
 *
 * `userDisplay` is what the reserved human target renders as. Callers pass the
 * configured nickname so the chip shows the same name agents actually use
 * (default `GROUP_USER_TARGET_NAME` = "User"). `userTarget` is the raw token
 * that identifies the human in the protocol; it defaults to
 * `GROUP_USER_TARGET_NAME` for callers/tests that predate the nickname setting.
 *
 * `resolveId`/`resolveName` may be null (outside a group), in which case only
 * the reserved-name and fallback branches apply.
 */
export function resolveMentionDisplayName(
  target: string,
  resolveId: ((t: string) => MentionSpeaker | null) | null | undefined,
  resolveName: ((t: string) => MentionSpeaker | null) | null | undefined,
  userDisplay: string,
  userTarget: string = GROUP_USER_TARGET_NAME,
): string {
  if (target === userTarget) return userDisplay
  const byId = resolveId?.(target)
  if (byId?.name) return byId.name
  const byName = resolveName?.(target)
  if (byName?.name) return byName.name
  return target
}

/** The inline chip class the message renderer styles (and DOMPurify must keep). */
export const MENTION_CHIP_CLASS = 'msg-mention-chip'

/**
 * The reserved group-chat participant name of the HUMAN user (mirrors the Go
 * `groupUserTarget` in `internal/service/group_store.go`). An agent @-mentions
 * "User" to hand the floor back; the display layer shows it as "you" instead of
 * the raw language-neutral token.
 */
export const GROUP_USER_TARGET_NAME = 'User'

/**
 * renderMentionChips rewrites a message body for DISPLAY: each well-formed
 * public mention becomes an inline `@target` chip followed by its body text,
 * and each private mention is dropped entirely (fail-closed — a private note is
 * not addressed to the reader and must not be shown as prose). The end signal is
 * removed.
 *
 * resolveTarget (optional) maps a raw target to a display label: a target may be
 * a member ROW id (what the frontend writes for a user's @-mention) while an
 * agent writes a display name, so the caller resolves ids to names. It returns
 * the input unchanged when it cannot resolve.
 *
 * Malformed mention-like spans are left untouched so their text stays visible
 * (detect-then-parse / never-lose-content); the markdown sanitizer then unwraps
 * the unknown tag but keeps its inner text.
 *
 * The chip is a plain `<span>` (a default-allowed DOMPurify tag), so it survives
 * sanitization without an allow-list entry; only its class matters for styling.
 */
export function renderMentionChips(text: string, resolveTarget?: (target: string) => string): string {
  const label = (t: string) => (resolveTarget ? resolveTarget(t) : t)
  const cleaned = replaceMentionSpans(text, (entry, raw) => {
    if (entry === null) return raw // malformed span: keep it verbatim (never lose content)
    if (entry.private) return ''
    const at = entry.targets.map((t) => `<span class="${MENTION_CHIP_CLASS}">@${escapeChipText(label(t))}</span>`).join(' ')
    return entry.content === '' ? at + ' ' : at + ' ' + entry.content + ' '
  })
  return stripEndTag(cleaned).trim()
}

/** escapeChipText HTML-escapes a target name so a crafted member name cannot
 *  inject markup through the chip. */
function escapeChipText(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/** The minimum shape buildMemberCandidates needs from a group member. */
export interface MentionableMember {
  id: string
  name: string
  left?: boolean
  /** The underlying agent id. Carried through so the caller can resolve the
   *  member's custom avatar (a property of the AGENT, not the member row). */
  agentId?: string
  /** The member's backend, used to render its built-in icon when no custom
   *  avatar is set. */
  backend?: string
}

/** A member candidate for the `@` completion menu. */
export interface MemberCandidate {
  /** Stable identity, namespaced so it cannot collide with a file path key. */
  key: string
  label: string
  description: string
  /** The member ROW id written into the mention tag on select. */
  mentionMemberId: string
  /** Underlying agent id (avatar resolution happens in the caller). */
  agentId?: string
  /** Member backend (fallback icon when the agent has no custom avatar). */
  backend?: string
}

/**
 * buildMemberCandidates returns the group members matching `query`, for the `@`
 * completion menu. The query matches the display name (case-insensitive
 * substring). Left members are excluded (they cannot speak). Order follows the
 * roster. An empty query returns every active member, so typing a bare "@"
 * lists the whole roster.
 */
export function buildMemberCandidates(members: MentionableMember[], query: string): MemberCandidate[] {
  const q = query.trim().toLowerCase()
  const out: MemberCandidate[] = []
  for (const m of members) {
    if (m.left) continue
    if (!m.id || !m.name) continue
    if (q && !m.name.toLowerCase().includes(q)) continue
    out.push({ key: `member:${m.id}`, label: m.name, description: '', mentionMemberId: m.id, agentId: m.agentId, backend: m.backend })
  }
  return out
}

/** buildMentionTag renders the tag a user's @-mention of a member inserts. The
 *  body is empty: the user is just naming who should speak; their question is
 *  the rest of the message. The target is the member ROW id (never the name),
 *  so a rename or a duplicate display name cannot misroute it. */
export function buildMentionTag(memberRowId: string): string {
  return `<clawbench-mention targets="${memberRowId}"></clawbench-mention> `
}

/** One staged member card, as the input serializes it on send. */
export interface MentionCardSpec {
  /** The member ROW id (the tag's targets value). */
  memberId: string
  /** The card's annotation — a PRIVATE note to that member. '' = no note. */
  note?: string
}

/**
 * serializeMentionCards turns the input's member cards into the protocol text a
 * send carries. Each card yields ONE public mention (an empty body: the card is
 * a "who should speak" marker — the user's question is the rest of the message),
 * and a card WITH a note yields an additional `private` mention carrying that
 * note (the 密送 channel: only the target may see it).
 *
 * Public tags come first, then the private ones, so the public routing block
 * reads as one contiguous run.
 *
 * A note containing the protocol's own tag syntax is stripped of it: the parser
 * matches spans non-greedily, so an embedded `</clawbench-mention>` would end
 * the span early and leak the remainder as PROSE — visible to every member. The
 * reserved token is simply not representable inside a note body (fail-closed,
 * matching stripAllMentionSpans elsewhere in this module).
 */
export function serializeMentionCards(cards: MentionCardSpec[]): string {
  const seen = new Set<string>()
  const publicParts: string[] = []
  const privateParts: string[] = []
  for (const c of cards) {
    const id = c.memberId
    if (!id || seen.has(id)) continue
    seen.add(id)
    publicParts.push(`<clawbench-mention targets="${id}"></clawbench-mention>`)
    const note = sanitizeNoteBody(c.note)
    if (note) {
      privateParts.push(`<clawbench-mention targets="${id}" private>${note}</clawbench-mention>`)
    }
  }
  return [...publicParts, ...privateParts].join(' ')
}

/**
 * sanitizeNoteBody strips any protocol tag token from a note body so a crafted
 * note cannot terminate its own span and leak the remainder as prose. The
 * protocol's reserved tokens are simply not representable inside a note.
 */
function sanitizeNoteBody(note: string | undefined): string {
  if (!note) return ''
  // Remove any opening/closing protocol token, well-formed or not. Done with a
  // single literal-token scan rather than a tag regex so a malformed/partial
  // token (which the parser would still treat as a span start) is covered too.
  return note
    .replace(/<\/?clawbench-mention\b[^>]*>/gi, '')
    .trim()
}
