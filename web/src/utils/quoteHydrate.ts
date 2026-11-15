import type { QuoteData } from '@/composables/useChatContext'
import { locatorOf } from '@/utils/quoteItem'

/**
 * Fill in a dragged quote whose content cannot travel in the drag itself.
 *
 * ## Why this exists
 *
 * An HTML5 `dragstart` must write the DataTransfer SYNCHRONOUSLY, so a drag can
 * only carry identity (a meeting token, a mail id) — never the minutes text or
 * the email body, both of which live behind an HTTP request. The drop handler
 * therefore has to fetch the content before it can stage a card.
 *
 * ## Why a registry
 *
 * That fetch used to be two hand-written `if` chains, one in App.vue's
 * chat-column drop and one in SessionList.vue's session-row drop:
 *
 *     if (quote.meetingToken) { …fetch meeting… }
 *     if (quote.mailId)        { …fetch mail… }
 *
 * Adding a source meant editing both, and forgetting the second one produced a
 * drag that worked onto the chat column and silently did nothing when dropped on
 * a session row. The spec doc only ever mentioned App.vue, so the omission was
 * easy to make and impossible to notice.
 *
 * Now a source registers its hydrator once, keyed by the LOCATOR name it carries
 * (`meeting`, `mail`, …), and both drop sites call `hydrateDraggedQuote`. A new
 * source that needs fetching adds one entry here and nothing else.
 *
 * ## Contract
 *
 * - Hydrators run in registration order, and the FIRST whose locator is present
 *   wins. A quote that carries both `meeting` and `mail` is a bug upstream; this
 *   picks deterministically rather than fetching twice.
 * - Returning `null` means "this source cannot produce a quote right now" (a
 *   meeting with no artifacts yet). The caller then drops the drag silently —
 *   the same outcome as the old `if (!quote) return`.
 * - A quote with no registered locator is returned UNCHANGED, so a source whose
 *   content already travelled in the payload (a commit, a task) needs no
 *   hydrator at all.
 */
export type QuoteHydrator = (locatorValue: string, payload: QuoteData) => Promise<QuoteData | null>

interface HydratorEntry {
  /** The locator key that selects this hydrator. */
  locator: string
  hydrate: QuoteHydrator
}

const hydrators: HydratorEntry[] = []

/**
 * Register a hydrator for one locator key.
 *
 * Called at module scope by the source that owns it (see `quoteHydrators.ts`),
 * not at runtime — the list is fixed for the life of the app.
 */
export function registerQuoteHydrator(locator: string, hydrate: QuoteHydrator): void {
  hydrators.push({ locator, hydrate })
}

/**
 * Resolve a dragged quote into a stageable one, fetching content if its source
 * requires it.
 *
 * Returns the payload unchanged when no hydrator claims it (the common case: a
 * commit/task/issue drag already carries everything the card needs), and `null`
 * when the owning source says there is nothing to quote.
 */
export async function hydrateDraggedQuote(payload: QuoteData): Promise<QuoteData | null> {
  for (const entry of hydrators) {
    const value = locatorOf(payload, entry.locator)
    if (!value) continue
    return entry.hydrate(value, payload)
  }
  return payload
}

/** Test-only: clear the registry so a spec can register its own doubles. */
export function resetQuoteHydratorsForTest(): void {
  hydrators.length = 0
}
