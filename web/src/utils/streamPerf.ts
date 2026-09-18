/**
 * Streaming render utilities for useChatRender.
 *
 * Core design: During streaming, renderTextBlock only does pure markdown
 * rendering (marked + DOMPurify + table-wrap). All structured detection
 * (KaTeX, Mermaid, scheduled-task, ask-question, file path annotation)
 * is deferred to after streaming ends.
 *
 * This module provides the pure functions used in the post-streaming
 * full pipeline:
 *
 * - scheduled-task regex (module-level, reused across calls)
 * - ask-question detection (delegates to the canonical askQuestion module)
 * - task semantic comparison (for blockTasks watcher)
 * - static block cache (for non-streaming re-renders)
 *
 * The ask-question functions are thin adapters over `@/utils/askQuestion.ts`,
 * which is the single implementation shared with the Go backend
 * (`internal/askquestion`). They are kept here so existing call sites and
 * tests keep working, but they no longer hold any parsing logic of their own —
 * that divergence was the source of the silent content-loss defect.
 */

import {
  extractAskMatches,
  parseItems,
  stripAskMatches,
  hasParsedMatches,
  allMatchItems,
  unparsedReasons,
  type AskItem,
  type AskMatch,
} from '@/utils/askQuestion.ts'

// ────────────────────────────────────────────────────────────
// Module-level scheduled-task regex
// ────────────────────────────────────────────────────────────

/** Regex to match <scheduled-task id="..." /> tags with integer IDs. */
const SCHEDULED_TASK_RE = /<scheduled-task\s+id="(\d+)"\s*\/>/gi

/**
 * Extract task IDs from text.
 * Resets the module-level regex lastIndex before use (required due to 'g' flag).
 * Only called post-streaming.
 */
export function extractScheduledTaskIds(text: string): string[] {
  const ids: string[] = []
  SCHEDULED_TASK_RE.lastIndex = 0
  let match
  while ((match = SCHEDULED_TASK_RE.exec(text)) !== null) {
    ids.push(match[1])
  }
  return ids
}

/**
 * Strip <scheduled-task .../> tags from text.
 * Resets the module-level regex lastIndex before use.
 * Only called post-streaming.
 */
export function stripScheduledTaskTags(text: string): string {
  SCHEDULED_TASK_RE.lastIndex = 0
  return text.replace(SCHEDULED_TASK_RE, '').trim()
}

// ────────────────────────────────────────────────────────────
// ask-question detection (delegates to @/utils/askQuestion.ts)
// ────────────────────────────────────────────────────────────

/**
 * Validate that <ask-question> content looks like a real structured payload.
 *
 * Now defined as "the payload actually parses", which is what makes detection
 * and parsing impossible to disagree. The previous implementation did literal
 * substring checks (`includes('<question>')`), so it accepted payloads the
 * strict parser then rejected — and the caller stripped the tag anyway,
 * deleting the question.
 */
export function isValidAskContent(raw: string): boolean {
  return parseAskItems(raw).length > 0
}

/** The canonical item shape (re-exported for callers that only need the type). */
export type { AskItem }

/** Parsed items for a payload — the single parse entry point. */
function parseAskItems(raw: string): AskItem[] {
  // Callers pass either a bare payload or a wrapped <ask-question> block.
  // The canonical module handles both, so try the wrapped form first (which
  // also applies the code-context and span-bounding rules) and fall back to
  // treating the whole string as the payload.
  const fromMatches = allMatchItems(extractAskMatches(raw))
  if (fromMatches.length > 0) return fromMatches
  return parseItems(raw)
}

export interface AskQuestionResult {
  /** True when at least one tag parsed into items. */
  found: boolean
  /** Items from every parsed tag, in document order. */
  items: AskItem[]
  /**
   * Every located span, including unparseable ones. Callers must pass this to
   * stripAskQuestionTag so unparseable spans are retained rather than deleted.
   */
  matches: AskMatch[]
  /** Reason codes for spans that failed to parse (for logging). */
  reasons: string[]
}

/**
 * Detect <ask-question> tags in text.
 * Skips tags that appear inside fenced code blocks or inline backticks, and
 * returns every tag rather than only the last one.
 * Only called post-streaming.
 */
export function detectAskQuestion(text: string): AskQuestionResult {
  const matches = extractAskMatches(text)
  return {
    found: hasParsedMatches(matches),
    items: allMatchItems(matches),
    matches,
    reasons: unparsedReasons(matches),
  }
}

/**
 * Remove the successfully parsed <ask-question> spans from text.
 *
 * Unparseable spans are deliberately retained: their raw text is the only
 * remaining copy of the question, and deleting it was the silent content-loss
 * defect. `result.matches` carries the parse outcome, so a span that failed is
 * left untouched.
 */
export function stripAskQuestionTag(text: string, result: AskQuestionResult): string {
  if (result.matches.length === 0) return text
  return stripAskMatches(text, result.matches).trim()
}

// ────────────────────────────────────────────────────────────
// Task semantic comparison (for blockTasks watcher)
// ────────────────────────────────────────────────────────────

/** Key fields to compare for semantic equality of a task. */
const TASK_COMPARE_KEYS = [
  'status', 'name', 'cronExpr', 'runCount',
  'lastRunAt', 'nextRunAt', 'runningCount',
  'repeatMode', 'maxRuns', 'agentId',
] as const

/**
 * Compare two task objects by semantic key fields.
 * Returns true if any key field differs (or either is null).
 */
export function taskChanged(oldTask: Record<string, unknown>, newTask: Record<string, unknown>): boolean {
  if (!oldTask || !newTask) return true
  for (const key of TASK_COMPARE_KEYS) {
    if (oldTask[key] !== newTask[key]) return true
  }
  return false
}

// ────────────────────────────────────────────────────────────
// Static block cache (for non-streaming re-renders)
// ────────────────────────────────────────────────────────────

/**
 * Cache for non-streaming block HTML rendering.
 * Prevents redundant renderTextBlock calls when Vue re-renders
 * already-completed message blocks.
 * Supports a "fast path" mode: when deferEnhancements is true,
 * blocks are initially cached with skipEnhancements=true and
 * scheduled for upgrade to the full pipeline via requestIdleCallback.
 */
export class StaticBlockCache {
  private cache = new Map<string, string>()
  // Tracks which cache entries were rendered without enhancements
  private deferredKeys = new Set<string>()
  private upgradeScheduled = false
  private upgradeFn: (() => void) | null = null

  private makeKey(msgId: string | number, blockIdx: number, text: string): string {
    const prefix = text.length > 40 ? text.slice(0, 20) : ''
    const suffix = text.slice(-20)
    return `${msgId}-${blockIdx}-${text.length}-${prefix}${suffix}`
  }

  get(msgId: string | number, blockIdx: number, text: string): string | undefined {
    return this.cache.get(this.makeKey(msgId, blockIdx, text))
  }

  set(msgId: string | number, blockIdx: number, text: string, html: string, deferred = false): void {
    const key = this.makeKey(msgId, blockIdx, text)
    this.cache.set(key, html)
    if (deferred) {
      this.deferredKeys.add(key)
    }
  }

  /** Mark an entry as upgraded from deferred to full render */
  markUpgraded(msgId: string | number, blockIdx: number, text: string): void {
    this.deferredKeys.delete(this.makeKey(msgId, blockIdx, text))
  }

  /** Check if an entry was rendered with deferred enhancements */
  isDeferred(msgId: string | number, blockIdx: number, text: string): boolean {
    return this.deferredKeys.has(this.makeKey(msgId, blockIdx, text))
  }

  /** Set the function to call when deferred entries need upgrading */
  setUpgradeFn(fn: () => void): void {
    this.upgradeFn = fn
  }

  /** Schedule upgrade of deferred entries using requestIdleCallback */
  scheduleUpgrade(): void {
    if (this.upgradeScheduled || this.deferredKeys.size === 0) return
    this.upgradeScheduled = true
    const schedule = typeof requestIdleCallback !== 'undefined'
      ? requestIdleCallback
      : (cb: () => void) => requestAnimationFrame(cb)
    schedule(() => {
      this.upgradeScheduled = false
      if (this.upgradeFn && this.deferredKeys.size > 0) {
        this.upgradeFn()
      }
    })
  }

  /** Get the number of pending deferred entries */
  get deferredCount(): number {
    return this.deferredKeys.size
  }

  clear(): void {
    this.cache.clear()
    this.deferredKeys.clear()
    this.upgradeScheduled = false
  }
}
