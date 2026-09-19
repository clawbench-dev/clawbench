/**
 * Pure functions extracted from ContentBlocks.vue for testability.
 * These are stateless utility functions with no Vue reactivity dependencies.
 */

/** Reasons that indicate a severe issue (red error-level styling) */
const SEVERE_REASONS = new Set(['disconnect', 'timeout', 'panic'])

/**
 * Check if a warning block represents a severe issue.
 * Severe warnings render with red/error-level styling.
 */
export function isSevereWarning(block: { reason?: string }): boolean {
  return SEVERE_REASONS.has(block.reason || '')
}

/**
 * Format the structured error code suffix for a warning/error block.
 * Returns "" when no structured error info is present.
 * Prefers the upstream HTTP status when both are present (more actionable
 * for provider errors, e.g. "500"), otherwise falls back to the JSON-RPC /
 * internal error code (e.g. "-32603").
 */
export function formatErrorCode(
  block: { error_code?: number; http_status?: number }
): string {
  if (block.http_status && block.http_status > 0) {
    return ` [HTTP ${block.http_status}]`
  }
  if (block.error_code && block.error_code !== 0) {
    return ` [code ${block.error_code}]`
  }
  return ''
}

/**
 * Get localized warning/error text.
 * Uses reason code to look up i18n key, falls back to block.text.
 * For parse_error and backend_exit, appends detail after colon/newline.
 * For request_failed, appends the human-readable error detail.
 * Appends a structured error-code suffix (e.g. " [HTTP 500]") when present.
 * When the agent reported its own reason (error_detail), that is appended
 * after the localized label so a placeholder code like -32603 is not the only
 * thing the user sees.
 */
export function getWarningText(
  block: { reason?: string; text?: string; error_code?: number; http_status?: number; error_detail?: string },
  t: (key: string) => string
): string {
  const codeSuffix = formatErrorCode(block)
  // An agent-reported detail is strictly more informative than the localized
  // label alone; the code suffix still follows it so both remain visible.
  const detailSuffix = block.error_detail ? ': ' + block.error_detail : ''
  if (block.reason) {
    const key = `chat.contentBlocks.warningReasons.${block.reason}`
    const translated = t(key)
    // t() returns the key itself when not found — fall back to block.text
    if (translated !== key) {
      // For parse_error: append detail after ": " from block.text
      // For backend_exit: append stderr after "\n" from block.text
      // For request_failed: append human-readable error detail
      if ((block.reason === 'parse_error' || block.reason === 'backend_exit') && block.text) {
        const newlineIdx = block.text.indexOf('\n')
        if (newlineIdx >= 0) {
          return translated + block.text.substring(newlineIdx) + codeSuffix
        }
        const colonIdx = block.text.indexOf(': ')
        if (colonIdx >= 0) {
          return translated + ': ' + block.text.substring(colonIdx + 2) + codeSuffix
        }
      }
      if (block.reason === 'request_failed' && block.text) {
        return translated + ': ' + block.text + codeSuffix
      }
      return translated + detailSuffix + codeSuffix
    }
  }
  // Fallback: no reason code or no matching i18n key
  return (block.text || '') + detailSuffix + codeSuffix
}

/**
 * Get the localized source label for a warning/error block.
 * Distinguishes errors originating from the AI agent vs ClawBench itself
 * vs network/transport issues. Returns "" when no source is tagged.
 */
export function getErrorSourceLabel(
  block: { error_source?: string },
  t: (key: string) => string
): string {
  if (!block.error_source) return ''
  const key = `chat.contentBlocks.errorSources.${block.error_source}`
  const translated = t(key)
  return translated === key ? '' : translated
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
 * Get detailed status label for a task.
 */
export function statusLabel(
  task: { status: string; runCount: number; runningCount: number },
  t: (key: string, params?: Record<string, unknown>) => string
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
 * Get simple (short) status label for a task badge.
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
  t: (key: string, params?: Record<string, unknown>) => string
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
export function askQuestionSummary(input: Record<string, unknown>): string {
  if (!input || !Array.isArray(input.questions) || input.questions.length === 0) return ''
  const q = input.questions[0]
  const header = q.header || ''
  const question = q.question || ''
  if (header) return header
  return question
}

/**
 * A single ask-question entry is renderable when it carries question text or
 * at least one option. Mirrors the 'valid' branch of classifyAskQuestionsInput
 * (renderToolDetail.ts) without importing it (keeps this module dependency-free).
 */
function isRenderableQuestion(q: unknown): boolean {
  if (!q || typeof q !== 'object' || Array.isArray(q)) return false
  const entry = q as Record<string, unknown>
  const hasQuestion = typeof entry.question === 'string' && entry.question.trim() !== ''
  const hasOptions = Array.isArray(entry.options) && entry.options.length > 0
  return hasQuestion || hasOptions
}

/**
 * Extract the renderable questions from an AskUserQuestion tool input.
 * Returns [] for non-objects, a missing/wrongly-typed `questions` field, or an
 * array with no renderable entry (an unanswerable/malformed call).
 */
export function extractAskQuestions(input: unknown): Array<Record<string, unknown>> {
  if (!input || typeof input !== 'object' || Array.isArray(input)) return []
  const questions = (input as Record<string, unknown>).questions
  if (!Array.isArray(questions)) return []
  return questions.filter(isRenderableQuestion) as Array<Record<string, unknown>>
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
 * Build an index: block index → sorted array of task keys.
 * This pre-computes the mapping to avoid O(n) scan per block per render.
 */
export function buildTaskKeyIndex(
  msgId: string | number | undefined,
  blockTasks: Record<string, unknown>
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
 * Check if a block has any tasks based on the pre-computed index.
 */
export function hasScheduledTasks(
  taskKeyIndex: Record<string, string[]>,
  bi: string | number
): boolean {
  return !!(taskKeyIndex[bi]?.length)
}

/**
 * Return all task keys for a block, sorted by tag index.
 */
export function scheduledTaskKeys(
  taskKeyIndex: Record<string, string[]>,
  bi: string | number
): string[] {
  return taskKeyIndex[bi] || []
}

// ────────────────────────────────────────────────────────────
// Slash command badge detection (agent + ClawBench built-in commands)
// ────────────────────────────────────────────────────────────

/** Match slash command prefix at start of text: /command-name (with optional space+rest) */
const SLASH_COMMAND_RE = /^\/(\w[\w:-]*)(\s[\s\S]*)?$/

/**
 * ClawBench built-in commands are namespaced under "/cb-" so they can be
 * distinguished from the current agent's ACP commands (e.g. "/compact").
 * Mirrors the backend constants in internal/handler/clawbench_command.go.
 *
 * The names are listed explicitly rather than matched as a bare "/cb-" prefix:
 * an agent command that happens to start with "cb-" must still render as an
 * agent badge (see the /cb-something case in contentBlocks.test.ts).
 */
const CLAWBENCH_COMMAND_RE = /^\/cb-(chatsearch|task|usage)(\s|$)/

export interface SlashCommandBadge {
  command: string    // e.g. "/commit"
  rest: string       // e.g. " fix auth bug" (including the leading space) or ""
  clawbench: boolean // true for ClawBench built-in /cb-* commands
}

/**
 * Extract slash command prefix from a text block.
 * Returns null if the text doesn't start with a slash command.
 * Covers both agent commands (dynamic, from ACP) and ClawBench built-ins.
 */
export function extractSlashCommand(text: string): SlashCommandBadge | null {
  if (!text.startsWith('/')) return null
  const match = text.match(SLASH_COMMAND_RE)
  if (!match) return null
  return {
    command: '/' + match[1],
    rest: match[2] || '',
    clawbench: CLAWBENCH_COMMAND_RE.test(text),
  }
}
