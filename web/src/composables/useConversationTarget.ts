import { ref } from 'vue'
import { useSessionIdentity } from '@/composables/useSessionIdentity.ts'
import { useChatContext } from '@/composables/useChatContext.ts'
import { useToast } from '@/composables/useToast.ts'
import { gt } from '@/composables/useLocale'
import { store } from '@/stores/app.ts'
import { isChatPanelVisible, useWideScreenLayout } from '@/composables/useWideScreenLayout.ts'
import { materializeQuotes } from '@/utils/quoteItem.ts'
import type { QuoteData } from '@/composables/useChatContext.ts'
import type { FileEntry } from '@/utils/fileAttachmentUtils'

/**
 * A quote captured for deferred delivery: the raw payload, its annotation, and
 * an id minted at capture time.
 *
 * The id matters for the SEND path: `toFileEntry` carries it to the backend,
 * which addresses the entry by it when the annotation is edited after sending.
 * An id-less quote would send fine but be uneditable afterwards.
 */
export interface PendingQuote extends QuoteData {
  note: string
  id: string
}

/**
 * Where a quote / attachment should be delivered.
 *
 *  - `current`  — the session already on screen (the pre-existing behavior).
 *  - `session`  — a session the user picked in the picker.
 *  - `create`   — a brand-new session, created with the DEFAULT agent.
 */
export type SessionTarget =
  | { kind: 'current' }
  | { kind: 'session'; id: string }
  | { kind: 'create' }

/** What the pending picker request will do once the user chooses. */
export interface PendingRequest {
  mode: 'add' | 'send'
  /**
   * Open the target session after delivering, instead of staying put.
   *
   * Set by the picker's "add and open" button. Only meaningful for `add` — a
   * `send` is already running in the background and the user asked to stay.
   */
  openAfter?: boolean
  /**
   * The payload, snapshotted at trigger time.
   *
   * Snapshotting is required, not an optimization: the quote bar is dismissed
   * before the picker opens (it would otherwise float above the dialog at
   * --z-quote-bar), and the live staged quotes are the CURRENT session's draft.
   * By the time the user confirms, both are gone — so the payload has to be
   * captured here.
   */
  quotes: PendingQuote[]
  attachments: FileEntry[]
  /** The message text ('' for a pure "add to draft" with no typed text). */
  text: string
}

// ── Visibility gate ────────────────────────────────────────────

/**
 * `activeTab` lives in App.vue (it is not part of the wide-screen layout
 * module), so App injects a getter once at setup. Keeping the getter here
 * rather than importing App state avoids a cycle and keeps this module
 * testable in isolation.
 */
let activeTabGetter: (() => string) | null = null

export function setActiveTabGetter(fn: () => string) {
  activeTabGetter = fn
}

/**
 * How to actually OPEN a session: switch to it AND bring the chat panel into
 * view. Injected by App.vue because revealing the panel is a tab switch that
 * only App owns — `switchSession` alone would change the session while the user
 * is still looking at another tab.
 */
let openSession: ((sessionId: string) => void) | null = null

export function setOpenSessionHandler(fn: (sessionId: string) => void) {
  openSession = fn
}

/**
 * Whether the user can SEE the chat panel right now.
 *
 * This is the whole trigger for the picker: if the conversation is on screen the
 * user would simply switch it to the session they want (the intuitive move), so
 * we keep the old "go straight to the current session" behavior. Only when the
 * panel is hidden — the narrow layout on another tab, or a collapsed chat
 * column on desktop — does the destination become ambiguous and the picker
 * earn its place.
 *
 * MUST go through `isChatPanelVisible`: an inline `isWideScreen || ...` is
 * always truthy in `<script setup>` because a ref does not auto-unwrap there,
 * which would silently make every session look "on screen" (the exact bug that
 * helper was written for). It also covers the desktop case the user called out —
 * the chat column can be hidden even on a wide screen.
 */
export function canSeeChatPanel(): boolean {
  const layout = useWideScreenLayout()
  return isChatPanelVisible({
    isWideScreen: layout.isWideScreen.value,
    chatCollapsed: layout.chatCollapsed.value,
    activeTab: activeTabGetter?.() ?? '',
  })
}

// ── Picker state ───────────────────────────────────────────────

const pickerOpen = ref(false)
const pending = ref<PendingRequest | null>(null)

/**
 * Ask the user where the payload should go.
 *
 * Returns true when the picker was opened (the caller should NOT also perform
 * the action — it happens on confirm), false when the chat panel is visible and
 * the caller should just target the current session.
 */
export function requestTarget(request: PendingRequest): boolean {
  if (canSeeChatPanel()) return false
  pending.value = request
  pickerOpen.value = true
  return true
}

/** The request awaiting a choice, for the dialog to render against. */
export function usePendingTarget() {
  return { pickerOpen, pending }
}

export function cancelTarget() {
  pickerOpen.value = false
  pending.value = null
}

/**
 * Route an "attach this to the chat" action.
 *
 * Returns true when the picker took over — the caller must then NOT attach
 * locally (the picker will, once the user picks a destination). Returns false
 * when the chat panel is visible, so the caller keeps its existing behavior.
 *
 * Only the ADD direction goes through here. Removing an attachment must stay
 * immediate: asking "which conversation?" to take something away would be
 * nonsense, and the user can only remove what they can already see.
 */
export function requestAttachmentTarget(entry: FileEntry): boolean {
  if (!entry?.path) return false
  return requestTarget({ mode: 'add', quotes: [], attachments: [entry], text: '' })
}

// ── Dispatch ───────────────────────────────────────────────────

function newQueueId(): string {
  return `pending-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

/**
 * Resolve the chosen target to a concrete session id.
 *
 * `current` needs no work. `create` makes a session with the default agent —
 * deliberately no agent choice, since the user is picking a destination for an
 * already-composed message, not configuring a conversation — and refreshes the
 * session list so the new row appears. Returns '' when creation failed (already
 * toasted).
 */
async function resolveTargetId(target: SessionTarget): Promise<string> {
  const identity = useSessionIdentity()
  if (target.kind === 'current') return identity.currentSessionId.value
  if (target.kind === 'session') return target.id

  const maxCount = store.state.sessionMaxCount
  if (maxCount > 0 && store.state.sessionCount >= maxCount) {
    useToast().show(gt('chat.session.sessionLimitReached'), { icon: '⚠️', type: 'error' })
    return ''
  }
  const id = await identity.createSessionInBackground()
  if (!id) {
    useToast().show(gt('chat.session.createFailed', { status: '' }), { icon: '⚠️', type: 'error' })
    return ''
  }
  return id
}

/**
 * Open a session: switch to it and bring the chat panel into view. Falls back
 * to a plain switch when no handler was injected (unit tests, early boot).
 */
function openTargetSession(sessionId: string) {
  if (openSession) openSession(sessionId)
  else useSessionIdentity().switchSession(sessionId)
}

/**
 * Deliver a snapshotted payload to the chosen session.
 *
 * The two modes differ in intent:
 *  - `send`  — post it now. Goes through the shared enqueue path for ANY
 *    session (idle → backend starts the turn; running → drain loop), which is
 *    how the push bots already deliver into sessions nobody is watching. No
 *    switch, so the user stays where they are.
 *  - `add`   — stage it into that session's stored draft, so it is waiting in
 *    the input when the user opens that session. For the CURRENT session it
 *    goes to the live input instead, since there is nothing to restore.
 *
 * `request.openAfter` is the picker's "add and open" half: same delivery, then
 * navigate. The delivery always happens first, so opening can never lose the
 * payload.
 */
export async function dispatchToTarget(target: SessionTarget, request: PendingRequest): Promise<boolean> {
  const identity = useSessionIdentity()
  const chatContext = useChatContext()

  const sessionId = await resolveTargetId(target)
  if (!sessionId) return false

  const isCurrent = sessionId === identity.currentSessionId.value
  const isNew = target.kind === 'create'
  const quoteEntries = materializeQuotes(request.quotes)
  const entries: FileEntry[] = [...request.attachments, ...quoteEntries]
  if (request.mode === 'send') {
    if (!request.text.trim() && entries.length === 0) return false
    const ok = await identity.enqueueToSession(sessionId, request.text, entries, newQueueId())
    if (ok) {
      // Name the outcome that actually happened: "sent" reads oddly when the
      // user had to have a session created for them.
      useToast().show(
        gt(isNew ? 'quoteBar.createdSession' : 'quoteBar.sentToSession'),
        { icon: '✅', type: 'success', duration: 2000 },
      )
    }
    return ok
  }

  // mode === 'add'
  if (isCurrent && !isNew) {
    // Nothing to restore later — the card belongs in the live input right now.
    // (A freshly created session can never be the one already on screen, hence
    // the `!isNew` guard: it must take the draft path so the payload is waiting
    // when the session opens.)
    for (const q of request.quotes) chatContext.addStagedQuote(q, q.note)
    for (const f of request.attachments) chatContext.addAttachedFile(f.path, f.isDir, f.startLine, f.endLine)
    useToast().show(gt('quoteBar.addedToChat'), { icon: '📎', type: 'success', duration: 1500 })
    // Already on this session; "open" is a no-op, and there is nothing to
    // reveal that is not already in front of the user.
    return true
  }

  for (const q of request.quotes) chatContext.stageQuoteIntoDraft(sessionId, q, q.note)
  for (const f of request.attachments) chatContext.stageAttachmentIntoDraft(sessionId, f)

  if (request.openAfter) {
    // Open LAST: `switchSession` snapshots the live input on the way out, so
    // doing it after the staging above cannot disturb the payload we just
    // parked in the target's draft.
    openTargetSession(sessionId)
    useToast().show(
      gt(isNew ? 'quoteBar.openAndCreated' : 'quoteBar.addedToChat'),
      { icon: '📎', type: 'success', duration: 2000 },
    )
    return true
  }

  // Say where it went: the card is not on screen, so a bare "added" would look
  // like nothing happened. The create row's plain click reaches here too, and
  // it must NOT claim the session was opened.
  useToast().show(
    gt(isNew ? 'quoteBar.createdSession' : 'quoteBar.addedToSessionDraft'),
    { icon: '📎', type: 'success', duration: 2000 },
  )
  return true
}

/**
 * Confirm the pending request against the chosen session and close the picker.
 * A no-op when nothing is pending (e.g. a stray event after cancel).
 *
 * `opts.openAfter` comes from the picker's "add and open" button.
 */
export async function confirmTarget(target: SessionTarget, opts: { openAfter?: boolean } = {}): Promise<void> {
  const request = pending.value
  pickerOpen.value = false
  pending.value = null
  if (!request) return
  if (opts.openAfter) request.openAfter = true
  await dispatchToTarget(target, request)
}
