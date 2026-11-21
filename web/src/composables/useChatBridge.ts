/**
 * One-call surface for putting something from a panel into the chat.
 *
 * Every step already existed, but a new panel had to know which of five
 * modules owned which step, and "quote this into the conversation" had no
 * generic path at all: the knowledge-base panel had to park its payload in a
 * bespoke module-level ref (`kbNavigation`) for App.vue to pick up, because a
 * dock panel cannot switch the app to the chat view itself. That is the gap
 * this closes.
 *
 * Call it in `setup()` (it borrows `useQuoteQuestion`, whose document listeners
 * are installed on mount):
 *
 *   const chat = useChatBridge()
 *   chat.quote(payload)               // stage it in the current conversation
 *   chat.comment(payload, { anchorEl: btn })  // open the bar to annotate it
 *   chat.attach('src/a.ts')           // ask which conversation (file-manager behaviour)
 *   chat.attach('src/a.ts', { to: 'active' })
 *
 * For drag-and-drop, use `v-chat-drag` instead of these methods.
 */
import { useChatContext, type QuoteData } from './useChatContext'
import { useQuoteQuestion } from './useQuoteQuestion'
import { requestAttachmentTarget } from './useConversationTarget'
import { useToast } from './useToast'
import { gt } from './useLocale'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

/**
 * How to bring the chat pane into view.
 *
 * Injected by App.vue: revealing the pane is a tab switch, and only App owns
 * that. Module-level (like `setOpenSessionHandler` in useConversationTarget) so
 * the bridge stays usable from any panel without a prop chain.
 */
let revealChat: (() => void) | null = null

export function setChatRevealHandler(fn: (() => void) | null): void {
  revealChat = fn
}

export interface QuoteIntoChatOptions {
  /** Optional annotation that travels with the card. */
  note?: string
  /**
   * Bring the chat pane into view after staging (default true).
   *
   * Turn it off when the caller is already showing the chat, or when jumping
   * would abandon something the user is in the middle of.
   */
  reveal?: boolean
}

export interface CommentOnChatOptions {
  /** The control that opened the bar; it floats next to this when given. */
  anchorEl?: Element | null
  /** Card label. Defaults to the payload's `filePath`. */
  label?: string
}

export interface AttachIntoChatOptions {
  /**
   * `pick` (default) asks which conversation, matching the file manager's
   * "add to session" button. `active` stages it in the current one, matching
   * the inline attach buttons on a document being read.
   */
  to?: 'active' | 'pick'
}

/** Accept a path, an entry, or a mixed list, and drop anything unusable. */
function normalizeEntries(input: string | FileEntry | (string | FileEntry)[]): FileEntry[] {
  const list = Array.isArray(input) ? input : [input]
  const entries: FileEntry[] = []
  for (const item of list) {
    if (typeof item === 'string') {
      if (item) entries.push({ path: item, isDir: false })
    } else if (item?.path) {
      entries.push(item)
    }
  }
  return entries
}

export function useChatBridge() {
  const chatContext = useChatContext()
  const quoteQuestion = useQuoteQuestion()
  const toast = useToast()

  /**
   * Stage a quote in the conversation the user is working in, right now.
   *
   * This is the "one-click" path (no bar, no annotation, no destination
   * picker). The payload must be COMPLETE: a source whose content has to be
   * fetched should use `comment` (which keeps the bar open while it composes)
   * or a drag (whose drop site hydrates it through `quoteHydrate`).
   */
  function quote(payload: QuoteData | null | undefined, opts: QuoteIntoChatOptions = {}): void {
    if (!payload) return
    chatContext.addStagedQuote(payload, opts.note ?? '')
    if (opts.reveal !== false) revealChat?.()
    toast.show(gt('chat.attach.quotedToChat', { label: payload.filePath }), {
      icon: 'chat',
      type: 'success',
      duration: 1800,
    })
  }

  /**
   * Open the quote bar on a payload so the user can add a note first.
   *
   * The card is only staged when the user commits, so dismissing the bar leaves
   * the draft untouched — the same contract every existing composer entry
   * point has.
   */
  function comment(payload: QuoteData | null | undefined, opts: CommentOnChatOptions = {}): void {
    if (!payload) return
    quoteQuestion.openComposer(
      { quote: payload, label: opts.label || payload.filePath },
      { anchorEl: opts.anchorEl ?? null },
    )
  }

  /**
   * Attach local files/directories to a conversation.
   *
   * Goes through the same dispatcher as every other attach entry point, so the
   * "already attached" and multi-select behaviour are identical.
   */
  function attach(
    input: string | FileEntry | (string | FileEntry)[] | null | undefined,
    opts: AttachIntoChatOptions = {},
  ): void {
    if (!input) return
    const entries = normalizeEntries(input)
    if (!entries.length) return
    requestAttachmentTarget(entries, opts.to === 'active' ? { deliverTo: 'active' } : {})
  }

  return { quote, comment, attach }
}
