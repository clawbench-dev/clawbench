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
 * - ask-question detection (with early exit optimization)
 * - task semantic comparison (for blockTasks watcher)
 * - static block cache (for non-streaming re-renders)
 */

// ────────────────────────────────────────────────────────────
// Module-level scheduled-task regex
// ────────────────────────────────────────────────────────────

/** Regex to match <scheduled-task id="..." /> tags with integer IDs. */
const SCHEDULED_TASK_RE = /<scheduled-task\s+id="(\d+)"\s*\/>/gi

/**
 * Extract scheduled task IDs from text.
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
// ask-question detection (with early exit)
// ────────────────────────────────────────────────────────────

/**
 * Validate that <ask-question> content looks like a real structured payload.
 * Supports both XML format (with <item> child elements) and JSON format
 * (with "questions" array containing objects with "question" and "options").
 * Only called post-streaming.
 */
export function isValidAskContent(raw: string): boolean {
  const probe = raw.trim()
  // XML format: check for <item> child elements
  if (probe.includes('<item>') || probe.includes('<item ')) {
    // Basic validation: must have at least a <question> and <option> inside
    return probe.includes('<question>') && probe.includes('<option>')
  }
  // JSON format: check for "questions" key with array
  if (probe.startsWith('{') && probe.includes('"questions"')) {
    try {
      const data = JSON.parse(probe)
      return Array.isArray(data.questions) && data.questions.length > 0
        && data.questions.some((q: any) => q.question && Array.isArray(q.options) && q.options.length > 0)
    } catch {
      return false
    }
  }
  return false
}

export interface AskQuestionResult {
  found: boolean
  content?: string
  startIdx?: number
  endIdx?: number
}

/**
 * Detect <ask-question> tags in text with early exit optimization.
 * Returns result with found=false immediately if '<ask-question' is not in the text.
 * Only called post-streaming.
 */
export function detectAskQuestion(text: string): AskQuestionResult {
  // Fast path: skip entire detection if tag substring not present
  if (!text.includes('<ask-question')) {
    return { found: false }
  }

  // Full detection: matchAll + up to 3 regex patterns + JSON.parse validation
  const allOpenTags = [...text.matchAll(/<ask-question\b[^>]*>/g)]
  for (let j = allOpenTags.length - 1; j >= 0; j--) {
    const startIdx = allOpenTags[j].index!
    const afterTag = text.slice(startIdx)

    const closedMatch = afterTag.match(/<ask-question\b[^>]*>([\s\S]*?)<\/ask-question>/)
    if (closedMatch && isValidAskContent(closedMatch[1])) {
      return { found: true, content: closedMatch[1], startIdx, endIdx: startIdx + closedMatch[0].length }
    }

    // Match wrong/obfuscated close tags — some models emit non-standard closing tags
    // (e.g. </｜｜DSML｜｜question> with fullwidth pipe chars). Use [^>]+ instead of
    // \w+ to catch any character sequence that looks like a closing tag.
    const wrongCloseMatch = afterTag.match(/<ask-question\b[^>]*>([\s\S]*?)<\/[^>]+>/)
    if (wrongCloseMatch && isValidAskContent(wrongCloseMatch[1])) {
      return { found: true, content: wrongCloseMatch[1], startIdx, endIdx: startIdx + wrongCloseMatch[0].length }
    }

    const subMatch = afterTag.match(/<ask-question\b[^>]*>([\s\S]+)$/)
    if (subMatch && isValidAskContent(subMatch[1])) {
      return { found: true, content: subMatch[1], startIdx }
    }
  }

  return { found: false }
}

// ────────────────────────────────────────────────────────────
// rag-results detection (re-export from xmlParser)
// ────────────────────────────────────────────────────────────

export { detectRagResults, stripRagResultsTags } from '@/utils/xmlParser.ts'

// ────────────────────────────────────────────────────────────
// Task semantic comparison (for blockTasks watcher)
// ────────────────────────────────────────────────────────────

/** Key fields to compare for semantic equality of a scheduled task. */
const TASK_COMPARE_KEYS = [
  'status', 'name', 'cronExpr', 'runCount',
  'lastRunAt', 'nextRunAt', 'runningCount',
  'repeatMode', 'maxRuns', 'agentId',
] as const

/**
 * Compare two task objects by semantic key fields.
 * Returns true if any key field differs (or either is null).
 */
export function taskChanged(oldTask: any, newTask: any): boolean {
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
      : (cb: () => void) => setTimeout(cb, 1)
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
