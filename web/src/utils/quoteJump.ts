import type { QuoteItem } from '@/utils/quoteItem'
import { locatorOf, numericLocatorOf, isForgeQuote } from '@/utils/quoteItem'

/**
 * Registry of "jump from a quote card back to its source".
 *
 * ## Why a registry
 *
 * This used to be one `if` chain inside `ChatPanelContent.vue` (a 3300-line
 * component), and the drawer's "跳转来源" button had to stay in sync with it by
 * hand — `canJumpToSource` decided whether to SHOW the button, the chain decided
 * where it GOES. A locator accepted by one and ignored by the other produced a
 * button that appeared and did nothing, and the only guard was a test that read
 * both files as text.
 *
 * Now each source registers one jumper keyed by the locator it needs, and both
 * the button's visibility and the actual navigation come from the SAME list —
 * they cannot disagree.
 *
 * ## Contract
 *
 * - Jumpers run in registration order (most specific first) and the first whose
 *   locator is present wins. A quote can carry several locators (a git-diff
 *   quote has both a file path and a commit); the user who selected a hunk in a
 *   commit view means "that commit", not "that file as it is now".
 * - Returning `true` means handled. `false` means "my locator is present but I
 *   cannot act on it right now" (a disabled feature) — the chain stops there and
 *   reports handled, so it never falls through to opening a label as a path.
 * - A quote with no locator at all falls back to the file branch (the last
 *   entry), which is what a plain file quote needs.
 */
export interface QuoteJumperContext {
  /**
   * Open a project file at a line range. Injected by the host component so this
   * module stays free of navigation singletons (and testable without them).
   */
  openFile: (path: string, startLine: number, endLine: number) => void | Promise<void>
  /** Open an external address in the browser. */
  openUrl: (url: string) => void
  /** True when the forge integration is on (see `isForgeQuote`). */
  forgeEnabled: () => boolean
  /** True when the scheduled-task feature is on. */
  tasksEnabled: () => boolean
  /** Tell the user the feature they just touched is switched off. */
  notifyDisabled: () => void
  /** Switch to a chat session and scroll to one of its messages. */
  openSession: (sessionId: string, messageId?: number) => void
  /** Fire the cross-panel navigation events (commit / task / meeting / mail). */
  dispatch: (event: string, detail: Record<string, unknown>) => void
}

interface JumperEntry {
  /** The locator key that selects this jumper. `null` = the fallback entry. */
  locator: string | null
  /** Human note for the registration table. */
  describe: string
  run: (q: QuoteItem, ctx: QuoteJumperContext) => boolean | Promise<boolean>
}

const jumpers: JumperEntry[] = []

function register(locator: string | null, describe: string, run: JumperEntry['run']): void {
  jumpers.push({ locator, describe, run })
}

// ── Specific locators, most specific first ────────────────────────────────

// Commit: open the git history AT that commit. A quote that also has a file
// path must not open the file instead — the user selected a hunk in a commit.
register('commit', 'git history at a commit', (q, ctx) => {
  ctx.dispatch('navigate-to-commit', { sha: locatorOf(q, 'commit') })
  return true
})

// Task: the scheduled task's detail view, optionally at one execution.
register('task', 'scheduled task detail', (q, ctx) => {
  if (!ctx.tasksEnabled()) {
    ctx.notifyDisabled()
    return true
  }
  const execution = locatorOf(q, 'execution')
  ctx.dispatch('clawbench-open-task', {
    taskId: numericLocatorOf(q, 'task'),
    ...(execution ? { executionId: execution } : {}),
  })
  return true
})

// Meeting: the 会议 tab owns its own view state, so the request travels as an
// event rather than a prop.
register('meeting', 'meeting record', (q, ctx) => {
  ctx.dispatch('clawbench-open-meeting', { token: locatorOf(q, 'meeting') })
  return true
})

// Mail: the 邮件 tab, same shape as the meeting jump. Mail is global, so no
// project switch is involved.
register('mail', 'email in the mail tab', (q, ctx) => {
  ctx.dispatch('clawbench-open-mail', { messageId: numericLocatorOf(q, 'mail') })
  return true
})

// Session (+ optional message): switch to the session and scroll to the
// message. The request is parked in module state by the host because the session
// may still have to load, and may belong to another project.
register('session', 'chat session and message', (q, ctx) => {
  const messageId = numericLocatorOf(q, 'message')
  ctx.openSession(locatorOf(q, 'session'), messageId > 0 ? messageId : undefined)
  return true
})

// ── Address, then file ────────────────────────────────────────────────────

/**
 * External address. Registered as an `url` pseudo-locator: the address lives in
 * `QuoteItem.url` rather than the bag, but the ordering rule is the same.
 *
 * A forge reference is not just a link — it points into the integration that
 * `forge.enabled` turns off, so with the switch off it is refused. A plain
 * external link (a pasted URL, a comment anchor) is not the integration and
 * still opens.
 */
register('url', 'external address', (q, ctx) => {
  if (!q.url) return false
  if (isForgeQuote(q) && !ctx.forgeEnabled()) {
    ctx.notifyDisabled()
    return true
  }
  ctx.openUrl(q.url)
  return true
})

/**
 * Fallback: a project file, opened at its line range.
 *
 * Deliberately LAST and unconditional. A quote whose source kind is
 * meeting/mail but whose locator did not survive must NOT reach here — opening
 * the human-readable label as a path would 404 and look like a broken file. That
 * refusal lives in the guard below rather than in the fallback, because the
 * fallback cannot tell "a file" from "a label that is not a file".
 */
register(null, 'project file at a line range', async (q, ctx) => {
  // `url` joins meeting/mail here: for all three the label is NOT a path, so
  // opening it would 404 and look like a broken file. A url quote that reaches
  // this fallback is one whose address was lost.
  if (q.sourceKind === 'meeting' || q.sourceKind === 'mail' || q.sourceKind === 'url') return false
  if (!q.filePath) return false
  await ctx.openFile(q.filePath, q.startLine, q.endLine)
  return true
})

// ── Public API ────────────────────────────────────────────────────────────

/**
 * Where a quote's "跳转来源" button should take the user, or null when there is
 * nowhere to go.
 *
 * This is the SINGLE source of truth for both questions — whether to show the
 * button and where it goes. They used to be answered by two separate `if` chains
 * in two files, kept aligned by a text-matching test.
 */
/**
 * Which locator would drive the jump for `q`, or null when the fallback (a file
 * path) is what applies.
 *
 * Exposed for diagnostics and tests: the drawer only needs the yes/no answer
 * below, but "which source won" is the question a confusing jump actually asks.
 */
export function quoteJumpLocator(q: QuoteItem): string | null {
  if (q.url) return 'url'
  for (const entry of jumpers) {
    if (entry.locator === null || entry.locator === 'url') continue
    if (locatorOf(q, entry.locator)) return entry.locator
  }
  return null
}

/** Whether the drawer should offer a jump-to-source action. */
export function canJumpQuoteToSource(q: QuoteItem): boolean {
  // The `url` pseudo-locator only counts when there IS an address: a forge quote
  // whose url was lost (an old row) has nothing to open, and the fallback below
  // must not offer its label as a file path either.
  if (q.url) return true
  for (const entry of jumpers) {
    if (entry.locator === null || entry.locator === 'url') continue
    if (locatorOf(q, entry.locator)) return true
  }
  // A meeting/mail quote whose locator did not survive has no addressable
  // source: its label is not a path, so the fallback must not claim it.
  if (q.sourceKind === 'meeting' || q.sourceKind === 'mail') return false
  // A chat-message quote with no session is equally unaddressable.
  if (q.sourceKind === 'message') return false
  if (q.sourceKind === 'selection') return false
  if (q.sourceKind === 'url') return false
  return !!q.filePath
}

/**
 * Run the jump for `q`. Returns true when something handled it.
 *
 * Callers should consult `canJumpQuoteToSource` first; this returns false for a
 * quote with nowhere to go rather than guessing.
 */
export async function jumpToQuoteSource(q: QuoteItem, ctx: QuoteJumperContext): Promise<boolean> {
  if (!q) return false
  for (const entry of jumpers) {
    if (entry.locator === 'url') {
      if (!q.url) continue
    } else if (entry.locator !== null && !locatorOf(q, entry.locator)) {
      continue
    }
    const handled = await entry.run(q, ctx)
    if (handled) return true
  }
  return false
}

/** The registration table, for a guard test that pins ordering. */
export function quoteJumperTable(): ReadonlyArray<{ locator: string | null; describe: string }> {
  return jumpers.map(j => ({ locator: j.locator, describe: j.describe }))
}
