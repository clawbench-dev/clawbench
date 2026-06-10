/**
 * Pure functions extracted from ContentBlocks.vue for testability.
 * These are stateless utility functions with no Vue reactivity dependencies.
 */

/** Reasons that indicate a severe issue (red error-level styling) */
const SEVERE_REASONS = new Set(['disconnect', 'timeout', 'restart', 'panic'])

/**
 * Check if a warning block represents a severe issue.
 * Severe warnings render with red/error-level styling.
 */
export function isSevereWarning(block: { reason?: string }): boolean {
  return SEVERE_REASONS.has(block.reason || '')
}

/**
 * Get localized warning/error text.
 * Uses reason code to look up i18n key, falls back to block.text.
 * For parse_error and backend_exit, appends detail after colon/newline.
 */
export function getWarningText(
  block: { reason?: string; text?: string },
  t: (key: string) => string
): string {
  if (block.reason) {
    const key = `chat.contentBlocks.warningReasons.${block.reason}`
    const translated = t(key)
    // t() returns the key itself when not found — fall back to block.text
    if (translated !== key) {
      // For parse_error: append detail after ": " from block.text
      // For backend_exit: append stderr after "\n" from block.text
      if ((block.reason === 'parse_error' || block.reason === 'backend_exit') && block.text) {
        const newlineIdx = block.text.indexOf('\n')
        if (newlineIdx >= 0) {
          return translated + block.text.substring(newlineIdx)
        }
        const colonIdx = block.text.indexOf(': ')
        if (colonIdx >= 0) {
          return translated + ': ' + block.text.substring(colonIdx + 2)
        }
      }
      return translated
    }
  }
  // Fallback: no reason code or no matching i18n key
  return block.text || ''
}

/**
 * Get CSS class for a task's status indicator.
 */
export function statusClass(task: { status: string }): string {
  if (task.status === 'active') return 'status-active'
  if (task.status === 'paused') return 'status-paused'
  if (task.status === 'completed') return 'status-completed'
  return ''
}

/**
 * Get detailed status label for a scheduled task.
 */
export function statusLabel(
  task: { status: string; runCount: number; runningCount: number },
  t: (key: string, params?: Record<string, any>) => string
): string {
  if (task.status === 'active') {
    const execLabel = t('chat.contentBlocks.statusExecutions', { count: task.runCount })
    if (task.runningCount > 0) return `${t('chat.contentBlocks.statusRunning')} (${execLabel})`
    return `${t('chat.contentBlocks.statusActive')} (${execLabel})`
  }
  if (task.status === 'paused') return t('chat.contentBlocks.statusPaused')
  if (task.status === 'completed') return t('chat.contentBlocks.statusCompleted')
  return task.status
}

/**
 * Get simple (short) status label for a scheduled task badge.
 */
export function statusLabelSimple(
  task: { status: string },
  t: (key: string) => string
): string {
  if (task.status === 'active') return t('chat.contentBlocks.statusActive')
  if (task.status === 'paused') return t('chat.contentBlocks.statusPaused')
  if (task.status === 'completed') return t('chat.contentBlocks.statusCompleted')
  return task.status
}

/**
 * Format an ISO timestamp into a human-readable relative or absolute time string.
 * - < 1 min: "just now"
 * - < 1 hour: "X minutes ago/from now"
 * - < 1 day: "X hours ago/from now"
 * - else: locale date string
 */
export function formatTime(
  iso: string | null | undefined,
  locale: string,
  t: (key: string, params?: Record<string, any>) => string
): string {
  if (!iso) return ''
  const d = new Date(iso)
  const now = new Date()
  const diff = d.getTime() - now.getTime()
  const absDiff = Math.abs(diff)
  if (absDiff < 60000) return t('chat.contentBlocks.justNow')
  if (absDiff < 3600000) {
    const count = Math.round(absDiff / 60000)
    return diff > 0
      ? t('chat.contentBlocks.minutesFromNow', { count })
      : t('chat.contentBlocks.minutesAgo', { count })
  }
  if (absDiff < 86400000) {
    const count = Math.round(absDiff / 3600000)
    return diff > 0
      ? t('chat.contentBlocks.hoursFromNow', { count })
      : t('chat.contentBlocks.hoursAgo', { count })
  }
  return d.toLocaleDateString(locale === 'zh' ? 'zh-CN' : 'en-US')
}

/**
 * Generate a short summary for an ask-question block.
 * Returns the first question's header if available, otherwise the question text.
 */
export function askQuestionSummary(input: any): string {
  if (!input || !Array.isArray(input.questions) || input.questions.length === 0) return ''
  const q = input.questions[0]
  const header = q.header || ''
  const question = q.question || ''
  if (header) return header
  return question
}

/**
 * Build a block key for DOM rendering and tool expand state tracking.
 * Uses msgId if available, otherwise msgIndex.
 */
export function blockKey(msgId: string | number, bi: number): string {
  return msgId ? `db-${msgId}-${bi}` : `local-${bi}`
}

/**
 * Build a key for blockTasks/blockAskQuestions lookup.
 * Prefix format: "msgId-blockIdx"
 */
export function blockTaskKey(msgId: string | number, bi: number): string {
  return `${msgId}-${bi}`
}

/**
 * Build an index: block index → sorted array of scheduled task keys.
 * This pre-computes the mapping to avoid O(n) scan per block per render.
 */
export function buildTaskKeyIndex(
  msgId: string | number | undefined,
  blockTasks: Record<string, any>
): Record<string, string[]> {
  if (!msgId) return {}
  const index: Record<string, string[]> = {}
  const prefix = `${msgId}-`
  for (const k of Object.keys(blockTasks)) {
    if (!k.startsWith(prefix)) continue
    const rest = k.slice(prefix.length)
    const dashIdx = rest.indexOf('-')
    if (dashIdx === -1) continue
    const bi = rest.slice(0, dashIdx)
    ;(index[bi] || (index[bi] = [])).push(k)
  }
  // Sort each group by key (tag index is already part of the key string)
  for (const bi of Object.keys(index)) index[bi].sort()
  return index
}

/**
 * Check if a block has any scheduled tasks based on the pre-computed index.
 */
export function hasScheduledTasks(
  taskKeyIndex: Record<string, string[]>,
  bi: string | number
): boolean {
  return !!(taskKeyIndex[bi]?.length)
}

/**
 * Return all scheduled task keys for a block, sorted by tag index.
 */
export function scheduledTaskKeys(
  taskKeyIndex: Record<string, string[]>,
  bi: string | number
): string[] {
  return taskKeyIndex[bi] || []
}

// ────────────────────────────────────────────────────────────
// @ command badge detection
// ────────────────────────────────────────────────────────────

/** Match @ command prefix at start of text: @chatsearch or @task followed by space */
const AT_COMMAND_RE = /^(@chatsearch|@task)(\s[\s\S]*)?$/

export interface AtCommandBadge {
  command: string    // e.g. "@chatsearch"
  rest: string       // e.g. " my query" (including the leading space) or ""
}

/**
 * Extract @ command prefix from a text block.
 * Returns null if the text doesn't start with an @ command.
 */
export function extractAtCommand(text: string): AtCommandBadge | null {
  if (!text.startsWith('@')) return null
  const match = text.match(AT_COMMAND_RE)
  if (!match) return null
  return { command: match[1], rest: match[2] || '' }
}

// ────────────────────────────────────────────────────────────
// Slash command badge detection (ACP backend commands)
// ────────────────────────────────────────────────────────────

/** Match slash command prefix at start of text: /command-name (with optional space+rest) */
const SLASH_COMMAND_RE = /^\/(\w[\w:-]*)(\s[\s\S]*)?$/

export interface SlashCommandBadge {
  command: string    // e.g. "/commit"
  rest: string       // e.g. " fix auth bug" (including the leading space) or ""
}

/**
 * Extract slash command prefix from a text block.
 * Returns null if the text doesn't start with a slash command.
 * Unlike @ commands, slash commands are dynamic (from ACP) — any /word match is valid.
 */
export function extractSlashCommand(text: string): SlashCommandBadge | null {
  if (!text.startsWith('/')) return null
  const match = text.match(SLASH_COMMAND_RE)
  if (!match) return null
  return { command: '/' + match[1], rest: match[2] || '' }
}
