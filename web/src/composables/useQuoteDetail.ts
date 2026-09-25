import { ref, computed } from 'vue'
import type { QuoteItem } from '@/utils/quoteItem'

/**
 * Singleton controller for the quote detail drawer.
 *
 * The drawer is mounted ONCE (in ChatPanelContent, outside the message v-for)
 * and opened by handing it a payload, rather than each quote card owning a
 * drawer. ChatMessageItem already mounts two drawers per message via
 * useTabDrawer, so a third per-message drawer would multiply further for no
 * benefit — the drawer shows one quote at a time anyway.
 *
 * `mode` decides what the drawer can do:
 *   - 'staged': the quote is still in the chat input, so the annotation is
 *     edited locally (and removable);
 *   - 'sent': the quote is persisted on a message, so saving the annotation
 *     goes through the PATCH endpoint.
 */
export type QuoteDetailMode = 'staged' | 'sent'

const open = ref(false)
const quote = ref<QuoteItem | null>(null)
const mode = ref<QuoteDetailMode>('staged')

export function useQuoteDetail() {
  return {
    open,
    quote,
    mode,
    /** Whether the annotation is editable in the current mode. */
    editable: computed(() => quote.value !== null),
    openQuoteDetail(item: QuoteItem, opts: { mode: QuoteDetailMode }) {
      quote.value = item
      mode.value = opts.mode
      open.value = true
    },
    close() {
      open.value = false
    },
    /** Reset for tests — the state is module-level. */
    _reset() {
      open.value = false
      quote.value = null
      mode.value = 'staged'
    },
  }
}
